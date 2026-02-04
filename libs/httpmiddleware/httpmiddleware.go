package httpmiddleware

import (
	"net/http"
	"time"

	"log/slog"

	"github.com/AfshinJalili/goex/libs/metrics"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	requestIDHeader   = "X-Request-ID"
	traceParentHeader = "traceparent"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader(requestIDHeader)
		if reqID == "" {
			reqID = uuid.NewString()
		}
		c.Set(requestIDHeader, reqID)
		c.Header(requestIDHeader, reqID)
		c.Next()
	}
}

func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)

		status := c.Writer.Status()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		reqID, _ := c.Get(requestIDHeader)
		traceParent := c.GetHeader(traceParentHeader)

		logger.Info("request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.String("client_ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
			slog.Any("request_id", reqID),
			slog.String("traceparent", traceParent),
		)

		metrics.RequestCount.WithLabelValues(c.Request.Method, path, http.StatusText(status)).Inc()
		metrics.RequestDuration.WithLabelValues(c.Request.Method, path, http.StatusText(status)).Observe(latency.Seconds())
	}
}

func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				reqID, _ := c.Get(requestIDHeader)
				logger.Error("panic",
					slog.Any("error", err),
					slog.String("path", c.Request.URL.Path),
					slog.Any("request_id", reqID),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
