package server

import (
	"math"
	"testing"
)

// EUR-based rates like Frankfurter returns (rate[X] = units of X per 1 EUR).
var testRates = map[string]float64{
	"USD": 1.10,
	"GBP": 0.85,
	"JPY": 160.0,
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestConvertAmount(t *testing.T) {
	cases := []struct {
		amount   float64
		from, to string
		want     float64
		ok       bool
	}{
		// Same currency is identity.
		{100, "USD", "USD", 100, true},
		// EUR is implicit (rate 1) and never appears in the rates map.
		{100, "EUR", "USD", 110, true},
		{110, "USD", "EUR", 100, true},
		// Cross-currency goes via EUR: 170 GBP → EUR (170/0.85=200) → USD (×1.10=220).
		{170, "GBP", "USD", 220, true},
		// EUR→EUR identity.
		{50, "EUR", "EUR", 50, true},
		// Unknown source or target currency → not convertible.
		{100, "XXX", "USD", 0, false},
		{100, "USD", "XXX", 0, false},
	}
	for _, c := range cases {
		got, ok := convertAmount(c.amount, c.from, c.to, testRates)
		if ok != c.ok {
			t.Errorf("convertAmount(%v,%s,%s) ok=%v, want %v", c.amount, c.from, c.to, ok, c.ok)
			continue
		}
		if ok && !approx(got, c.want) {
			t.Errorf("convertAmount(%v,%s,%s)=%v, want %v", c.amount, c.from, c.to, got, c.want)
		}
	}
}

func TestNormalizeTravelCost(t *testing.T) {
	f := func(v float64) *float64 { return &v }

	t.Run("nil amount clears both", func(t *testing.T) {
		var amount *float64
		cur := "usd"
		if err := normalizeTravelCost(&amount, &cur); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if amount != nil || cur != "" {
			t.Errorf("expected cleared pair, got amount=%v cur=%q", amount, cur)
		}
	})

	t.Run("non-positive amount clears both", func(t *testing.T) {
		amount := f(0)
		cur := "USD"
		if err := normalizeTravelCost(&amount, &cur); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if amount != nil || cur != "" {
			t.Errorf("expected cleared pair, got amount=%v cur=%q", amount, cur)
		}
	})

	t.Run("amount without currency errors", func(t *testing.T) {
		amount := f(120)
		cur := "  "
		if err := normalizeTravelCost(&amount, &cur); err == nil {
			t.Error("expected an error for a missing currency")
		}
	})

	t.Run("unsupported currency errors", func(t *testing.T) {
		amount := f(120)
		cur := "xbt"
		if err := normalizeTravelCost(&amount, &cur); err == nil {
			t.Error("expected an error for an unsupported currency")
		}
	})

	t.Run("valid pair is upper-cased", func(t *testing.T) {
		amount := f(120.5)
		cur := "usd"
		if err := normalizeTravelCost(&amount, &cur); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if amount == nil || *amount != 120.5 || cur != "USD" {
			t.Errorf("expected 120.5/USD, got amount=%v cur=%q", amount, cur)
		}
	})
}
