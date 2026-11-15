package sim

import (
	"testing"

	"github.com/dilaziz/blast/internal/model"
)

func TestQuantityMultipliesTheListPrice(t *testing.T) {
	seats := func(n int64, coupons ...string) model.Customer {
		c := customer("c1", "pro", model.Monthly, false, coupons...)
		c.Quantity = n
		return c
	}
	tenOff := model.Discount{Name: "TENOFF", AmountCents: 1000}
	cfg := model.Config{Plans: plans, Discounts: []model.Discount{welcome10, tenOff}}

	cases := []struct {
		name        string
		c           model.Customer
		base, total int64
	}{
		{"unstated quantity is one", seats(0), 9900, 9900},
		{"one seat", seats(1), 9900, 9900},
		{"twelve seats", seats(12), 118800, 118800},
		{"a percent applies to the whole invoice", seats(12, "WELCOME10"), 118800, 106920},
		{"a fixed amount comes off once, not per seat", seats(12, "TENOFF"), 118800, 117800},
		{"both, in declared order", seats(12, "TENOFF", "WELCOME10"), 118800, 105920},
		{"a nonsense negative is one", seats(-4), 9900, 9900},
	}
	for _, tc := range cases {
		inv, err := engine(t, cfg).Invoice(tc.c)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if inv.BaseCents != tc.base || inv.TotalCents != tc.total {
			t.Errorf("%s: base %d, total %d; want %d and %d", tc.name, inv.BaseCents, inv.TotalCents, tc.base, tc.total)
		}
	}
}

func TestQuantityCannotOverflow(t *testing.T) {
	eng := engine(t, model.Config{Plans: map[string]model.Plan{
		"pro":  {Prices: map[model.Billing]int64{model.Monthly: 9900}},
		"max":  {Prices: map[model.Billing]int64{model.Monthly: model.MaxCents}},
		"free": {Prices: map[model.Billing]int64{model.Monthly: 0}},
	}})
	on := func(plan string, n int64) model.Customer {
		c := customer("c1", plan, model.Monthly, false)
		c.Quantity = n
		return c
	}

	if inv, err := eng.Invoice(on("pro", 1_000_000)); err != nil || inv.TotalCents != 9_900_000_000 {
		t.Errorf("a million seats at $99: %d cents, %v", inv.TotalCents, err)
	}
	if inv, err := eng.Invoice(on("free", model.MaxQuantity)); err != nil || inv.TotalCents != 0 {
		t.Errorf("free plan at the quantity cap: %d cents, %v", inv.TotalCents, err)
	}
	for _, c := range []model.Customer{
		on("pro", 20_000_000),                // $1.98 billion
		on("max", 2),                         // twice the cap
		on("free", model.MaxQuantity+1),      // past the quantity cap
		on("pro", 9_223_372_036_854_775_807), // would wrap around if multiplied
	} {
		if inv, err := eng.Invoice(c); err == nil {
			t.Errorf("plan %s × %d priced at %d cents, want an error", c.Plan, c.Quantity, inv.TotalCents)
		}
	}
}
