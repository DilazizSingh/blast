package load

import (
	"errors"

	"go.yaml.in/yaml/v4"

	"github.com/dilaziz/blast/internal/rules/expr"
)

// compile parses the expression held in a YAML scalar and pins any error to
// its character in the file.
func compile(file string, n *yaml.Node, schema expr.Schema) (*expr.Expr, error) {
	src, err := text(file, n, "a condition")
	if err != nil {
		return nil, err
	}
	e, err := expr.Compile(src, schema)
	if err != nil {
		located := &Error{File: file, Line: n.Line, Msg: err.Error()}
		if bad := (*expr.Error)(nil); errors.As(err, &bad) {
			located.Col, located.Msg = column(n, bad.Offset), bad.Msg
		}
		return nil, located
	}
	return e, nil
}

// column maps an offset inside a scalar's value to a column in the file. It
// is exact for one-line plain and quoted scalars. A block scalar's text
// starts on a later line, so it falls back to the column of the scalar itself.
func column(n *yaml.Node, offset int) int {
	switch n.Style &^ yaml.TaggedStyle {
	case 0:
		return n.Column + offset
	case yaml.SingleQuotedStyle, yaml.DoubleQuotedStyle:
		return n.Column + 1 + offset
	}
	return n.Column
}
