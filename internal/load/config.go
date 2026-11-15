package load

import (
	"fmt"

	"go.yaml.in/yaml/v4"

	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/sim"
)

// parseConfig reads a pricing file. Unknown sections and fields are errors:
// a misspelt "discount:" that silently priced everyone at list would be the
// exact kind of mistake blast exists to catch.
func parseConfig(file string, data []byte) (model.Config, error) {
	root, err := parseYAML(file, data)
	if err != nil {
		return model.Config{}, err
	}
	sections, err := entries(file, root, "a pricing file")
	if err != nil {
		return model.Config{}, err
	}
	var cfg model.Config
	for _, s := range sections {
		switch s.key.Value {
		case "plans":
			cfg.Plans, err = parsePlans(file, s.val)
		case "discounts":
			cfg.Discounts, err = parseDiscounts(file, s.val)
		case "payment":
			cfg.Payment = map[string]string{}
			err = flatten(file, "payment", s.val, cfg.Payment)
		default:
			err = errAt(file, s.key, "unknown section %q (expected plans, discounts or payment)", s.key.Value)
		}
		if err != nil {
			return model.Config{}, err
		}
	}
	if len(cfg.Plans) == 0 {
		return model.Config{}, &Error{File: file, Msg: "no plans defined"}
	}
	return cfg, nil
}

func parsePlans(file string, n *yaml.Node) (map[string]model.Plan, error) {
	named, err := entries(file, n, "plans")
	if err != nil {
		return nil, err
	}
	plans := make(map[string]model.Plan, len(named))
	for _, e := range named {
		if plans[e.key.Value], err = parsePlan(file, e); err != nil {
			return nil, err
		}
	}
	return plans, nil
}

func parsePlan(file string, e entry) (model.Plan, error) {
	name := e.key.Value
	fields, err := entries(file, e.val, fmt.Sprintf("plan %q", name))
	if err != nil {
		return model.Plan{}, err
	}
	plan := model.Plan{Prices: map[model.Billing]int64{}}
	for _, f := range fields {
		switch key := f.key.Value; {
		case model.Billing(key).Valid():
			plan.Prices[model.Billing(key)], err = money(file, f.val)
		case key == "legacy":
			plan.Legacy, err = boolean(file, f.val)
		default:
			err = errAt(file, f.key, "unknown plan field %q (expected monthly, annual or legacy)", key)
		}
		if err != nil {
			return model.Plan{}, err
		}
	}
	if len(plan.Prices) == 0 {
		return model.Plan{}, errAt(file, e.key, "plan %q has no monthly or annual price", name)
	}
	return plan, nil
}

func parseDiscounts(file string, n *yaml.Node) ([]model.Discount, error) {
	named, err := entries(file, n, "discounts")
	if err != nil {
		return nil, err
	}
	discounts := make([]model.Discount, 0, len(named))
	for _, e := range named {
		d, err := parseDiscount(file, e)
		if err != nil {
			return nil, err
		}
		discounts = append(discounts, d)
	}
	return discounts, nil
}

func parseDiscount(file string, e entry) (model.Discount, error) {
	d := model.Discount{Name: e.key.Value}
	fields, err := entries(file, e.val, fmt.Sprintf("discount %q", d.Name))
	if err != nil {
		return model.Discount{}, err
	}
	for _, f := range fields {
		switch f.key.Value {
		case "percent":
			d.BasisPoints, err = hundredths(file, f.val)
			if err == nil && (d.BasisPoints <= 0 || d.BasisPoints > 100_00) {
				err = errAt(file, f.val, "percent must be above 0 and at most 100")
			}
		case "amount":
			d.AmountCents, err = money(file, f.val)
			if err == nil && d.AmountCents == 0 {
				err = errAt(file, f.val, "amount must be above 0")
			}
		case "when":
			d.When, err = condition(file, f.val)
		default:
			err = errAt(file, f.key, "unknown discount field %q (expected percent, amount or when)", f.key.Value)
		}
		if err != nil {
			return model.Discount{}, err
		}
	}
	if (d.BasisPoints > 0) == (d.AmountCents > 0) {
		return model.Discount{}, errAt(file, e.key, "discount %q needs exactly one of percent or amount", d.Name)
	}
	return d, nil
}

// condition reads a discount's `when:` expression. It is compiled here only
// to reject mistakes with a file position; the model keeps the source text.
func condition(file string, n *yaml.Node) (string, error) {
	cond, err := compile(file, n, sim.Schema())
	if err != nil {
		return "", err
	}
	return cond.String(), nil
}
