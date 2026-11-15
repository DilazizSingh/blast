// Package sim prices customers: one customer and one pricing config in, one
// invoice out. The rules it follows are written up in docs/pricing-model.md.
package sim

import (
	"fmt"

	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/rules/expr"
)

// Step is one discount taken off an invoice, with the running total on
// either side of it.
type Step struct {
	Discount    string
	BeforeCents int64
	AfterCents  int64
}

// Invoice is what one customer is charged for one billing cycle, and why.
type Invoice struct {
	BaseCents  int64  // the plan's list price
	TotalCents int64  // what is charged
	Steps      []Step // discounts, in the order they were applied
}

// Engine prices customers against one pricing config.
type Engine struct {
	cfg model.Config
	// conds runs parallel to cfg.Discounts; nil where a discount has no `when:`.
	conds []*expr.Expr
}

// New prepares an engine, compiling every discount condition once.
func New(cfg model.Config) (*Engine, error) {
	schema := Schema()
	conds := make([]*expr.Expr, len(cfg.Discounts))
	for i, d := range cfg.Discounts {
		if d.When == "" {
			continue
		}
		cond, err := expr.Compile(d.When, schema)
		if err != nil {
			return nil, fmt.Errorf("discount %q: when: %w", d.Name, err)
		}
		conds[i] = cond
	}
	return &Engine{cfg: cfg, conds: conds}, nil
}

// Invoice prices one customer. Discounts are taken in the order the config
// declares them, each off the running total, each at most once.
func (e *Engine) Invoice(c model.Customer) (Invoice, error) {
	plan, ok := e.cfg.Plans[c.Plan]
	if !ok {
		return Invoice{}, fmt.Errorf("customer %q: unknown plan %q", c.ID, c.Plan)
	}
	base, ok := plan.Prices[c.Billing]
	if !ok {
		return Invoice{}, fmt.Errorf("customer %q: plan %q has no %s price", c.ID, c.Plan, c.Billing)
	}

	inv := Invoice{BaseCents: base, TotalCents: base}
	who := Facts{Customer: c, Plan: plan}
	for i, d := range e.cfg.Discounts {
		automatic := e.conds[i] != nil && e.conds[i].Eval(who)
		if !automatic && !c.HasCoupon(d.Name) {
			continue
		}
		after := apply(d, inv.TotalCents)
		inv.Steps = append(inv.Steps, Step{Discount: d.Name, BeforeCents: inv.TotalCents, AfterCents: after})
		inv.TotalCents = after
	}
	return inv, nil
}

// apply takes one discount off an amount. A percent discount rounds the
// remaining price half up to a whole cent; a fixed amount stops at zero.
func apply(d model.Discount, cents int64) int64 {
	if d.BasisPoints > 0 {
		return (cents*(100_00-d.BasisPoints) + 50_00) / 100_00
	}
	return max(cents-d.AmountCents, 0)
}
