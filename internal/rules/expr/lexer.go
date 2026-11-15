package expr

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/dilaziz/blast/internal/model"
)

type kind int

const (
	tEOF kind = iota
	tNum
	tStr
	tName // an identifier or dotted path: referred, customer.discount_count
	tAnd
	tOr
	tNot
	tTrue
	tFalse
	tEq
	tNe
	tLt
	tLe
	tGt
	tGe
	tLParen
	tRParen
	tComma
)

type token struct {
	kind kind
	pos  int    // byte offset into the source
	text string // as written; for tStr, what is between the quotes
	num  int64  // tNum only, in hundredths
}

var keywords = map[string]kind{"and": tAnd, "or": tOr, "not": tNot, "true": tTrue, "false": tFalse}

// Two-character operators come first so "<=" is not read as "<" then "=".
var operators = []struct {
	text string
	kind kind
}{
	{"==", tEq}, {"!=", tNe}, {"<=", tLe}, {">=", tGe},
	{"<", tLt}, {">", tGt}, {"(", tLParen}, {")", tRParen}, {",", tComma},
}

// lex splits src into tokens, always ending with tEOF.
func lex(src string) ([]token, *Error) {
	var toks []token
	i := 0
	for {
		for i < len(src) && strings.IndexByte(" \t\r\n", src[i]) >= 0 {
			i++
		}
		if i == len(src) {
			return append(toks, token{kind: tEOF, pos: i}), nil
		}
		tok, err := scan(src, i)
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
		i += tok.width()
	}
}

// width is how many source bytes the token spans; strings also span their quotes.
func (t token) width() int {
	if t.kind == tStr {
		return len(t.text) + 2
	}
	return len(t.text)
}

// scan reads the one token that starts at src[i].
func scan(src string, i int) (token, *Error) {
	rest := src[i:]
	switch c := rest[0]; {
	case isLetter(c):
		name := rest[:nameLen(rest)]
		k, isKeyword := keywords[name]
		if !isKeyword {
			k = tName
		}
		return token{kind: k, pos: i, text: name}, nil
	case isDigit(c) || c == '-':
		return scanNumber(rest, i)
	case c == '"':
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return token{}, &Error{i, `text is missing its closing "`}
		}
		return token{kind: tStr, pos: i, text: rest[1 : end+1]}, nil
	case c == '=' && !strings.HasPrefix(rest, "=="):
		return token{}, &Error{i, `use "==" to compare`}
	}
	for _, op := range operators {
		if strings.HasPrefix(rest, op.text) {
			return token{kind: op.kind, pos: i, text: op.text}, nil
		}
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return token{}, &Error{i, fmt.Sprintf("unexpected character %q", string(r))}
}

// scanNumber takes every digit and dot it can and lets the money parser be
// the judge, so "1.2.3" and "0.999" fail with the same messages as in a
// pricing file. There is no subtraction, so a minus always starts a number.
func scanNumber(rest string, pos int) (token, *Error) {
	n := 1
	for n < len(rest) && (isDigit(rest[n]) || rest[n] == '.') {
		n++
	}
	num, err := model.ParseHundredths(rest[:n])
	if err != nil {
		return token{}, &Error{pos, err.Error()}
	}
	return token{kind: tNum, pos: pos, text: rest[:n], num: num}, nil
}

// nameLen measures an identifier, continuing through a dot only when another
// identifier follows it.
func nameLen(s string) int {
	n := 0
	for n < len(s) && (isLetter(s[n]) || isDigit(s[n])) {
		n++
	}
	if n+1 < len(s) && s[n] == '.' && isLetter(s[n+1]) {
		return n + 1 + nameLen(s[n+1:])
	}
	return n
}

func isLetter(c byte) bool { return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
func isDigit(c byte) bool  { return '0' <= c && c <= '9' }
