package expr

import "fmt"

// node is one node of a parsed expression. check runs once at compile time
// and is what makes eval unable to fail: by the time eval runs, every name
// exists and every operand has the type its operator needs.
type node interface {
	offset() int // byte offset to blame in errors
	check(Schema) (Type, *Error)
	eval(Env) Value
}

type literal struct {
	pos int
	val Value
}

func (n *literal) offset() int                 { return n.pos }
func (n *literal) check(Schema) (Type, *Error) { return n.val.Type, nil }
func (n *literal) eval(Env) Value              { return n.val }

type variable struct {
	pos  int
	name string
}

func (n *variable) offset() int { return n.pos }

func (n *variable) check(s Schema) (Type, *Error) {
	t, ok := s.Vars[n.name]
	if !ok {
		return 0, &Error{n.pos, fmt.Sprintf("unknown name %q", n.name)}
	}
	return t, nil
}

func (n *variable) eval(env Env) Value { return env.Var(n.name) }

type call struct {
	pos  int
	name string
	args []node
}

func (n *call) offset() int { return n.pos }

func (n *call) check(s Schema) (Type, *Error) {
	f, ok := s.Funcs[n.name]
	if !ok {
		return 0, &Error{n.pos, fmt.Sprintf("unknown function %q", n.name)}
	}
	if len(n.args) != len(f.Params) {
		want := fmt.Sprintf("%d arguments", len(f.Params))
		if len(f.Params) == 1 {
			want = "1 argument"
		}
		return 0, &Error{n.pos, fmt.Sprintf("%s takes %s, got %d", n.name, want, len(n.args))}
	}
	for i, arg := range n.args {
		t, err := arg.check(s)
		if err != nil {
			return 0, err
		}
		if t != f.Params[i] {
			return 0, &Error{arg.offset(), fmt.Sprintf("%s expects %s here, got %s", n.name, f.Params[i], t)}
		}
	}
	return f.Result, nil
}

func (n *call) eval(env Env) Value {
	args := make([]Value, len(n.args))
	for i, arg := range n.args {
		args[i] = arg.eval(env)
	}
	return env.Call(n.name, args)
}

type negation struct {
	pos     int
	operand node
}

func (n *negation) offset() int { return n.pos }

func (n *negation) check(s Schema) (Type, *Error) {
	t, err := n.operand.check(s)
	if err != nil {
		return 0, err
	}
	if t != Bool {
		return 0, &Error{n.operand.offset(), fmt.Sprintf(`"not" needs true/false, got %s`, t)}
	}
	return Bool, nil
}

func (n *negation) eval(env Env) Value { return BoolOf(!n.operand.eval(env).Bool) }

// binary is "and", "or" or a comparison.
type binary struct {
	op          token
	left, right node
}

func (n *binary) offset() int { return n.op.pos }

func (n *binary) check(s Schema) (Type, *Error) {
	lt, err := n.left.check(s)
	if err != nil {
		return 0, err
	}
	rt, err := n.right.check(s)
	if err != nil {
		return 0, err
	}
	switch n.op.kind {
	case tAnd, tOr:
		for _, side := range []struct {
			n node
			t Type
		}{{n.left, lt}, {n.right, rt}} {
			if side.t != Bool {
				return 0, &Error{side.n.offset(), fmt.Sprintf("%q needs true/false on both sides, got %s", n.op.text, side.t)}
			}
		}
	case tEq, tNe:
		if lt != rt {
			return 0, &Error{n.op.pos, fmt.Sprintf("cannot compare %s with %s", lt, rt)}
		}
	default:
		if lt != Num || rt != Num {
			return 0, &Error{n.op.pos, fmt.Sprintf("%q compares numbers, got %s and %s", n.op.text, lt, rt)}
		}
	}
	return Bool, nil
}

func (n *binary) eval(env Env) Value {
	// "and" and "or" skip the right side when the left one decides.
	switch n.op.kind {
	case tAnd:
		return BoolOf(n.left.eval(env).Bool && n.right.eval(env).Bool)
	case tOr:
		return BoolOf(n.left.eval(env).Bool || n.right.eval(env).Bool)
	}
	l, r := n.left.eval(env), n.right.eval(env)
	switch n.op.kind {
	case tEq:
		return BoolOf(l == r)
	case tNe:
		return BoolOf(l != r)
	case tLt:
		return BoolOf(l.Num < r.Num)
	case tLe:
		return BoolOf(l.Num <= r.Num)
	case tGt:
		return BoolOf(l.Num > r.Num)
	default:
		return BoolOf(l.Num >= r.Num)
	}
}
