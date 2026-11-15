package sim

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dilaziz/blast/internal/model"
)

// The pricing of the plan's section 5, built by hand so these tests depend on
// nothing but the engine.
var (
	plans = map[string]model.Plan{
		"starter":    {Prices: map[model.Billing]int64{model.Monthly: 2900, model.Annual: 29000}},
		"pro":        {Prices: map[model.Billing]int64{model.Monthly: 9900, model.Annual: 99000}},
		"legacy-pro": {Prices: map[model.Billing]int64{model.Monthly: 7900, model.Annual: 79000}, Legacy: true},
	}
	welcome10 = model.Discount{Name: "WELCOME10", BasisPoints: 1000}

	oldPricing = model.Config{Plans: plans, Discounts: []model.Discount{welcome10}}
	newPricing = model.Config{Plans: plans, Discounts: []model.Discount{
		welcome10,
		{Name: "REFERRAL20", BasisPoints: 2000, When: `billing == "annual" and referred`},
	}}
	fixedPricing = model.Config{Plans: plans, Discounts: []model.Discount{
		welcome10,
		{Name: "REFERRAL20", BasisPoints: 2000, When: `billing == "annual" and referred and not legacy and coupon_count == 0`},
	}}
)

func customer(id, plan string, billing model.Billing, referred bool, coupons ...string) model.Customer {
	return model.Customer{ID: id, Plan: plan, Billing: billing, Referred: referred, Coupons: coupons}
}

func engine(t *testing.T, cfg model.Config) *Engine {
	t.Helper()
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestInvoiceSectionFive(t *testing.T) {
	cases := []struct {
		c               model.Customer
		old, new, fixed int64 // cents
	}{
		{customer("c1", "starter", model.Monthly, false), 2900, 2900, 2900},
		{customer("c2", "pro", model.Monthly, false), 9900, 9900, 9900},
		{customer("c3", "pro", model.Annual, true), 99000, 79200, 79200},
		{customer("c4", "pro", model.Annual, true, "WELCOME10"), 89100, 71280, 89100},
		{customer("c5", "pro", model.Annual, false), 99000, 99000, 99000},
		{customer("c6", "legacy-pro", model.Annual, true), 79000, 63200, 79000},
		{customer("c7", "starter", model.Annual, true, "WELCOME10"), 26100, 20880, 26100},
		{customer("c8", "starter", model.Annual, true), 29000, 23200, 23200},
		{customer("c9", "pro", model.Monthly, true), 9900, 9900, 9900},
		{customer("c10", "legacy-pro", model.Annual, false), 79000, 79000, 79000},
	}
	configs := []struct {
		name string
		eng  *Engine
		want func(i int) int64
	}{
		{"old", engine(t, oldPricing), func(i int) int64 { return cases[i].old }},
		{"new", engine(t, newPricing), func(i int) int64 { return cases[i].new }},
		{"fixed", engine(t, fixedPricing), func(i int) int64 { return cases[i].fixed }},
	}
	for _, cfg := range configs {
		var total int64
		for i, tc := range cases {
			inv, err := cfg.eng.Invoice(tc.c)
			if err != nil {
				t.Fatalf("%s pricing, %s: %v", cfg.name, tc.c.ID, err)
			}
			if inv.TotalCents != cfg.want(i) {
				t.Errorf("%s pricing, %s: %d cents, want %d", cfg.name, tc.c.ID, inv.TotalCents, cfg.want(i))
			}
			total += inv.TotalCents
		}
		wantTotal := map[string]int64{"old": 522900, "new": 458460, "fixed": 497300}[cfg.name]
		if total != wantTotal {
			t.Errorf("%s pricing: total %d cents, want %d", cfg.name, total, wantTotal)
		}
	}
}

// c4 is the plan's worked example: 99000 -> 89100 -> 71280.
func TestInvoiceRecordsEachStep(t *testing.T) {
	inv, err := engine(t, newPricing).Invoice(customer("c4", "pro", model.Annual, true, "WELCOME10"))
	if err != nil {
		t.Fatal(err)
	}
	want := Invoice{BaseCents: 99000, TotalCents: 71280, Steps: []Step{
		{Discount: "WELCOME10", BeforeCents: 99000, AfterCents: 89100},
		{Discount: "REFERRAL20", BeforeCents: 89100, AfterCents: 71280},
	}}
	if !reflect.DeepEqual(inv, want) {
		t.Errorf("got  %+v\nwant %+v", inv, want)
	}
}

func TestApply(t *testing.T) {
	percent := func(bp int64) model.Discount { return model.Discount{BasisPoints: bp} }
	amount := func(cents int64) model.Discount { return model.Discount{AmountCents: cents} }
	cases := []struct {
		name  string
		d     model.Discount
		cents int64
		want  int64
	}{
		{"10% of $990", percent(1000), 99000, 89100},
		{"20% of $891", percent(2000), 89100, 71280},
		{"half a cent rounds up", percent(5000), 1, 1},      // 0.5 -> 1
		{"1.5 cents rounds up", percent(5000), 3, 2},        // 1.5 -> 2
		{"below half rounds down", percent(1500), 999, 849}, // 849.15 -> 849
		{"above half rounds up", percent(1500), 1005, 854},  // 854.25 -> 854
		{"exactly half at scale", percent(2500), 1002, 752}, // 751.5 -> 752
		{"12.5% of $100", percent(1250), 10000, 8750},
		{"33.33% of $100", percent(3333), 10000, 6667},
		{"100% is free", percent(10000), 99000, 0},
		{"percent of nothing", percent(2000), 0, 0},
		{"percent at the cap", percent(1), model.MaxCents, 99_990_000_000},
		{"$10 off", amount(1000), 9900, 8900},
		{"amount equal to price", amount(9900), 9900, 0},
		{"amount above price stops at zero", amount(10000), 9900, 0},
	}
	for _, tc := range cases {
		if got := apply(tc.d, tc.cents); got != tc.want {
			t.Errorf("%s: apply = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestStackingIsSequentialInDeclaredOrder(t *testing.T) {
	half := model.Discount{Name: "HALF", BasisPoints: 5000}
	ten := model.Discount{Name: "TENOFF", AmountCents: 1000}
	c := customer("c1", "pro", model.Monthly, false, "TENOFF", "HALF") // coupon order must not matter

	cases := []struct {
		order []model.Discount
		want  int64
	}{
		{[]model.Discount{half, ten}, 3950}, // 9900 -> 4950 -> 3950
		{[]model.Discount{ten, half}, 4450}, // 9900 -> 8900 -> 4450
	}
	for _, tc := range cases {
		inv, err := engine(t, model.Config{Plans: plans, Discounts: tc.order}).Invoice(c)
		if err != nil {
			t.Fatal(err)
		}
		if inv.TotalCents != tc.want {
			t.Errorf("%s first: %d cents, want %d", tc.order[0].Name, inv.TotalCents, tc.want)
		}
	}
}

func TestDiscountAppliesAtMostOnce(t *testing.T) {
	cfg := model.Config{Plans: plans, Discounts: []model.Discount{
		{Name: "REFERRAL20", BasisPoints: 2000, When: "referred"},
	}}
	// Holding the coupon and meeting the condition is still one discount.
	inv, err := engine(t, cfg).Invoice(customer("c1", "pro", model.Annual, true, "REFERRAL20"))
	if err != nil {
		t.Fatal(err)
	}
	if inv.TotalCents != 79200 || len(inv.Steps) != 1 {
		t.Errorf("got %d cents in %d steps, want 79200 in 1", inv.TotalCents, len(inv.Steps))
	}
}

func TestCouponWorksWithoutTheCondition(t *testing.T) {
	cfg := model.Config{Plans: plans, Discounts: []model.Discount{
		{Name: "REFERRAL20", BasisPoints: 2000, When: "referred"},
	}}
	inv, err := engine(t, cfg).Invoice(customer("c1", "pro", model.Annual, false, "REFERRAL20"))
	if err != nil {
		t.Fatal(err)
	}
	if inv.TotalCents != 79200 {
		t.Errorf("got %d cents, want 79200: a held coupon applies whatever `when:` says", inv.TotalCents)
	}
}

func TestCouponTheConfigDoesNotDeclareIsIgnored(t *testing.T) {
	inv, err := engine(t, oldPricing).Invoice(customer("c1", "pro", model.Annual, false, "RETIRED"))
	if err != nil {
		t.Fatal(err)
	}
	if inv.TotalCents != 99000 || len(inv.Steps) != 0 {
		t.Errorf("got %d cents in %d steps, want list price", inv.TotalCents, len(inv.Steps))
	}
}

func TestErrors(t *testing.T) {
	eng := engine(t, model.Config{Plans: map[string]model.Plan{
		"solo": {Prices: map[model.Billing]int64{model.Monthly: 900}},
	}})
	cases := []struct {
		c    model.Customer
		want string
	}{
		{customer("c1", "pro-plus", model.Monthly, false), `customer "c1": unknown plan "pro-plus"`},
		{customer("c2", "solo", model.Annual, false), `customer "c2": plan "solo" has no annual price`},
	}
	for _, tc := range cases {
		if _, err := eng.Invoice(tc.c); err == nil || err.Error() != tc.want {
			t.Errorf("got %v, want %s", err, tc.want)
		}
	}

	_, err := New(model.Config{Plans: plans, Discounts: []model.Discount{{Name: "X", BasisPoints: 500, When: "refered"}}})
	if err == nil || !strings.Contains(err.Error(), `discount "X": when: column 1: unknown name "refered"`) {
		t.Errorf("New with a bad condition: %v", err)
	}
}

// Every fact must answer under each of its names with the declared type.
func TestFactsMatchSchema(t *testing.T) {
	who := Facts{
		Customer: customer("c6", "legacy-pro", model.Annual, true, "WELCOME10"),
		Plan:     plans["legacy-pro"],
	}
	for name, typ := range Schema().Vars {
		if got := who.Var(name).Type; got != typ {
			t.Errorf("%s: value has type %v, schema says %v", name, got, typ)
		}
	}
	for bare, qualified := range map[string]string{"legacy": "plan.legacy", "billing": "customer.billing", "plan": "plan.name"} {
		if who.Var(bare) != who.Var(qualified) {
			t.Errorf("%s and %s disagree", bare, qualified)
		}
	}
}
