package diff

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/dilaziz/blast/internal/model"
)

// Change is one setting that differs between two pricing configs.
type Change struct {
	Path string // dotted, such as "discounts.REFERRAL20.percent"
	Old  string // "" when the setting did not exist before
	New  string // "" when the setting no longer exists
}

// Configs lists every setting that differs between two pricing configs,
// sorted by path.
func Configs(before, after model.Config) []Change {
	old, cur := settings(before), settings(after)
	paths := slices.Collect(maps.Keys(old))
	for path := range cur {
		if _, ok := old[path]; !ok {
			paths = append(paths, path)
		}
	}

	var changes []Change
	for _, path := range paths {
		was, existed := old[path]
		is, exists := cur[path]
		if was != is || existed != exists {
			changes = append(changes, Change{Path: path, Old: was, New: is})
		}
	}
	// Stacking follows declaration order, so reordering discounts changes
	// invoices without changing any single setting.
	if was, is := sharedOrder(before, after), sharedOrder(after, before); was != is {
		changes = append(changes, Change{Path: "discounts.order", Old: was, New: is})
	}
	slices.SortFunc(changes, func(a, b Change) int { return cmp.Compare(a.Path, b.Path) })
	return changes
}

// settings flattens a config to dotted paths and printable values.
func settings(cfg model.Config) map[string]string {
	out := maps.Clone(cfg.Payment)
	if out == nil {
		out = map[string]string{}
	}
	for name, plan := range cfg.Plans {
		for period, cents := range plan.Prices {
			out["plans."+name+"."+string(period)] = model.Dollars(cents)
		}
		if plan.Legacy {
			out["plans."+name+".legacy"] = "true"
		}
	}
	for _, d := range cfg.Discounts {
		prefix := "discounts." + d.Name + "."
		if d.BasisPoints > 0 {
			out[prefix+"percent"] = model.Decimal(d.BasisPoints) + "%"
		} else {
			out[prefix+"amount"] = model.Dollars(d.AmountCents)
		}
		if d.When != "" {
			out[prefix+"when"] = d.When
		}
	}
	return out
}

// sharedOrder lists the discounts both configs declare, in a's order.
func sharedOrder(a, b model.Config) string {
	var names []string
	for _, d := range a.Discounts {
		if b.HasDiscount(d.Name) {
			names = append(names, d.Name)
		}
	}
	return strings.Join(names, ", ")
}
