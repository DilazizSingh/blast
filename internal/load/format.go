package load

import (
	"errors"
	"maps"
	"slices"

	"go.yaml.in/yaml/v4"

	"github.com/dilaziz/blast/internal/model"
)

// FormatConfig writes a pricing config as a YAML file that reads back as the
// same config. Plans are listed by name and discounts in their stacking
// order. comment, if any, heads the file.
func FormatConfig(cfg model.Config, comment string) ([]byte, error) {
	if len(cfg.Payment) > 0 {
		// Payment settings are kept flattened, which loses their layout.
		return nil, errors.New("a config with payment settings cannot be written back")
	}
	plans := mapping(0)
	for _, name := range slices.Sorted(maps.Keys(cfg.Plans)) {
		plan, fields := cfg.Plans[name], mapping(yaml.FlowStyle)
		for _, billing := range []model.Billing{model.Monthly, model.Annual} {
			if cents, ok := plan.Prices[billing]; ok {
				put(fields, string(billing), number(cents))
			}
		}
		if plan.Legacy {
			put(fields, "legacy", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		}
		put(plans, name, fields)
	}
	root := mapping(0)
	root.HeadComment = comment
	put(root, "plans", plans)

	if len(cfg.Discounts) > 0 {
		discounts := mapping(0)
		for _, d := range cfg.Discounts {
			fields := mapping(yaml.FlowStyle)
			if d.BasisPoints > 0 {
				put(fields, "percent", number(d.BasisPoints))
			} else {
				put(fields, "amount", number(d.AmountCents))
			}
			if d.When != "" {
				put(fields, "when", str(d.When))
			}
			put(discounts, d.Name, fields)
		}
		put(root, "discounts", discounts)
	}
	return yaml.Dump(root, yaml.WithIndent(2))
}

func mapping(style yaml.Style) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: style}
}

func put(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, str(key), value)
}

// str is a string scalar. Tagging it as a string makes the encoder quote
// whatever would otherwise read back as a number, a boolean or null.
func str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// number writes hundredths as a plain decimal: dollars, or a percentage.
func number(hundredths int64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: model.Decimal(hundredths)}
}
