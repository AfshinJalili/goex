package validation

import "testing"

func TestValidateOrderRequest(t *testing.T) {
	cases := []struct {
		name    string
		symbol  string
		side    string
		typeVal string
		tif     string
		qty     string
		price   string
		valid   bool
	}{
		{"valid limit", "BTC-USD", "buy", "limit", "GTC", "1.5", "100", true},
		{"valid market", "BTC-USD", "sell", "market", "IOC", "2", "", true},
		{"valid market with price", "BTC-USD", "sell", "market", "IOC", "2", "101", true},
		{"missing symbol", "", "buy", "limit", "GTC", "1", "100", false},
		{"bad side", "BTC-USD", "hold", "limit", "GTC", "1", "100", false},
		{"bad type", "BTC-USD", "buy", "stop", "GTC", "1", "100", false},
		{"bad tif", "BTC-USD", "buy", "limit", "DAY", "1", "100", false},
		{"bad qty", "BTC-USD", "buy", "limit", "GTC", "-1", "100", false},
		{"missing price", "BTC-USD", "buy", "limit", "GTC", "1", "", false},
		{"zero price", "BTC-USD", "buy", "limit", "GTC", "1", "0", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateOrderRequest(tc.symbol, tc.side, tc.typeVal, tc.tif, tc.qty, tc.price)
			if tc.valid && len(errs) > 0 {
				t.Fatalf("expected valid, got errors: %+v", errs)
			}
			if !tc.valid && len(errs) == 0 {
				t.Fatalf("expected errors, got none")
			}
		})
	}
}

func TestNormalizeSymbol(t *testing.T) {
	got := NormalizeSymbol(" btc-usd ")
	if got != "BTC-USD" {
		t.Fatalf("expected BTC-USD, got %s", got)
	}
}
