package expr

import "fmt"

// parser is a recursive-descent parser with one method per grammar rule.
type parser struct {
	toks []token
	i    int
}

func parse(toks []token) (node, *Error) {
	p := &parser{toks: toks}
	root, err := p.or()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, &Error{t.pos, fmt.Sprintf("unexpected %q", t.text)}
	}
	return root, nil
}

// peek and next never run off the end: the final tEOF repeats forever.
func (p *parser) peek() token { return p.toks[min(p.i, len(p.toks)-1)] }

func (p *parser) next() token {
	t := p.peek()
	p.i++
	return t
}

func (p *parser) or() (node, *Error)  { return p.chain(tOr, p.and) }
func (p *parser) and() (node, *Error) { return p.chain(tAnd, p.not) }

// chain parses a left-associative run of operands joined by one keyword.
func (p *parser) chain(op kind, operand func() (node, *Error)) (node, *Error) {
	left, err := operand()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == op {
		joiner := p.next()
		right, err := operand()
		if err != nil {
			return nil, err
		}
		left = &binary{op: joiner, left: left, right: right}
	}
	return left, nil
}

func (p *parser) not() (node, *Error) {
	if p.peek().kind != tNot {
		return p.cmp()
	}
	keyword := p.next()
	operand, err := p.not()
	if err != nil {
		return nil, err
	}
	return &negation{pos: keyword.pos, operand: operand}, nil
}

// cmp allows a single comparison: "a == b == c" is rejected, not guessed at.
func (p *parser) cmp() (node, *Error) {
	left, err := p.atom()
	if err != nil {
		return nil, err
	}
	if k := p.peek().kind; k < tEq || k > tGe {
		return left, nil
	}
	op := p.next()
	right, err := p.atom()
	if err != nil {
		return nil, err
	}
	return &binary{op: op, left: left, right: right}, nil
}

func (p *parser) atom() (node, *Error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return &literal{t.pos, NumOf(t.num)}, nil
	case tStr:
		return &literal{t.pos, StrOf(t.text)}, nil
	case tTrue, tFalse:
		return &literal{t.pos, BoolOf(t.kind == tTrue)}, nil
	case tName:
		if p.peek().kind == tLParen {
			return p.call(t)
		}
		return &variable{t.pos, t.text}, nil
	case tLParen:
		inner, err := p.or()
		if err != nil {
			return nil, err
		}
		if closing := p.next(); closing.kind != tRParen {
			return nil, &Error{closing.pos, `expected ")"`}
		}
		return inner, nil
	}
	// t was not a value. Name the token it should have followed, if any.
	if before := p.i - 2; before >= 0 {
		return nil, &Error{t.pos, fmt.Sprintf("expected value after %q", p.toks[before].text)}
	}
	return nil, &Error{t.pos, "expected an expression"}
}

func (p *parser) call(name token) (node, *Error) {
	p.next() // the opening parenthesis
	c := &call{pos: name.pos, name: name.text}
	if p.peek().kind == tRParen {
		p.next()
		return c, nil
	}
	for {
		arg, err := p.or()
		if err != nil {
			return nil, err
		}
		c.args = append(c.args, arg)
		switch t := p.next(); t.kind {
		case tRParen:
			return c, nil
		case tComma:
		default:
			return nil, &Error{t.pos, fmt.Sprintf(`expected "," or ")" in the call to %s`, name.text)}
		}
	}
}
