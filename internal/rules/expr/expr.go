// Package expr is blast's small expression language: the conditions written
// after `when:`, `never:` and `require:`.
//
// An expression is compiled once against a Schema, which type-checks it, and
// can then be evaluated any number of times without the possibility of a
// runtime error. Numbers are fixed-point hundredths, so a dollar amount in an
// expression compares exactly against money held in cents.
//
//	expr := or
//	or   := and ("or" and)*
//	and  := not ("and" not)*
//	not  := "not" not | cmp
//	cmp  := atom (("==" | "!=" | "<" | ">" | "<=" | ">=") atom)?
//	atom := number | string | "true" | "false" | name | call | "(" expr ")"
//	name := ident ("." ident)*
//	call := name "(" (expr ("," expr)*)? ")"
package expr

import (
	"fmt"
	"unicode/utf8"
)

// Type is the type of a value: every expression has exactly one.
type Type int

const (
	Bool Type = iota + 1
	Num
	Str
)

func (t Type) String() string {
	switch t {
	case Bool:
		return "true/false"
	case Num:
		return "a number"
	case Str:
		return "text"
	}
	return "nothing"
}

// Value is one typed value. Only the field matching Type is meaningful.
type Value struct {
	Type Type
	Bool bool
	Num  int64 // hundredths: 712.80 is 71280, a count of 2 is 200
	Str  string
}

func BoolOf(b bool) Value { return Value{Type: Bool, Bool: b} }

// NumOf wraps hundredths, which is what an amount in cents already is.
func NumOf(hundredths int64) Value { return Value{Type: Num, Num: hundredths} }

// CountOf wraps a whole number such as a number of coupons.
func CountOf(n int) Value { return NumOf(int64(n) * 100) }

func StrOf(s string) Value { return Value{Type: Str, Str: s} }

// Func is the signature of something an expression may call.
type Func struct {
	Params []Type
	Result Type
}

// Schema declares the names an expression may refer to.
type Schema struct {
	Vars  map[string]Type
	Funcs map[string]Func
}

// Env supplies values during evaluation. It is only ever asked for names the
// Schema declared, with arguments of the declared types.
type Env interface {
	Var(name string) Value
	Call(name string, args []Value) Value
}

// Error is a syntax or type error at a position in the source.
type Error struct {
	Offset int // characters before the problem; 0 is the first character
	Msg    string
}

func (e *Error) Error() string { return fmt.Sprintf("column %d: %s", e.Offset+1, e.Msg) }

// Expr is a compiled condition.
type Expr struct {
	src  string
	root node
	vars []string
}

// maxLen bounds the source so that no input, however deeply nested, can
// exhaust the stack of the recursive-descent parser.
const maxLen = 4096

// Compile parses src and checks it against schema. The result is always a
// true/false condition. On failure the error is an *Error.
func Compile(src string, schema Schema) (*Expr, error) {
	e, err := compile(src, schema)
	if err != nil {
		// Positions are tracked in bytes and reported in characters.
		err.Offset = utf8.RuneCountInString(src[:err.Offset])
		return nil, err
	}
	return e, nil
}

func compile(src string, schema Schema) (*Expr, *Error) {
	if len(src) > maxLen {
		return nil, &Error{0, fmt.Sprintf("expression is longer than %d characters", maxLen)}
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	root, err := parse(toks)
	if err != nil {
		return nil, err
	}
	typ, err := root.check(schema)
	if err != nil {
		return nil, err
	}
	if typ != Bool {
		return nil, &Error{0, fmt.Sprintf("a condition must be true/false, but this is %s", typ)}
	}

	e := &Expr{src: src, root: root}
	for i, t := range toks {
		// The token list ends in EOF, so a name always has a successor.
		if t.kind == tName && toks[i+1].kind != tLParen {
			e.vars = append(e.vars, t.text)
		}
	}
	return e, nil
}

// Eval reports whether the condition holds in env.
func (e *Expr) Eval(env Env) bool { return e.root.eval(env).Bool }

// Vars lists the variables the expression reads, in source order.
func (e *Expr) Vars() []string { return e.vars }

// String returns the source the expression was compiled from.
func (e *Expr) String() string { return e.src }
