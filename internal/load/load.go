// Package load reads blast's input files and reports every problem by file
// and line, so a broken input is fixed in one look.
package load

import (
	"fmt"
	"os"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/rules"
)

// Error is a problem in an input file, pinned to where it was found.
type Error struct {
	File string
	Line int // 1-based; 0 when the problem is the file as a whole
	Col  int // 1-based; 0 when only the line is known
	Msg  string
}

func (e *Error) Error() string {
	switch {
	case e.Line == 0:
		return e.File + ": " + e.Msg
	case e.Col == 0:
		return fmt.Sprintf("%s line %d: %s", e.File, e.Line, e.Msg)
	default:
		return fmt.Sprintf("%s:%d:%d %s", e.File, e.Line, e.Col, e.Msg)
	}
}

// Inputs names the files of one check.
type Inputs struct {
	Before    string // pricing as it is today
	After     string // pricing as proposed
	Customers string
	Rules     string
	// Read fetches a file's contents; os.ReadFile when nil. `--base` uses
	// it to serve the "before" pricing from git instead of from disk.
	Read func(path string) ([]byte, error)
}

// Scenario is everything one check needs, loaded and cross-checked.
type Scenario struct {
	Before, After model.Config
	Customers     []model.Customer
	Rules         []rules.Rule
	Segments      []diff.Filter // the rules file's segments, or the built-in ones
}

// Open loads every input and makes sure the customers can be priced under
// both configs.
func Open(in Inputs) (*Scenario, error) {
	read := in.Read
	if read == nil {
		read = os.ReadFile
	}
	before, err := readConfig(read, in.Before)
	if err != nil {
		return nil, err
	}
	after, err := readConfig(read, in.After)
	if err != nil {
		return nil, err
	}
	data, err := read(in.Customers)
	if err != nil {
		return nil, err
	}
	customers, lines, err := parseCustomers(in.Customers, data)
	if err != nil {
		return nil, err
	}
	data, err = read(in.Rules)
	if err != nil {
		return nil, err
	}
	policy, segments, err := parsePolicy(in.Rules, data)
	if err != nil {
		return nil, err
	}
	s := &Scenario{Before: before, After: after, Customers: customers, Rules: policy, Segments: segments}
	if err := s.crossCheck(in, lines); err != nil {
		return nil, err
	}
	return s, nil
}

func readConfig(read func(string) ([]byte, error), path string) (model.Config, error) {
	data, err := read(path)
	if err != nil {
		return model.Config{}, err
	}
	return parseConfig(path, data)
}

// crossCheck rejects customers that either config cannot price. A coupon only
// has to exist on one side: adding or retiring a coupon is a change blast is
// meant to simulate, while a coupon neither side knows is a typo.
func (s *Scenario) crossCheck(in Inputs, lines []int) error {
	sides := []struct {
		file string
		cfg  model.Config
	}{{in.Before, s.Before}, {in.After, s.After}}

	for i, c := range s.Customers {
		fail := func(format string, args ...any) error {
			return &Error{File: in.Customers, Line: lines[i], Msg: fmt.Sprintf(format, args...)}
		}
		for _, side := range sides {
			plan, ok := side.cfg.Plans[c.Plan]
			if !ok {
				return fail("unknown plan %q (not defined in %s)", c.Plan, side.file)
			}
			price, ok := plan.Prices[c.Billing]
			if !ok {
				return fail("plan %q has no %s price in %s", c.Plan, c.Billing, side.file)
			}
			if price > model.MaxCents/c.Units() {
				return fail("%s × %s on plan %q in %s is over the %s limit for one invoice",
					model.Dollars(price), model.Thousands(c.Units()), c.Plan, side.file, model.Dollars(model.MaxCents))
			}
		}
		for _, coupon := range c.Coupons {
			if !s.Before.HasDiscount(coupon) && !s.After.HasDiscount(coupon) {
				return fail("unknown coupon %q (not defined in either pricing file)", coupon)
			}
		}
	}
	return nil
}
