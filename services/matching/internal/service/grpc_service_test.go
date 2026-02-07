package service

import (
	"context"
	"testing"

	matchingpb "github.com/AfshinJalili/goex/services/matching/proto/matching/v1"
)

type fakeEngine struct{}

func (f *fakeEngine) LoadSnapshot(ctx context.Context, symbol string) (int, []string, error) {
	return 2, []string{"BTC-USD"}, nil
}

func (f *fakeEngine) ActiveSymbols() int { return 1 }

func (f *fakeEngine) TotalOrders() int { return 5 }

func TestMatchingServiceHealth(t *testing.T) {
	svc := NewMatchingService(&fakeEngine{}, nil)
	resp, err := svc.HealthCheck(context.Background(), &matchingpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check error: %v", err)
	}
	if resp.Status != "healthy" {
		t.Fatalf("expected healthy, got %s", resp.Status)
	}
	if resp.ActiveSymbols != 1 || resp.TotalOrders != 5 {
		t.Fatalf("unexpected counts")
	}
}

func TestMatchingServiceLoadSnapshot(t *testing.T) {
	svc := NewMatchingService(&fakeEngine{}, nil)
	resp, err := svc.LoadSnapshot(context.Background(), &matchingpb.LoadSnapshotRequest{Symbol: "BTC-USD"})
	if err != nil {
		t.Fatalf("load snapshot error: %v", err)
	}
	if resp.OrdersLoaded != 2 {
		t.Fatalf("expected 2 orders loaded, got %d", resp.OrdersLoaded)
	}
	if len(resp.Symbols) != 1 || resp.Symbols[0] != "BTC-USD" {
		t.Fatalf("unexpected symbols")
	}
}
