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
	"github.com/AfshinJalili/goex/libs/logging"
	"github.com/AfshinJalili/goex/libs/metrics"
	"github.com/AfshinJalili/goex/libs/trace"
	"github.com/AfshinJalili/goex/services/auth/internal/config"
	"github.com/AfshinJalili/goex/services/auth/internal/handlers"
	"github.com/AfshinJalili/goex/services/auth/internal/rate"
	"github.com/AfshinJalili/goex/services/auth/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/redis/go-redis/v9"
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

	ready := health.NewManager(true)

	pool, err := connectDB(cfg)
	if err != nil {
		logger.Error("db connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	limiter, limiterClose, err := buildLimiter(cfg, logger)
	if err != nil {
		logger.Error("rate limiter init failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = limiterClose()
	}()

	store := storage.New(pool)
	authHandler := handlers.NewAuthHandler(store, logger, cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL, limiter, cfg.JWTIssuer)

	router := gin.New()
	router.Use(httpmiddleware.RequestID())
	router.Use(httpmiddleware.Logger(logger))
	router.Use(httpmiddleware.Recovery(logger))
	router.Use(trace.Middleware(cfg.App.ServiceName))

	router.GET("/healthz", health.LivenessHandler)
	router.GET("/readyz", health.ReadinessHandler(ready))
	router.GET(cfg.App.MetricsPath, gin.WrapH(metrics.Handler(registry)))

	authHandler.RegisterRoutes(router)

	addr := fmt.Sprintf("%s:%d", cfg.App.HTTP.Host, cfg.App.HTTP.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.App.HTTP.ReadTimeout,
		WriteTimeout: cfg.App.HTTP.WriteTimeout,
		IdleTimeout:  cfg.App.HTTP.IdleTimeout,
	}

	go func() {
		logger.Info("auth service starting", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
		}
	}()

	waitForShutdown(server, logger)
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

func buildLimiter(cfg *config.Config, logger *slog.Logger) (rate.Limiter, func() error, error) {
	if cfg.RateLimit.Redis.Addr != "" {
		client := redis.NewClient(&redis.Options{
			Addr:     cfg.RateLimit.Redis.Addr,
			Password: cfg.RateLimit.Redis.Password,
			DB:       cfg.RateLimit.Redis.DB,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.Ping(ctx).Err(); err != nil {
			_ = client.Close()
			if cfg.App.Env == "dev" || cfg.App.Env == "test" {
				logger.Warn("redis rate limiter unavailable, falling back to memory", "error", err)
				return rate.NewMemory(cfg.RateLimit.LoginLimit, cfg.RateLimit.Window), func() error { return nil }, nil
			}
			return nil, nil, err
		}

		return rate.NewRedisLimiter(client, cfg.RateLimit.LoginLimit, cfg.RateLimit.Window, cfg.RateLimit.Redis.Prefix), client.Close, nil
	}

	if cfg.App.Env == "dev" || cfg.App.Env == "test" {
		return rate.NewMemory(cfg.RateLimit.LoginLimit, cfg.RateLimit.Window), func() error { return nil }, nil
	}

	return nil, nil, fmt.Errorf("rate limiter redis not configured")
}

func waitForShutdown(server *http.Server, logger *slog.Logger) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("shutdown started")
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
		return
	}
	logger.Info("shutdown complete")
}
