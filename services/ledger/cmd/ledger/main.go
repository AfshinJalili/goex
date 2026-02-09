package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AfshinJalili/goex/libs/health"
	"github.com/AfshinJalili/goex/libs/httpmiddleware"
	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/AfshinJalili/goex/libs/logging"
	"github.com/AfshinJalili/goex/libs/metrics"
	"github.com/AfshinJalili/goex/libs/trace"
	feepb "github.com/AfshinJalili/goex/services/fee/proto/fee/v1"
	"github.com/AfshinJalili/goex/services/ledger/internal/config"
	"github.com/AfshinJalili/goex/services/ledger/internal/consumer"
	"github.com/AfshinJalili/goex/services/ledger/internal/service"
	"github.com/AfshinJalili/goex/services/ledger/internal/storage"
	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
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

	ledgerMetrics := service.NewMetrics(registry)
	kafkaMetrics := kafka.NewProducerMetrics(registry)

	ready := health.NewManager(false)

	pool, err := connectDB(cfg)
	if err != nil {
		logger.Error("db connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	feeConn, err := grpc.Dial(cfg.FeeService.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("fee service connection failed", "error", err)
		os.Exit(1)
	}
	defer feeConn.Close()

	feeClient := feepb.NewFeeServiceClient(feeConn)
	store := storage.New(pool, feeClient, logger, ledgerMetrics)

	ledgerService := service.NewLedgerService(store, logger, ledgerMetrics)

	grpcServer := grpc.NewServer()
	ledgerpb.RegisterLedgerServer(grpcServer, ledgerService)

	healthServer := grpchealth.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	httpServer := buildHTTPServer(cfg, ready, registry, logger)

	producer, err := kafka.NewSyncProducer(cfg.Kafka.Brokers, logger, kafkaMetrics)
	if err != nil {
		logger.Error("kafka producer init failed", "error", err)
		os.Exit(1)
	}
	defer producer.Close()
	publisher := kafka.Publisher(producer)
	if strings.TrimSpace(cfg.Kafka.Topics.DeadLetter) != "" {
		publisher = kafka.NewDLQPublisher(producer, producer, cfg.Kafka.Topics.DeadLetter, logger)
	}

	consumerGroup, err := kafka.NewConsumer(cfg.Kafka.Brokers, cfg.Kafka.ConsumerGroup, logger)
	if err != nil {
		logger.Error("kafka consumer init failed", "error", err)
		os.Exit(1)
	}
	consumerGroup.WithDLQ(producer, cfg.Kafka.Topics.DeadLetter)
	defer consumerGroup.Close()

	tradeConsumer := consumer.NewTradeConsumer(store, ledgerService, publisher, logger)

	ready.SetReady(true)

	grpcAddr := fmt.Sprintf("%s:%d", cfg.GRPC.Host, cfg.GRPC.Port)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		logger.Error("grpc listen failed", "error", err)
		os.Exit(1)
	}

	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	defer consumerCancel()

	go func() {
		logger.Info("ledger grpc starting", "addr", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("grpc server error", "error", err)
		}
	}()

	go func() {
		logger.Info("ledger http starting", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
		}
	}()

	go func() {
		logger.Info("ledger consumer starting", "topic", cfg.Kafka.Topics.TradesExecuted)
		if err := consumerGroup.Consume(consumerCtx, []string{cfg.Kafka.Topics.TradesExecuted}, tradeConsumer); err != nil {
			logger.Error("kafka consumer error", "error", err)
		}
	}()

	waitForShutdown(grpcServer, healthServer, httpServer, ready, consumerCancel, logger)
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

func buildHTTPServer(cfg *config.Config, ready *health.Manager, registry *prometheus.Registry, logger *slog.Logger) *http.Server {
	router := gin.New()
	router.Use(httpmiddleware.RequestID())
	router.Use(httpmiddleware.Logger(logger))
	router.Use(httpmiddleware.Recovery(logger))
	router.Use(trace.Middleware(cfg.App.ServiceName))

	router.GET("/healthz", health.LivenessHandler)
	router.GET("/readyz", health.ReadinessHandler(ready))
	router.GET(cfg.App.MetricsPath, gin.WrapH(metrics.Handler(registry)))

	addr := fmt.Sprintf("%s:%d", cfg.App.HTTP.Host, cfg.App.HTTP.Port)
	return &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.App.HTTP.ReadTimeout,
		WriteTimeout: cfg.App.HTTP.WriteTimeout,
		IdleTimeout:  cfg.App.HTTP.IdleTimeout,
	}
}

func waitForShutdown(grpcServer *grpc.Server, healthServer *grpchealth.Server, httpServer *http.Server, ready *health.Manager, cancel context.CancelFunc, logger *slog.Logger) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutdown started")
	ready.SetReady(false)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	cancel()

	ctx, cancelTimeout := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelTimeout()

	grpcDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcDone)
	}()

	select {
	case <-grpcDone:
	case <-ctx.Done():
		grpcServer.Stop()
	}

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("http shutdown error", "error", err)
	}
	logger.Info("shutdown complete")
}
