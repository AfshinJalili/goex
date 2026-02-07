package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AfshinJalili/goex/libs/health"
	"github.com/AfshinJalili/goex/libs/httpmiddleware"
	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/AfshinJalili/goex/libs/logging"
	"github.com/AfshinJalili/goex/libs/metrics"
	"github.com/AfshinJalili/goex/libs/trace"
	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/config"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/consumer"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/handlers"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/service"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/storage"
	riskpb "github.com/AfshinJalili/goex/services/risk/proto/risk/v1"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log/slog"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := logging.NewLogger(cfg.App.LogLevel, cfg.App.ServiceName, cfg.App.Env)
	shutdownTracer, err := trace.InitTracer(cfg.App.ServiceName, cfg.App.Env)
	if err != nil {
		logger.Error("tracer init failed", "error", err)
	} else {
		defer func() {
			_ = shutdownTracer(context.Background())
		}()
	}

	if cfg.App.Env == "dev" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics.Register(registry)

	orderMetrics := service.NewMetrics(registry)
	kafkaMetrics := kafka.NewProducerMetrics(registry)

	ready := health.NewManager(false)

	pool, err := connectDB(cfg)
	if err != nil {
		logger.Error("db connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	riskConn, err := grpc.Dial(cfg.RiskService.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("risk service connection failed", "error", err)
		os.Exit(1)
	}
	defer riskConn.Close()

	ledgerConn, err := grpc.Dial(cfg.LedgerService.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("ledger service connection failed", "error", err)
		os.Exit(1)
	}
	defer ledgerConn.Close()

	riskClient := riskpb.NewRiskClient(riskConn)
	ledgerClient := ledgerpb.NewLedgerClient(ledgerConn)

	store := storage.New(pool)

	producer, err := kafka.NewSyncProducer(cfg.Kafka.Brokers, logger, kafkaMetrics)
	if err != nil {
		logger.Error("kafka producer init failed", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	consumerGroup, err := kafka.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.ConsumerGroup, logger)
	if err != nil {
		logger.Error("kafka consumer init failed", "error", err)
		os.Exit(1)
	}
	defer consumerGroup.Close()

	orderSvc := service.NewOrderService(store, riskClient, ledgerClient, producer, logger, orderMetrics, service.Topics{
		OrdersAccepted:  cfg.Kafka.Topics.OrdersAccepted,
		OrdersRejected:  cfg.Kafka.Topics.OrdersRejected,
		OrdersCancelled: cfg.Kafka.Topics.OrdersCancelled,
	})

	handler := handlers.New(orderSvc, logger)
	router := gin.New()
	router.Use(httpmiddleware.RequestID())
	router.Use(httpmiddleware.Logger(logger))
	router.Use(httpmiddleware.Recovery(logger))
	router.Use(trace.Middleware(cfg.App.ServiceName))

	router.GET("/healthz", health.LivenessHandler)
	router.GET("/readyz", health.ReadinessHandler(ready))
	router.GET(cfg.App.MetricsPath, gin.WrapH(metrics.Handler(registry)))

	handler.Register(router, []byte(cfg.JWTSecret))

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.App.HTTP.Host, cfg.App.HTTP.Port),
		Handler:      router,
		ReadTimeout:  cfg.App.HTTP.ReadTimeout,
		WriteTimeout: cfg.App.HTTP.WriteTimeout,
		IdleTimeout:  cfg.App.HTTP.IdleTimeout,
	}

	tradeConsumer := consumer.NewTradeConsumer(store, logger, orderMetrics)

	ready.SetReady(true)

	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	defer consumerCancel()

	go func() {
		logger.Info("order-ingest http starting", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
		}
	}()

	go func() {
		logger.Info("order-ingest consumer starting", "topic", cfg.Kafka.Topics.TradesExecuted)
		if err := consumerGroup.Consume(consumerCtx, []string{cfg.Kafka.Topics.TradesExecuted}, tradeConsumer); err != nil {
			logger.Error("kafka consumer error", "error", err)
		}
	}()

	waitForShutdown(httpServer, ready, consumerCancel, logger)
}

func connectDB(cfg *config.Config) (*pgxpool.Pool, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.DB.User,
		cfg.DB.Password,
		cfg.DB.Host,
		cfg.DB.Port,
		cfg.DB.Name,
		cfg.DB.SSLMode,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func waitForShutdown(httpServer *http.Server, ready *health.Manager, cancel context.CancelFunc, logger *slog.Logger) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutdown started")
	ready.SetReady(false)
	cancel()

	ctx, cancelTimeout := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelTimeout()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("http shutdown error", "error", err)
	}
	logger.Info("shutdown complete")
}
