package logging

import (
	"log/slog"
	"os"
	"strings"
)

func NewLogger(level string, serviceName string, env string) *slog.Logger {
	lvl := parseLevel(level)
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	logger := slog.New(h)
	return logger.With(
		slog.String("service", serviceName),
		slog.String("env", env),
	)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
