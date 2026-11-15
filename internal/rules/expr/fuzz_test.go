package expr

import (
	"testing"
	"unicode/utf8"
)

// FuzzCompile feeds the compiler arbitrary input. Whatever it is given, it
// must either return a usable expression or an *Error pointing inside the
// source; it must never panic. Run it with `make fuzz`.
func FuzzCompile(f *testing.F) {
	seeds := []string{
		`billing == "annual" and referred`,
		`billing == "annual" and referred and not legacy and coupon_count == 0`,
		`customer.discount_count > 1`,
		`legacy and invoice.new != invoice.old`,
		`approval("finance")`,
		`diff.touches("payment") or between(coupon_count, -1, 2.50)`,
		`not (referred or legacy) == true`,
		`customer.discount_count >`,
		`((((`, `))))`, `"unterminated`, `a..b`, `1.2.3`, `-`, `=`, `not not not`, `f(,)`, `f(a,`, "\x00\xff", `é == "é"`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		e, err := Compile(src, testSchema)
		if err == nil {
			e.Eval(c4)
			return
		}
		bad, ok := err.(*Error)
		if !ok {
			t.Fatalf("Compile(%q) returned a %T, want *Error", src, err)
		}
		if bad.Msg == "" || bad.Offset < 0 || bad.Offset > utf8.RuneCountInString(src) {
			t.Fatalf("Compile(%q) = offset %d, %q: not a position in the source", src, bad.Offset, bad.Msg)
		}
	})
}
