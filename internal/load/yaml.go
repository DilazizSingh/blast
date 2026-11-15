package load

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"

	"github.com/dilaziz/blast/internal/model"
)

// Files are walked as yaml.Node trees rather than decoded into structs,
// because only the nodes remember which line each value came from.

// parseYAML returns the root of a YAML file that holds exactly one document.
func parseYAML(file string, data []byte) (*yaml.Node, error) {
	var docs []yaml.Node
	if err := yaml.Load(data, &docs, yaml.WithAllDocuments()); err != nil {
		return nil, yamlError(file, err)
	}
	switch {
	case len(docs) == 0 || len(docs[0].Content) == 0:
		return nil, &Error{File: file, Msg: "file is empty"}
	case len(docs) > 1:
		// A reader that took only the first document would silently drop
		// whatever pricing the rest of the file declares.
		return nil, errAt(file, &docs[1], "a second YAML document starts here; put everything in one")
	}
	root := docs[0].Content[0]
	if err := refuseAliases(file, root); err != nil {
		return nil, err
	}
	return root, nil
}

// yamlError keeps the parser's message but reports it in blast's own shape.
func yamlError(file string, err error) error {
	var syntax *yaml.LoadError
	if errors.As(err, &syntax) {
		return &Error{File: file, Line: syntax.Mark.Line, Msg: syntax.Message}
	}
	return &Error{File: file, Msg: strings.TrimPrefix(err.Error(), "yaml: ")}
}

// refuseAliases rejects anchors and aliases. blast's files have no use for
// them, and nested aliases are how a file of a few hundred bytes expands
// into gigabytes once something walks it.
func refuseAliases(file string, n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errAt(file, n, "YAML anchors and aliases are not supported")
	}
	for _, child := range n.Content {
		if err := refuseAliases(file, child); err != nil {
			return err
		}
	}
	return nil
}

func errAt(file string, n *yaml.Node, format string, args ...any) error {
	return &Error{File: file, Line: n.Line, Msg: fmt.Sprintf(format, args...)}
}

// printable reports whether s is safe to show as it is. Names and conditions
// from input files end up on terminals and in pull-request comments, where a
// control character could repaint the screen and a bidirectional override
// could reorder what a reviewer reads.
func printable(s string) bool {
	return utf8.ValidString(s) && strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) < 0
}

type entry struct{ key, val *yaml.Node }

// entries lists a mapping's pairs in file order. what names the mapping for
// the error raised when n is something else.
func entries(file string, n *yaml.Node, what string) ([]entry, error) {
	if n.Kind != yaml.MappingNode {
		return nil, errAt(file, n, "%s must be a mapping of names to settings", what)
	}
	seen := make(map[string]bool, len(n.Content)/2)
	out := make([]entry, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		if key.Kind != yaml.ScalarNode || key.Value == "" || !printable(key.Value) {
			return nil, errAt(file, key, "a name must be plain, visible text")
		}
		if seen[key.Value] {
			return nil, errAt(file, key, "%q is defined twice", key.Value)
		}
		seen[key.Value] = true
		out = append(out, entry{key, n.Content[i+1]})
	}
	return out, nil
}

// items lists a sequence's elements.
func items(file string, n *yaml.Node, what string) ([]*yaml.Node, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, errAt(file, n, "%s must be a list", what)
	}
	return n.Content, nil
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

// text reads a piece of text: a name or a condition. A folded scalar (">")
// may spread a long condition over several lines; its trailing newline is
// dropped, and anything else that is not visible on one line is refused.
func text(file string, n *yaml.Node, what string) (string, error) {
	value := strings.TrimRight(n.Value, " \n")
	switch {
	case n.Kind != yaml.ScalarNode || value == "":
		return "", errAt(file, n, "expected %s", what)
	case !printable(value):
		return "", errAt(file, n, "%s must be visible text on one line", what)
	}
	return value, nil
}

// flatten records every scalar under n as a dotted path, so a section blast
// does not interpret can still be compared between two files.
func flatten(file, prefix string, n *yaml.Node, into map[string]string) error {
	switch n.Kind {
	case yaml.MappingNode:
		pairs, err := entries(file, n, prefix)
		if err != nil {
			return err
		}
		for _, e := range pairs {
			if err := flatten(file, prefix+"."+e.key.Value, e.val, into); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			if err := flatten(file, prefix+"."+strconv.Itoa(i), item, into); err != nil {
				return err
			}
		}
	default:
		if !printable(n.Value) {
			return errAt(file, n, "%s must be visible text on one line", prefix)
		}
		into[prefix] = n.Value
	}
	return nil
}
