package report

import (
	"testing"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/rules"
	"github.com/dilaziz/blast/internal/rules/expr"
)

func TestRadius(t *testing.T) {
	cases := []struct {
		affected, customers int
		want                string
	}{
		{0, 0, "0%"}, {0, 10, "0%"}, {5, 10, "50%"}, {2, 10, "20%"}, {10, 10, "100%"},
		{1, 3, "33%"}, {2, 3, "67%"}, {1, 12000, "<1%"}, {59, 12000, "<1%"}, {60, 12000, "1%"},
		{11999, 12000, ">99%"}, {199, 200, ">99%"}, {198, 200, "99%"},
	}
	for _, tc := range cases {
		got := Radius(diff.Totals{Affected: tc.affected, Customers: tc.customers})
		if got != tc.want {
			t.Errorf("Radius(%d of %d) = %q, want %q", tc.affected, tc.customers, got, tc.want)
		}
	}
}

func TestChangePercent(t *testing.T) {
	cases := []struct {
		old, new int64
		want     string
	}{
		{522900, 458460, "-12.3%"}, {522900, 497300, "-4.9%"}, {100, 100, "0.0%"}, {0, 500, "n/a"},
		{10000, 10500, "+5.0%"}, {10000, 0, "-100.0%"}, {10000, 30000, "+200.0%"},
		{10000, 10005, "+0.1%"}, {10000, 10004, "+0.0%"}, {1000, 875, "-12.5%"},
	}
	for _, tc := range cases {
		got := ChangePercent(diff.Totals{OldCents: tc.old, NewCents: tc.new})
		if got != tc.want {
			t.Errorf("ChangePercent(%d -> %d) = %q, want %q", tc.old, tc.new, got, tc.want)
		}
	}
}

func TestDetail(t *testing.T) {
	cond, err := expr.Compile(`approval("finance")`, rules.Schema())
	if err != nil {
		t.Fatal(err)
	}
	many := []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10", "c11"}
	cases := []struct {
		v    rules.Verdict
		want string
	}{
		{rules.Verdict{Status: rules.Passed}, "ok"},
		{rules.Verdict{Status: rules.NotTriggered}, "not triggered"},
		{rules.Verdict{Status: rules.Broken, Customers: []string{"c6"}}, "1 customer: c6"},
		{rules.Verdict{Status: rules.Broken, Customers: []string{"c4", "c7"}}, "2 customers: c4, c7"},
		{rules.Verdict{Status: rules.Broken, Customers: many}, "11 customers: c1, c2, c3, c4, c5, c6, c7, c8, +3 more"},
		{rules.Verdict{Status: rules.Broken, Rule: rules.Rule{Kind: rules.Require, Cond: cond}}, `needs approval("finance")`},
		{rules.Verdict{Status: rules.Broken, Rule: rules.Rule{Kind: rules.Never, Cond: cond}}, `must not hold: approval("finance")`},
	}
	for _, tc := range cases {
		if got := Detail(tc.v); got != tc.want {
			t.Errorf("Detail = %q, want %q", got, tc.want)
		}
	}
}
