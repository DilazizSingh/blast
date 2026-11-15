// Package report renders a finished check. Every renderer draws from the same
// Report, and nothing here computes a price: it only formats what sim, diff
// and rules already decided.
package report

import (
	"fmt"
	"strings"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/rules"
)

// Pitch is the one-line description of the tool.
const Pitch = "See which customers a pricing change hits before you ship it."

// maxNamed is how many customers one rule line names before summarising.
const maxNamed = 8

// Report is everything a finished check has to say.
type Report struct {
	Before, After string // the pricing files compared, as the user named them
	Result        diff.Result
	Changes       []diff.Change
	Verdicts      []rules.Verdict
}

// Broken is how many rules the change breaks.
func (r Report) Broken() int { return rules.CountBroken(r.Verdicts) }

// ExitCode is the exit status the report calls for: 1 when a rule broke.
func (r Report) ExitCode() int {
	if r.Broken() > 0 {
		return 1
	}
	return 0
}

// brokenBy maps each customer to the rules they break.
func (r Report) brokenBy() map[string][]string {
	out := map[string][]string{}
	for _, v := range r.Verdicts {
		for _, id := range v.Customers {
			out[id] = append(out[id], v.Rule.Name)
		}
	}
	return out
}

// Radius formats the share of customers a change touches, such as "50%". A
// sliver shows as "<1%" rather than a reassuring "0%", and anything short of
// everyone never rounds up to "100%".
func Radius(t diff.Totals) string {
	if t.Customers == 0 {
		return "0%"
	}
	percent := (t.Affected*200 + t.Customers) / (2 * t.Customers)
	switch {
	case percent == 0 && t.Affected > 0:
		return "<1%"
	case percent == 100 && t.Affected < t.Customers:
		return ">99%"
	}
	return fmt.Sprintf("%d%%", percent)
}

// ChangePercent formats the revenue change relative to the old revenue, to
// one decimal place: "-12.3%". There is no percentage of nothing, so a zero
// base gives "n/a".
func ChangePercent(t diff.Totals) string {
	if t.OldCents == 0 {
		return "n/a"
	}
	change, sign := t.ChangeCents(), ""
	switch {
	case change < 0:
		change, sign = -change, "-"
	case change > 0:
		sign = "+"
	}
	tenths := (change*1000 + t.OldCents/2) / t.OldCents
	return fmt.Sprintf("%s%d.%d%%", sign, tenths/10, tenths%10)
}

// Detail says in a few words how a rule came out.
func Detail(v rules.Verdict) string {
	switch {
	case v.Status == rules.Passed:
		return "ok"
	case v.Status == rules.NotTriggered:
		return "not triggered"
	case len(v.Customers) == 0 && v.Rule.Kind == rules.Require:
		return "needs " + v.Rule.Cond.String()
	case len(v.Customers) == 0:
		return "must not hold: " + v.Rule.Cond.String()
	}
	named := v.Customers[:min(len(v.Customers), maxNamed)]
	detail := plural(len(v.Customers), "customer") + ": " + strings.Join(named, ", ")
	if rest := len(v.Customers) - len(named); rest > 0 {
		detail += fmt.Sprintf(", +%s more", model.Thousands(int64(rest)))
	}
	return detail
}

// planLabel describes what a customer is billed for: "pro annual", or
// "pro annual ×12" for twelve seats.
func planLabel(c model.Customer) string {
	label := c.Plan + " " + string(c.Billing)
	if c.Units() > 1 {
		label += " ×" + model.Thousands(c.Units())
	}
	return label
}

// plural formats a count with its noun: "1 customer", "12,000 customers".
func plural(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return model.Thousands(int64(n)) + " " + noun
}
