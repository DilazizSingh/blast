// Package rules checks a team's own policy against a simulated pricing
// change. The conditions are written in the language of the expr package.
package rules

import (
	"slices"
	"strings"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/rules/expr"
	"github.com/dilaziz/blast/internal/sim"
)

// Kind is how a rule's condition is read.
type Kind int

const (
	Never   Kind = iota + 1 // broken wherever the condition holds
	Require                 // broken wherever the condition does not hold
)

func (k Kind) String() string {
	if k == Require {
		return "require"
	}
	return "never"
}

// Rule is one line of policy.
type Rule struct {
	Name string
	Kind Kind
	Cond *expr.Expr
	When *expr.Expr // optional; where it is false the rule does not apply
}

// perCustomer reports whether the rule reads anything about a customer. A
// rule that does not, such as one built only from approval() and
// diff.touches(), is about the change as a whole and is judged once.
func (r Rule) perCustomer() bool {
	return len(r.Cond.Vars()) > 0 || r.When != nil && len(r.When.Vars()) > 0
}

// Context is what rules can know about a change beyond its customers.
type Context struct {
	Approvals []string      // who signed off, for approval("...")
	Changes   []diff.Change // settings that differ, for diff.touches("...")
}

// Status is the outcome of one rule.
type Status int

const (
	Passed Status = iota + 1
	Broken
	NotTriggered // its `when:` held nowhere, so there was nothing to judge
)

func (s Status) String() string {
	switch s {
	case Passed:
		return "passed"
	case Broken:
		return "broken"
	}
	return "not_triggered"
}

// Verdict is one rule's outcome for a change.
type Verdict struct {
	Rule   Rule
	Status Status
	// Customers lists who breaks a per-customer rule, in customer-file
	// order. It is empty for a rule about the change as a whole.
	Customers []string
}

// rowFacts are what a rule can read beyond sim's customer facts: how the
// comparison came out for one customer. All of them are numbers.
var rowFacts = map[string]func(diff.Row) expr.Value{
	"customer.discount_count": func(r diff.Row) expr.Value { return expr.CountOf(len(r.New.Steps)) },
	"invoice.old":             func(r diff.Row) expr.Value { return expr.NumOf(r.Old.TotalCents) },
	"invoice.new":             func(r diff.Row) expr.Value { return expr.NumOf(r.New.TotalCents) },
	"invoice.change":          func(r diff.Row) expr.Value { return expr.NumOf(r.ChangeCents()) },
}

// Schema declares everything a rule's condition may refer to.
func Schema() expr.Schema {
	s := sim.Schema()
	for name := range rowFacts {
		s.Vars[name] = expr.Num
	}
	text := []expr.Type{expr.Str}
	s.Funcs = map[string]expr.Func{
		"approval":     {Params: text, Result: expr.Bool},
		"diff.touches": {Params: text, Result: expr.Bool},
	}
	return s
}

// env presents one customer's outcome, and the change around it, to a rule.
// row is nil when judging a rule about the change as a whole; such a rule
// reads no variables, so Var is never reached.
type env struct {
	row *diff.Row
	ctx Context
}

func (e env) Var(name string) expr.Value {
	if get, ok := rowFacts[name]; ok {
		return get(*e.row)
	}
	return sim.Facts{Customer: e.row.Customer, Plan: e.row.Plan}.Var(name)
}

func (e env) Call(name string, args []expr.Value) expr.Value {
	if name == "approval" {
		return expr.BoolOf(slices.Contains(e.ctx.Approvals, args[0].Str))
	}
	return expr.BoolOf(touches(e.ctx.Changes, args[0].Str))
}

// touches reports whether any changed setting sits at or under path:
// "payment" matches payment.capture, "discounts.REFERRAL20" matches its
// percent, and "pay" matches neither.
func touches(changes []diff.Change, path string) bool {
	return slices.ContainsFunc(changes, func(c diff.Change) bool {
		return c.Path == path || strings.HasPrefix(c.Path, path+".")
	})
}

// Check judges every rule against the comparison.
func Check(rules []Rule, res diff.Result, ctx Context) []Verdict {
	verdicts := make([]Verdict, len(rules))
	for i, r := range rules {
		verdicts[i] = r.check(res, ctx)
	}
	return verdicts
}

func (r Rule) check(res diff.Result, ctx Context) Verdict {
	v := Verdict{Rule: r, Status: NotTriggered}
	if !r.perCustomer() {
		v.Status = r.judge(env{ctx: ctx})
		return v
	}
	for i := range res.Rows {
		row := &res.Rows[i]
		switch r.judge(env{row: row, ctx: ctx}) {
		case Broken:
			v.Status = Broken
			v.Customers = append(v.Customers, row.Customer.ID)
		case Passed:
			if v.Status == NotTriggered {
				v.Status = Passed
			}
		}
	}
	return v
}

// judge applies the rule to one subject.
func (r Rule) judge(e env) Status {
	if r.When != nil && !r.When.Eval(e) {
		return NotTriggered
	}
	if r.Cond.Eval(e) == (r.Kind == Never) {
		return Broken
	}
	return Passed
}

// CountBroken is how many rules a change breaks.
func CountBroken(verdicts []Verdict) int {
	n := 0
	for _, v := range verdicts {
		if v.Status == Broken {
			n++
		}
	}
	return n
}
