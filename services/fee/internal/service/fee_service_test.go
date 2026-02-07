package service

import (
	"context"
	"testing"

	"github.com/AfshinJalili/goex/services/fee/internal/storage"
	feepb "github.com/AfshinJalili/goex/services/fee/proto/fee/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
)

type fakeStore struct {
	volume      string
	tier        *storage.FeeTier
	defaultTier *storage.FeeTier
	err         error
	calledVolumeLookup  bool
	calledAccountVolume bool
}

func (f *fakeStore) GetFeeTierByVolume(ctx context.Context, volume string) (*storage.FeeTier, error) {
	f.calledVolumeLookup = true
	return f.tier, f.err
}

func (f *fakeStore) GetDefaultFeeTier(ctx context.Context) (*storage.FeeTier, error) {
	return f.defaultTier, f.err
}

func (f *fakeStore) GetAccountVolume30d(ctx context.Context, accountID uuid.UUID) (string, error) {
	f.calledAccountVolume = true
	return f.volume, f.err
}

type fakeCache struct {
	tier *storage.FeeTier
	ok   bool
}

func (f *fakeCache) GetTierByVolume(volume string) (*storage.FeeTier, bool) {
	return f.tier, f.ok
}

func (f *fakeCache) Size() int {
	return 1
}

func TestGetFeeTierWithVolumeOverride(t *testing.T) {
	accountID := uuid.New()
	tier := &storage.FeeTier{ID: uuid.New(), Name: "vip", MakerFeeBps: 5, TakerFeeBps: 10, MinVolume: "100000"}

	svc := NewFeeService(&fakeStore{}, &fakeCache{tier: tier, ok: true}, slog.Default(), nil)
	resp, err := svc.GetFeeTier(context.Background(), &feepb.GetFeeTierRequest{
		AccountId: accountID.String(),
		Volume:    "100000",
	})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.TierName != "vip" {
		t.Fatalf("expected vip, got %s", resp.TierName)
	}
}

func TestGetFeeTierFallbackToStore(t *testing.T) {
	accountID := uuid.New()
	tier := &storage.FeeTier{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"}

	svc := NewFeeService(&fakeStore{tier: tier}, &fakeCache{ok: false}, slog.Default(), nil)
	resp, err := svc.GetFeeTier(context.Background(), &feepb.GetFeeTierRequest{
		AccountId: accountID.String(),
		Volume:    "0",
	})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.TierName != "default" {
		t.Fatalf("expected default, got %s", resp.TierName)
	}
}

func TestGetFeeTierInvalidAccount(t *testing.T) {
	svc := NewFeeService(&fakeStore{}, &fakeCache{ok: false}, slog.Default(), nil)
	_, err := svc.GetFeeTier(context.Background(), &feepb.GetFeeTierRequest{
		AccountId: "invalid",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestGetFeeTierInvalidVolume(t *testing.T) {
	store := &fakeStore{}
	svc := NewFeeService(store, &fakeCache{ok: false}, slog.Default(), nil)
	_, err := svc.GetFeeTier(context.Background(), &feepb.GetFeeTierRequest{
		AccountId: uuid.New().String(),
		Volume:    "not-a-number",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.calledAccountVolume || store.calledVolumeLookup {
		t.Fatal("expected volume validation to skip store calls")
	}
}

func TestGetFeeTierNegativeVolume(t *testing.T) {
	store := &fakeStore{}
	svc := NewFeeService(store, &fakeCache{ok: false}, slog.Default(), nil)
	_, err := svc.GetFeeTier(context.Background(), &feepb.GetFeeTierRequest{
		AccountId: uuid.New().String(),
		Volume:    "-1",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.calledAccountVolume || store.calledVolumeLookup {
		t.Fatal("expected volume validation to skip store calls")
	}
}

func TestCalculateFeesMaker(t *testing.T) {
	accountID := uuid.New()
	tier := &storage.FeeTier{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"}

	svc := NewFeeService(&fakeStore{volume: "0", defaultTier: tier}, &fakeCache{tier: tier, ok: true}, slog.Default(), nil)
	resp, err := svc.CalculateFees(context.Background(), &feepb.CalculateFeesRequest{
		AccountId: accountID.String(),
		Symbol:    "BTC-USD",
		Side:      "buy",
		OrderType: "maker",
		Quantity:  "2",
		Price:     "100",
	})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if resp.FeeAmount != "0.2" {
		t.Fatalf("expected fee 0.2, got %s", resp.FeeAmount)
	}
	if resp.FeeAsset != "USD" {
		t.Fatalf("expected USD fee asset, got %s", resp.FeeAsset)
	}
}

func TestCalculateFeesInvalidOrderType(t *testing.T) {
	accountID := uuid.New()
	svc := NewFeeService(&fakeStore{volume: "0"}, &fakeCache{ok: false}, slog.Default(), nil)
	_, err := svc.CalculateFees(context.Background(), &feepb.CalculateFeesRequest{
		AccountId: accountID.String(),
		Symbol:    "BTC-USD",
		OrderType: "invalid",
		Quantity:  "1",
		Price:     "100",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestCalculateFeesInvalidSymbol(t *testing.T) {
	accountID := uuid.New()
	tier := &storage.FeeTier{ID: uuid.New(), Name: "default", MakerFeeBps: 10, TakerFeeBps: 20, MinVolume: "0"}
	svc := NewFeeService(&fakeStore{volume: "0", defaultTier: tier}, &fakeCache{tier: tier, ok: true}, slog.Default(), nil)
	_, err := svc.CalculateFees(context.Background(), &feepb.CalculateFeesRequest{
		AccountId: accountID.String(),
		Symbol:    "BTCUSD",
		OrderType: "maker",
		Quantity:  "1",
		Price:     "100",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestCalculateFeesTierNotFound(t *testing.T) {
	accountID := uuid.New()
	svc := NewFeeService(&fakeStore{volume: "0"}, &fakeCache{ok: false}, slog.Default(), nil)
	_, err := svc.CalculateFees(context.Background(), &feepb.CalculateFeesRequest{
		AccountId: accountID.String(),
		Symbol:    "BTC-USD",
		OrderType: "maker",
		Quantity:  "1",
		Price:     "100",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}
