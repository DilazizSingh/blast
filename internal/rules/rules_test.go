package rules_test

import (
	"reflect"
	"testing"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/load"
	"github.com/dilaziz/blast/internal/rules"
	"github.com/dilaziz/blast/internal/rules/expr"
)

const acme = "../../examples/acme/"

// example loads the plan's section 5 with the given proposed pricing.
func example(t *testing.T, after string) (*load.Scenario, diff.Result) {
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
	return s, res
}

type outcome struct {
	rule      string
	status    rules.Status
	customers []string
}

func outcomes(verdicts []rules.Verdict) []outcome {
	out := make([]outcome, len(verdicts))
	for i, v := range verdicts {
		out[i] = outcome{v.Rule.Name, v.Status, v.Customers}
	}
	return out
}

func TestSectionFive(t *testing.T) {
	s, res := example(t, "pricing.new.yml")
	verdicts := rules.Check(s.Rules, res, rules.Context{Changes: diff.Configs(s.Before, s.After)})

	want := []outcome{
		{"max-one-discount", rules.Broken, []string{"c4", "c7"}},
		{"no-legacy-repricing", rules.Broken, []string{"c6"}},
		{"finance-approval", rules.NotTriggered, nil},
	}
	if got := outcomes(verdicts); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if n := rules.CountBroken(verdicts); n != 2 {
		t.Errorf("CountBroken = %d, want 2", n)
	}
}

func TestSectionFiveAfterTheFix(t *testing.T) {
	s, res := example(t, "pricing.fixed.yml")
	verdicts := rules.Check(s.Rules, res, rules.Context{Changes: diff.Configs(s.Before, s.After)})

	want := []outcome{
		{"max-one-discount", rules.Passed, nil},
		{"no-legacy-repricing", rules.Passed, nil},
		{"finance-approval", rules.NotTriggered, nil},
	}
	if got := outcomes(verdicts); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if n := rules.CountBroken(verdicts); n != 0 {
		t.Errorf("CountBroken = %d, want 0", n)
	}
}

func rule(t *testing.T, name string, kind rules.Kind, cond, when string) rules.Rule {
	t.Helper()
	compile := func(src string) *expr.Expr {
		e, err := expr.Compile(src, rules.Schema())
		if err != nil {
			t.Fatalf("rule %s: %q: %v", name, src, err)
		}
		return e
	}
	r := rules.Rule{Name: name, Kind: kind, Cond: compile(cond)}
	if when != "" {
		r.When = compile(when)
	}
	return r
}

// A rule that reads no customer facts is judged once, for the whole change.
func TestChangeLevelRules(t *testing.T) {
	_, res := example(t, "pricing.new.yml")
	financeApproval := rule(t, "finance-approval", rules.Require, `approval("finance")`, `diff.touches("payment")`)
	paymentChange := []diff.Change{{Path: "payment.capture", Old: "automatic", New: "manual"}}

	cases := []struct {
		name string
		rule rules.Rule
		ctx  rules.Context
		want rules.Status
	}{
		{"payment untouched", financeApproval, rules.Context{}, rules.NotTriggered},
		{"payment touched, nobody approved", financeApproval, rules.Context{Changes: paymentChange}, rules.Broken},
		{"payment touched, wrong approver", financeApproval, rules.Context{Changes: paymentChange, Approvals: []string{"legal"}}, rules.Broken},
		{"payment touched, finance approved", financeApproval, rules.Context{Changes: paymentChange, Approvals: []string{"legal", "finance"}}, rules.Passed},
		{"never, and it happened", rule(t, "freeze", rules.Never, `diff.touches("plans")`, ""),
			rules.Context{Changes: []diff.Change{{Path: "plans.pro.annual"}}}, rules.Broken},
		{"never, and it did not", rule(t, "freeze", rules.Never, `diff.touches("plans")`, ""),
			rules.Context{Changes: []diff.Change{{Path: "discounts.X.percent"}}}, rules.Passed},
		{"touches matches the exact path", rule(t, "r", rules.Never, `diff.touches("payment.capture")`, ""),
			rules.Context{Changes: paymentChange}, rules.Broken},
		{"touches matches whole path segments only", rule(t, "r", rules.Never, `diff.touches("pay")`, ""),
			rules.Context{Changes: paymentChange}, rules.Passed},
	}
	for _, tc := range cases {
		v := rules.Check([]rules.Rule{tc.rule}, res, tc.ctx)[0]
		if v.Status != tc.want || v.Customers != nil {
			t.Errorf("%s: status %v, customers %v; want status %v and no customers", tc.name, v.Status, v.Customers, tc.want)
		}
	}
}

func TestPerCustomerRules(t *testing.T) {
	_, res := example(t, "pricing.new.yml")
	cases := []struct {
		name string
		rule rules.Rule
		ctx  rules.Context
		want outcome
	}{
		{"require lists who falls short",
			rule(t, "annual-stays-above-250", rules.Require, `invoice.new >= 250`, `customer.billing == "annual"`),
			rules.Context{}, outcome{"annual-stays-above-250", rules.Broken, []string{"c7", "c8"}}},
		{"when narrows a rule to nobody",
			rule(t, "enterprise-untouched", rules.Never, `invoice.change != 0`, `plan.name == "enterprise"`),
			rules.Context{}, outcome{"enterprise-untouched", rules.NotTriggered, nil}},
		{"a per-customer when gates a change-level requirement",
			rule(t, "legacy-needs-finance", rules.Require, `approval("finance")`, `plan.legacy and invoice.change != 0`),
			rules.Context{}, outcome{"legacy-needs-finance", rules.Broken, []string{"c6"}}},
		{"and the approval satisfies it",
			rule(t, "legacy-needs-finance", rules.Require, `approval("finance")`, `plan.legacy and invoice.change != 0`),
			rules.Context{Approvals: []string{"finance"}}, outcome{"legacy-needs-finance", rules.Passed, nil}},
		{"dollars compare against cents",
			rule(t, "c4-exactly", rules.Never, `invoice.new == 712.80 and invoice.old == 891 and invoice.change == -178.2`, ""),
			rules.Context{}, outcome{"c4-exactly", rules.Broken, []string{"c4"}}},
		{"bare and qualified names agree",
			rule(t, "names", rules.Never, `legacy != plan.legacy or billing != customer.billing or coupon_count != customer.coupon_count`, ""),
			rules.Context{}, outcome{"names", rules.Passed, nil}},
	}
	for _, tc := range cases {
		got := outcomes(rules.Check([]rules.Rule{tc.rule}, res, tc.ctx))[0]
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", tc.name, got, tc.want)
		}
	}
}

func TestNoCustomersTriggersNothing(t *testing.T) {
	r := rule(t, "max-one-discount", rules.Never, `customer.discount_count > 1`, "")
	v := rules.Check([]rules.Rule{r}, diff.Result{}, rules.Context{})[0]
	if v.Status != rules.NotTriggered {
		t.Errorf("status = %v, want NotTriggered", v.Status)
	}
}
