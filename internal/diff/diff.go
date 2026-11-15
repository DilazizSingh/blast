// Package diff prices every customer under two pricing configs and reports
// what changed: per customer, per segment and in total.
package diff

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/rules/expr"
	"github.com/dilaziz/blast/internal/sim"
)

// Row is one customer priced both ways.
type Row struct {
	Customer model.Customer
	Plan     model.Plan // the customer's plan as the new config defines it
	Old, New sim.Invoice
}

// ChangeCents is how much more (or, when negative, less) the customer pays.
func (r Row) ChangeCents() int64 { return r.New.TotalCents - r.Old.TotalCents }

// Totals summarises a set of customers.
type Totals struct {
	Customers int   // how many were priced
	Affected  int   // how many pay a different amount
	OldCents  int64 // revenue per cycle before the change
	NewCents  int64 // revenue per cycle after it
}

func (t Totals) ChangeCents() int64 { return t.NewCents - t.OldCents }

func (t *Totals) add(r Row) {
	t.Customers++
	if r.ChangeCents() != 0 {
		t.Affected++
	}
	t.OldCents += r.Old.TotalCents
	t.NewCents += r.New.TotalCents
}

// Filter defines a segment: a label and the condition its customers meet.
type Filter struct {
	Name  string
	Where *expr.Expr // compiled against sim.Schema
}

// Segment is the part of the customer base one Filter selected. Segments may
// overlap: a legacy customer on annual billing counts in both.
type Segment struct {
	Name string
	Totals
	Rows []int // indexes into Result.Rows
}

// Result is the full comparison.
type Result struct {
	Rows []Row // in the order the customers were given
	Totals
	Segments []Segment
}

// Changed returns the affected customers, largest change first.
func (r Result) Changed() []Row {
	var rows []Row
	for _, row := range r.Rows {
		if row.ChangeCents() != 0 {
			rows = append(rows, row)
		}
	}
	slices.SortStableFunc(rows, func(a, b Row) int {
		return cmp.Compare(abs(b.ChangeCents()), abs(a.ChangeCents()))
	})
	return rows
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// Compare prices every customer under both configs.
func Compare(customers []model.Customer, before, after model.Config, filters []Filter) (Result, error) {
	oldPricing, err := sim.New(before)
	if err != nil {
		return Result{}, fmt.Errorf("pricing before the change: %w", err)
	}
	newPricing, err := sim.New(after)
	if err != nil {
		return Result{}, fmt.Errorf("pricing after the change: %w", err)
	}

	res := Result{Rows: make([]Row, 0, len(customers)), Segments: make([]Segment, len(filters))}
	for i, f := range filters {
		res.Segments[i].Name = f.Name
	}
	for i, c := range customers {
		row := Row{Customer: c, Plan: after.Plans[c.Plan]}
		if row.Old, err = oldPricing.Invoice(c); err != nil {
			return Result{}, fmt.Errorf("pricing before the change: %w", err)
		}
		if row.New, err = newPricing.Invoice(c); err != nil {
			return Result{}, fmt.Errorf("pricing after the change: %w", err)
		}
		res.Rows = append(res.Rows, row)
		res.add(row)

		who := sim.Facts{Customer: c, Plan: row.Plan}
		for j, f := range filters {
			if f.Where.Eval(who) {
				res.Segments[j].add(row)
				res.Segments[j].Rows = append(res.Segments[j].Rows, i)
			}
		}
	}
	return res, nil
}

// defaultFilters are the segments from the plan's Step 4.
var defaultFilters = []struct{ name, where string }{
	{"annual + referred", `billing == "annual" and referred`},
	{"monthly", `billing == "monthly"`},
	{"legacy plans", `legacy`},
	{"has coupon", `coupon_count > 0`},
}

// DefaultFilters are the segments reported when a rules file defines none.
func DefaultFilters() []Filter {
	filters := make([]Filter, len(defaultFilters))
	for i, d := range defaultFilters {
		where, err := expr.Compile(d.where, sim.Schema())
		if err != nil {
			panic(fmt.Sprintf("diff: built-in segment %q does not compile: %v", d.name, err))
		}
		filters[i] = Filter{Name: d.name, Where: where}
	}
	return filters
}
