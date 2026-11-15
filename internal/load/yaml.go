package load

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dilaziz/blast/internal/model"
)

// Files are walked as yaml.Node trees rather than decoded into structs,
// because only the nodes remember which line each value came from.

// parseYAML returns the top-level mapping of a YAML file.
func parseYAML(file string, data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, yamlError(file, err)
	}
	if len(doc.Content) == 0 {
		return nil, &Error{File: file, Msg: "file is empty"}
	}
	return doc.Content[0], nil
}

// yamlError keeps the parser's message but reports it in blast's own shape.
func yamlError(file string, err error) error {
	var syntax *yaml.LoadError
	if errors.As(err, &syntax) {
		return &Error{File: file, Line: syntax.Mark.Line, Msg: syntax.Message}
	}
	return &Error{File: file, Msg: err.Error()}
}

func errAt(file string, n *yaml.Node, format string, args ...any) error {
	return &Error{File: file, Line: n.Line, Msg: fmt.Sprintf(format, args...)}
}

type entry struct{ key, val *yaml.Node }

// entries lists a mapping's pairs in file order. what names the mapping for
// the error raised when n is something else.
func entries(file string, n *yaml.Node, what string) ([]entry, error) {
	n = resolve(n)
	if n.Kind != yaml.MappingNode {
		return nil, errAt(file, n, "%s must be a mapping of names to settings", what)
	}
	seen := make(map[string]bool, len(n.Content)/2)
	out := make([]entry, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		if seen[key.Value] {
			return nil, errAt(file, key, "%q is defined twice", key.Value)
		}
		seen[key.Value] = true
		out = append(out, entry{key, resolve(n.Content[i+1])})
	}
	return out, nil
}

// items lists a sequence's elements.
func items(file string, n *yaml.Node, what string) ([]*yaml.Node, error) {
	n = resolve(n)
	if n.Kind != yaml.SequenceNode {
		return nil, errAt(file, n, "%s must be a list", what)
	}
	return n.Content, nil
}

func resolve(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.AliasNode {
		return n.Alias
	}
	return n
}

func hundredths(file string, n *yaml.Node) (int64, error) {
	if n.Kind != yaml.ScalarNode {
		return 0, errAt(file, n, "expected a number")
	}
	v, err := model.ParseHundredths(n.Value)
	if err != nil {
		return 0, errAt(file, n, "%v", err)
	}
	return v, nil
}

// money reads a dollar amount as cents.
func money(file string, n *yaml.Node) (int64, error) {
	cents, err := hundredths(file, n)
	if err == nil && cents < 0 {
		err = errAt(file, n, "an amount cannot be negative")
	}
	return cents, err
}

func boolean(file string, n *yaml.Node) (bool, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!bool" {
		return false, errAt(file, n, "expected true or false")
	}
	return strings.EqualFold(n.Value, "true"), nil
}

func text(file string, n *yaml.Node, what string) (string, error) {
	if n.Kind != yaml.ScalarNode || strings.TrimSpace(n.Value) == "" {
		return "", errAt(file, n, "expected %s", what)
	}
	return n.Value, nil
}

// flatten records every scalar under n as a dotted path, so a section blast
// does not interpret can still be compared between two files.
func flatten(prefix string, n *yaml.Node, into map[string]string) {
	n = resolve(n)
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			flatten(prefix+"."+n.Content[i].Value, n.Content[i+1], into)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			flatten(prefix+"."+strconv.Itoa(i), item, into)
		}
	default:
		into[prefix] = n.Value
	}
}
