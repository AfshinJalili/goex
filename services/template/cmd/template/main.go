package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AfshinJalili/goex/libs/config"
	"github.com/AfshinJalili/goex/libs/health"
	"github.com/AfshinJalili/goex/libs/httpmiddleware"
	"github.com/AfshinJalili/goex/libs/logging"
	"github.com/AfshinJalili/goex/libs/metrics"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func main() {
	configPath := os.Getenv("CEX_CONFIG")
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := logging.NewLogger(cfg.LogLevel, cfg.ServiceName, cfg.Env)

	if cfg.Env == "dev" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics.Register(registry)

	ready := health.NewManager(true)

	router := gin.New()
	router.Use(httpmiddleware.RequestID())
	router.Use(httpmiddleware.Logger(logger))
	router.Use(httpmiddleware.Recovery(logger))

	router.GET("/healthz", health.LivenessHandler)
	router.GET("/readyz", health.ReadinessHandler(ready))
	router.GET(cfg.MetricsPath, gin.WrapH(metrics.Handler(registry)))

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": cfg.ServiceName,
			"status":  "ok",
		})
	})

	addr := fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	go func() {
		logger.Info("server starting", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
		}
	}()

	waitForShutdown(server, logger)
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
