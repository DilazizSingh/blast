package diff_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/load"
	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/sim"
)

const acme = "../../examples/acme/"

func compareExample(t *testing.T, after string) diff.Result {
	t.Helper()
	s, err := load.Open(load.Inputs{
		Before: acme + "pricing.old.yml", After: acme + after,
		Customers: acme + "customers.json", Rules: acme + "rules.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := diff.Compare(s.Customers, s.Before, s.After, s.Segments)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func ids(rows []diff.Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Customer.ID
	}
	return out
}

func TestSectionFive(t *testing.T) {
	res := compareExample(t, "pricing.new.yml")

	want := diff.Totals{Customers: 10, Affected: 5, OldCents: 522900, NewCents: 458460}
	if res.Totals != want {
		t.Errorf("totals = %+v, want %+v", res.Totals, want)
	}
	if res.ChangeCents() != -64440 {
		t.Errorf("change = %d cents, want -64440", res.ChangeCents())
	}
	if radius := res.Affected * 100 / res.Customers; radius != 50 {
		t.Errorf("blast radius = %d%%, want 50%%", radius)
	}
	if got, want := ids(res.Changed()), []string{"c3", "c4", "c6", "c8", "c7"}; !reflect.DeepEqual(got, want) {
		t.Errorf("changed, largest first = %v, want %v", got, want)
	}

	segments := []struct {
		name string
		diff.Totals
		change int64
	}{
		{"annual + referred", diff.Totals{Customers: 5, Affected: 5, OldCents: 322200, NewCents: 257760}, -64440},
		{"monthly", diff.Totals{Customers: 3, Affected: 0, OldCents: 22700, NewCents: 22700}, 0},
		{"legacy plans", diff.Totals{Customers: 2, Affected: 1, OldCents: 158000, NewCents: 142200}, -15800},
		{"has coupon", diff.Totals{Customers: 2, Affected: 2, OldCents: 115200, NewCents: 92160}, -23040},
	}
	if len(res.Segments) != len(segments) {
		t.Fatalf("got %d segments, want %d", len(res.Segments), len(segments))
	}
	for i, want := range segments {
		got := res.Segments[i]
		if got.Name != want.name || got.Totals != want.Totals || got.ChangeCents() != want.change {
			t.Errorf("segment %d = %q %+v (change %d)\nwant %q %+v (change %d)",
				i, got.Name, got.Totals, got.ChangeCents(), want.name, want.Totals, want.change)
		}
	}
}

func TestSectionFiveAfterTheFix(t *testing.T) {
	res := compareExample(t, "pricing.fixed.yml")

	want := diff.Totals{Customers: 10, Affected: 2, OldCents: 522900, NewCents: 497300}
	if res.Totals != want {
		t.Errorf("totals = %+v, want %+v", res.Totals, want)
	}
	if res.ChangeCents() != -25600 {
		t.Errorf("change = %d cents, want -25600", res.ChangeCents())
	}
	changed := res.Changed()
	if got, want := ids(changed), []string{"c3", "c8"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
	if changed[0].ChangeCents() != -19800 || changed[1].ChangeCents() != -5800 {
		t.Errorf("changes = %d, %d; want -19800, -5800", changed[0].ChangeCents(), changed[1].ChangeCents())
	}
}

// The properties from the plan's testing table, on random pricing and
// customers: totals are the sum of their rows, and no discount stack ever
// takes an invoice below zero or above list price.
func TestProperties(t *testing.T) {
	rng := rand.New(rand.NewPCG(2026, 11))
	for round := range 300 {
		before, after := randomConfig(rng), randomConfig(rng)
		customers := randomCustomers(rng, 40)
		res, err := diff.Compare(customers, before, after, diff.DefaultFilters())
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}

		if got := sumOf(res.Rows, allRows(len(res.Rows))); got != res.Totals {
			t.Fatalf("round %d: totals %+v, but the rows add up to %+v", round, res.Totals, got)
		}
		var change int64
		for _, row := range res.Rows {
			change += row.ChangeCents()
			checkInvoice(t, round, row.Customer.ID, "old", row.Old)
			checkInvoice(t, round, row.Customer.ID, "new", row.New)
		}
		if change != res.ChangeCents() {
			t.Fatalf("round %d: total change %d, but per-customer changes add up to %d", round, res.ChangeCents(), change)
		}
		for _, seg := range res.Segments {
			if got := sumOf(res.Rows, seg.Rows); got != seg.Totals {
				t.Fatalf("round %d: segment %q totals %+v, but its rows add up to %+v", round, seg.Name, seg.Totals, got)
			}
		}
	}
}

func checkInvoice(t *testing.T, round int, id, side string, inv sim.Invoice) {
	t.Helper()
	if inv.TotalCents < 0 || inv.TotalCents > inv.BaseCents {
		t.Fatalf("round %d: %s %s invoice is %d cents on a list price of %d", round, id, side, inv.TotalCents, inv.BaseCents)
	}
	running := inv.BaseCents
	for _, s := range inv.Steps {
		if s.BeforeCents != running || s.AfterCents > s.BeforeCents || s.AfterCents < 0 {
			t.Fatalf("round %d: %s %s step %+v does not follow a running total of %d", round, id, side, s, running)
		}
		running = s.AfterCents
	}
	if running != inv.TotalCents {
		t.Fatalf("round %d: %s %s steps end at %d, invoice says %d", round, id, side, running, inv.TotalCents)
	}
}

func allRows(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func sumOf(rows []diff.Row, indexes []int) diff.Totals {
	var t diff.Totals
	for _, i := range indexes {
		r := rows[i]
		t.Customers++
		if r.ChangeCents() != 0 {
			t.Affected++
		}
		t.OldCents += r.Old.TotalCents
		t.NewCents += r.New.TotalCents
	}
	return t
}

var (
	planNames     = []string{"starter", "pro", "legacy-pro"}
	discountNames = []string{"D0", "D1", "D2", "D3"}
	conditions    = []string{"", "", "referred", `billing == "annual"`, "legacy", "coupon_count == 0", "not referred and coupon_count > 1"}
)

func randomConfig(rng *rand.Rand) model.Config {
	cfg := model.Config{Plans: map[string]model.Plan{}}
	for _, name := range planNames {
		price := func() int64 {
			if rng.IntN(20) == 0 {
				return model.MaxCents
			}
			return rng.Int64N(200_000)
		}
		cfg.Plans[name] = model.Plan{
			Prices: map[model.Billing]int64{model.Monthly: price(), model.Annual: price()},
			Legacy: rng.IntN(3) == 0,
		}
	}
	for _, i := range rng.Perm(len(discountNames))[:rng.IntN(len(discountNames)+1)] {
		d := model.Discount{Name: discountNames[i], When: conditions[rng.IntN(len(conditions))]}
		if rng.IntN(2) == 0 {
			d.BasisPoints = 1 + rng.Int64N(100_00)
		} else {
			d.AmountCents = 1 + rng.Int64N(300_000)
		}
		cfg.Discounts = append(cfg.Discounts, d)
	}
	return cfg
}

func randomCustomers(rng *rand.Rand, n int) []model.Customer {
	customers := make([]model.Customer, n)
	for i := range customers {
		c := model.Customer{
			ID:       fmt.Sprintf("c%d", i),
			Plan:     planNames[rng.IntN(len(planNames))],
			Billing:  []model.Billing{model.Monthly, model.Annual}[rng.IntN(2)],
			Referred: rng.IntN(2) == 0,
		}
		for _, j := range rng.Perm(len(discountNames))[:rng.IntN(3)] {
			c.Coupons = append(c.Coupons, discountNames[j])
		}
		customers[i] = c
	}
	return customers
}

func TestConfigs(t *testing.T) {
	plan := func(monthly, annual int64, legacy bool) model.Plan {
		return model.Plan{Prices: map[model.Billing]int64{model.Monthly: monthly, model.Annual: annual}, Legacy: legacy}
	}
	welcome := model.Discount{Name: "WELCOME10", BasisPoints: 1000}
	referral := model.Discount{Name: "REFERRAL20", BasisPoints: 2000, When: "referred"}
	base := model.Config{
		Plans:     map[string]model.Plan{"pro": plan(9900, 99000, false), "old": plan(7900, 79000, true)},
		Discounts: []model.Discount{welcome, referral},
		Payment:   map[string]string{"payment.capture": "automatic"},
	}

	cases := []struct {
		name  string
		after model.Config
		want  []diff.Change
	}{
		{"identical", base, nil},
		{"price raised", model.Config{
			Plans:     map[string]model.Plan{"pro": plan(10900, 99000, false), "old": plan(7900, 79000, true)},
			Discounts: base.Discounts, Payment: base.Payment,
		}, []diff.Change{{Path: "plans.pro.monthly", Old: "$99.00", New: "$109.00"}}},
		{"plan no longer legacy, discount edited, payment touched", model.Config{
			Plans: map[string]model.Plan{"pro": plan(9900, 99000, false), "old": plan(7900, 79000, false)},
			Discounts: []model.Discount{
				{Name: "WELCOME10", AmountCents: 1000},
				{Name: "REFERRAL20", BasisPoints: 1250, When: "referred and not legacy"},
			},
			Payment: map[string]string{"payment.capture": "manual", "payment.retries": "3"},
		}, []diff.Change{
			{Path: "discounts.REFERRAL20.percent", Old: "20%", New: "12.5%"},
			{Path: "discounts.REFERRAL20.when", Old: "referred", New: "referred and not legacy"},
			{Path: "discounts.WELCOME10.amount", Old: "", New: "$10.00"},
			{Path: "discounts.WELCOME10.percent", Old: "10%", New: ""},
			{Path: "payment.capture", Old: "automatic", New: "manual"},
			{Path: "payment.retries", Old: "", New: "3"},
			{Path: "plans.old.legacy", Old: "true", New: ""},
		}},
		{"discount removed", model.Config{Plans: base.Plans, Discounts: []model.Discount{welcome}, Payment: base.Payment},
			[]diff.Change{
				{Path: "discounts.REFERRAL20.percent", Old: "20%", New: ""},
				{Path: "discounts.REFERRAL20.when", Old: "referred", New: ""},
			}},
		{"discounts reordered", model.Config{Plans: base.Plans, Discounts: []model.Discount{referral, welcome}, Payment: base.Payment},
			[]diff.Change{{Path: "discounts.order", Old: "WELCOME10, REFERRAL20", New: "REFERRAL20, WELCOME10"}}},
	}
	for _, tc := range cases {
		if got := diff.Configs(base, tc.after); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", tc.name, got, tc.want)
		}
	}
}
