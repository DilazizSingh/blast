package model

import (
	"strings"
	"testing"
)

func TestBuildByHand(t *testing.T) {
	cfg := Config{
		Plans: map[string]Plan{
			"pro": {Prices: map[Billing]int64{Monthly: 9900, Annual: 99000}},
		},
		Discounts: []Discount{{Name: "WELCOME10", BasisPoints: 1000}},
	}
	c := Customer{ID: "c4", Plan: "pro", Billing: Annual, Coupons: []string{"WELCOME10"}, Referred: true}

	if got := cfg.Plans[c.Plan].Prices[c.Billing]; got != 99000 {
		t.Errorf("pro annual = %d cents, want 99000", got)
	}
	if !c.HasCoupon("WELCOME10") || c.HasCoupon("REFERRAL20") {
		t.Errorf("HasCoupon is wrong for %+v", c)
	}
	if !cfg.HasDiscount("WELCOME10") || cfg.HasDiscount("REFERRAL20") {
		t.Errorf("HasDiscount is wrong for %+v", cfg.Discounts)
	}
	if !Annual.Valid() || Billing("yearly").Valid() {
		t.Error("Billing.Valid accepts the wrong periods")
	}
}

func TestParseHundredths(t *testing.T) {
	good := map[string]int64{
		"0": 0, "29": 2900, "29.9": 2990, "29.99": 2999, "0.05": 5,
		"-12.50": -1250, "990": 99000, "1000000000": MaxCents,
	}
	for in, want := range good {
		got, err := ParseHundredths(in)
		if err != nil || got != want {
			t.Errorf("ParseHundredths(%q) = %d, %v; want %d", in, got, err, want)
		}
	}

	bad := map[string]string{
		"":                     "not a number",
		"abc":                  "not a number",
		".5":                   "not a number",
		"5.":                   "not a number",
		"1.2.3":                "not a number",
		"+5":                   "not a number",
		"1e3":                  "not a number",
		"1,000":                "not a number",
		"0.999":                "more than two decimal places",
		"1000000001":           "too large",
		"99999999999999999999": "too large",
	}
	for in, want := range bad {
		if _, err := ParseHundredths(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseHundredths(%q) error = %v; want it to mention %q", in, err, want)
		}
	}
}

func TestDollars(t *testing.T) {
	cases := map[int64]string{
		0: "$0.00", 5: "$0.05", 2900: "$29.00", 71280: "$712.80",
		522900: "$5,229.00", -64440: "-$644.40", 123456789: "$1,234,567.89",
		100000: "$1,000.00", 99999: "$999.99",
	}
	for cents, want := range cases {
		if got := Dollars(cents); got != want {
			t.Errorf("Dollars(%d) = %q, want %q", cents, got, want)
		}
	}
}

func TestDecimal(t *testing.T) {
	cases := map[int64]string{0: "0", 2000: "20", 1250: "12.5", 1225: "12.25", 5: "0.05", -100: "-1", 10000: "100"}
	for n, want := range cases {
		if got := Decimal(n); got != want {
			t.Errorf("Decimal(%d) = %q, want %q", n, got, want)
		}
	}
}
