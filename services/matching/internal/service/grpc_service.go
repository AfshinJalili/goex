package service

import (
	"context"

	matchingpb "github.com/AfshinJalili/goex/services/matching/proto/matching/v1"
	"log/slog"
)

type MatchingService struct {
	matchingpb.UnimplementedMatchingEngineServer
	engine Engine
	logger *slog.Logger
}

type Engine interface {
	LoadSnapshot(ctx context.Context, symbol string) (int, []string, error)
	ActiveSymbols() int
	TotalOrders() int
}

func NewMatchingService(engine Engine, logger *slog.Logger) *MatchingService {
	if logger == nil {
		logger = slog.Default()
	}
	return &MatchingService{engine: engine, logger: logger}
}

func (s *MatchingService) LoadSnapshot(ctx context.Context, req *matchingpb.LoadSnapshotRequest) (*matchingpb.LoadSnapshotResponse, error) {
	if s.engine == nil {
		return &matchingpb.LoadSnapshotResponse{}, nil
	}
	loaded, symbols, err := s.engine.LoadSnapshot(ctx, req.GetSymbol())
	if err != nil {
		s.logger.Error("snapshot load failed", "error", err)
		return nil, err
	}
	return &matchingpb.LoadSnapshotResponse{
		OrdersLoaded: int32(loaded),
		Symbols:      symbols,
	}, nil
}

func (s *MatchingService) HealthCheck(ctx context.Context, _ *matchingpb.HealthCheckRequest) (*matchingpb.HealthCheckResponse, error) {
	if s.engine == nil {
		return &matchingpb.HealthCheckResponse{Status: "unavailable"}, nil
	}
	return &matchingpb.HealthCheckResponse{
		Status:        "healthy",
		ActiveSymbols: int32(s.engine.ActiveSymbols()),
		TotalOrders:   int32(s.engine.TotalOrders()),
	}, nil
}
