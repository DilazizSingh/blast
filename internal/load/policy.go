package load

import (
	"go.yaml.in/yaml/v4"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/rules"
	"github.com/dilaziz/blast/internal/rules/expr"
	"github.com/dilaziz/blast/internal/sim"
)

// parsePolicy reads a rules file: the rules, and optionally the segments
// reports are grouped by. Without a segments section the built-in ones apply.
func parsePolicy(file string, data []byte) ([]rules.Rule, []diff.Filter, error) {
	root, err := parseYAML(file, data)
	if err != nil {
		return nil, nil, err
	}
	sections, err := entries(file, root, "a rules file")
	if err != nil {
		return nil, nil, err
	}
	var (
		policy   []rules.Rule
		segments []diff.Filter
	)
	for _, s := range sections {
		switch s.key.Value {
		case "rules":
			policy, err = parseList(file, s.val, "rules", parseRule)
		case "segments":
			segments, err = parseList(file, s.val, "segments", parseSegment)
		default:
			err = errAt(file, s.key, "unknown section %q (expected rules or segments)", s.key.Value)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if segments == nil {
		segments = diff.DefaultFilters()
	}
	return policy, segments, nil
}

// named is a list element that is identified by a name.
type named interface{ rules.Rule | diff.Filter }

// parseList reads a YAML list of named things, refusing duplicate names.
func parseList[T named](file string, n *yaml.Node, what string, parse func(string, *yaml.Node) (string, T, error)) ([]T, error) {
	nodes, err := items(file, n, what)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(nodes))
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		name, item, err := parse(file, node)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, errAt(file, node, "%q is defined twice", name)
		}
		seen[name] = true
		out = append(out, item)
	}
	return out, nil
}

func parseRule(file string, n *yaml.Node) (string, rules.Rule, error) {
	fields, err := entries(file, n, "a rule")
	if err != nil {
		return "", rules.Rule{}, err
	}
	var (
		r      rules.Rule
		schema = rules.Schema()
	)
	for _, f := range fields {
		switch key := f.key.Value; key {
		case "name":
			r.Name, err = text(file, f.val, "a rule name")
		case "never", "require":
			if r.Cond != nil {
				err = errAt(file, f.key, "a rule has either never or require, not both")
				break
			}
			r.Kind = map[string]rules.Kind{"never": rules.Never, "require": rules.Require}[key]
			r.Cond, err = compile(file, f.val, schema)
		case "when":
			r.When, err = compile(file, f.val, schema)
		default:
			err = errAt(file, f.key, "unknown rule field %q (expected name, never, require or when)", key)
		}
		if err != nil {
			return "", rules.Rule{}, err
		}
	}
	switch {
	case r.Name == "":
		return "", rules.Rule{}, errAt(file, n, `a rule needs a "name"`)
	case r.Cond == nil:
		return "", rules.Rule{}, errAt(file, n, "rule %q needs a never or a require", r.Name)
	}
	return r.Name, r, nil
}

func parseSegment(file string, n *yaml.Node) (string, diff.Filter, error) {
	fields, err := entries(file, n, "a segment")
	if err != nil {
		return "", diff.Filter{}, err
	}
	var seg diff.Filter
	for _, f := range fields {
		switch key := f.key.Value; key {
		case "name":
			seg.Name, err = text(file, f.val, "a segment name")
		case "where":
			// Segments group customers, so they read customer facts only.
			seg.Where, err = compile(file, f.val, expr.Schema{Vars: sim.Schema().Vars})
		default:
			err = errAt(file, f.key, "unknown segment field %q (expected name or where)", key)
		}
		if err != nil {
			return "", diff.Filter{}, err
		}
	}
	switch {
	case seg.Name == "":
		return "", diff.Filter{}, errAt(file, n, `a segment needs a "name"`)
	case seg.Where == nil:
		return "", diff.Filter{}, errAt(file, n, `segment %q needs a "where"`, seg.Name)
	}
	return seg.Name, seg, nil
}
