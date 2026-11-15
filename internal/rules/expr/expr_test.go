package expr

import (
	"strings"
	"testing"
)

var testSchema = Schema{
	Vars: map[string]Type{
		"referred": Bool, "legacy": Bool, "billing": Str, "coupon_count": Num,
		"customer.discount_count": Num, "invoice.old": Num, "invoice.new": Num, "invoice.change": Num,
	},
	Funcs: map[string]Func{
		"approval":     {Params: []Type{Str}, Result: Bool},
		"diff.touches": {Params: []Type{Str}, Result: Bool},
		"between":      {Params: []Type{Num, Num, Num}, Result: Bool},
	},
}

// testEnv is c4 from the plan's example: pro annual, WELCOME10, referred.
type testEnv map[string]Value

var c4 = testEnv{
	"referred": BoolOf(true), "legacy": BoolOf(false), "billing": StrOf("annual"), "coupon_count": CountOf(1),
	"customer.discount_count": CountOf(2), "invoice.old": NumOf(89100), "invoice.new": NumOf(71280), "invoice.change": NumOf(-17820),
}

func (e testEnv) Var(name string) Value { return e[name] }

func (e testEnv) Call(name string, args []Value) Value {
	switch name {
	case "approval":
		return BoolOf(args[0].Str == "finance")
	case "between":
		return BoolOf(args[0].Num >= args[1].Num && args[0].Num <= args[2].Num)
	}
	return BoolOf(false)
}

func TestEval(t *testing.T) {
	cases := map[string]bool{
		// The conditions the plan itself uses.
		`billing == "annual" and referred`:                                      true,
		`billing == "annual" and referred and not legacy and coupon_count == 0`: false,
		`customer.discount_count > 1`:                                           true,
		`legacy and invoice.new != invoice.old`:                                 false,
		`approval("finance")`:                                                   true,
		`diff.touches("payment")`:                                               false,

		// Precedence: not, then and, then or.
		`true or false and false`:   true,
		`(true or false) and false`: false,
		`not true or true`:          true,
		`not (true or true)`:        false,
		`not not referred`:          true,
		`not legacy and referred`:   true,

		// Comparisons, including dollars against cents.
		`invoice.new == 712.80`:        true,
		`invoice.new == 712.8`:         true,
		`invoice.new != 712.80`:        false,
		`invoice.new < invoice.old`:    true,
		`invoice.new <= 712.80`:        true,
		`invoice.new > 712.80`:         false,
		`invoice.new >= 712.81`:        false,
		`invoice.change <= -178.2`:     true,
		`invoice.change < -178.2`:      false,
		`coupon_count == 1`:            true,
		`billing != "monthly"`:         true,
		`referred == true`:             true,
		`legacy == referred`:           false,
		`"a b" == "a b"`:               true,
		"  referred\tand\nnot legacy ": true,

		// Calls.
		`approval("legal")`:                        false,
		`not approval("legal") and referred`:       true,
		`between(coupon_count, 0, 1)`:              true,
		`between(invoice.new, 800, 900) or legacy`: false,
		`approval("finance") == referred`:          true,
		`(approval("finance")) and (referred)`:     true,
		`between((coupon_count), -1, (((2))))`:     true,
	}
	for src, want := range cases {
		e, err := Compile(src, testSchema)
		if err != nil {
			t.Errorf("Compile(%q): %v", src, err)
			continue
		}
		if got := e.Eval(c4); got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
		if e.String() != src {
			t.Errorf("String() = %q, want the source %q", e.String(), src)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []struct {
		src    string
		offset int
		msg    string
	}{
		// Syntax.
		{``, 0, `expected an expression`},
		{`   `, 3, `expected an expression`},
		{`customer.discount_count >`, 25, `expected value after ">"`},
		{`referred and`, 12, `expected value after "and"`},
		{`referred and or legacy`, 13, `expected value after "and"`},
		{`not`, 3, `expected value after "not"`},
		{`(referred`, 9, `expected ")"`},
		{`referred)`, 8, `unexpected ")"`},
		{`referred legacy`, 9, `unexpected "legacy"`},
		{`coupon_count == 1 == true`, 18, `unexpected "=="`},
		{`coupon_count = 1`, 13, `use "==" to compare`},
		{`billing == "annual`, 11, `text is missing its closing "`},
		{`coupon_count > 1.234`, 15, `"1.234" has more than two decimal places`},
		{`coupon_count > 1.2.3`, 15, `"1.2.3" is not a number`},
		{`coupon_count > -`, 15, `"-" is not a number`},
		{`referred # legacy`, 9, `unexpected character "#"`},
		{`customer. > 1`, 8, `unexpected character "."`},
		{`approval("finance"`, 18, `expected "," or ")" in the call to approval`},
		{`approval("finance",)`, 19, `expected value after ","`},
		{`"é" == billing and $`, 19, `unexpected character "$"`},
		{strings.Repeat("(", maxLen+1), 0, `expression is longer than 4096 characters`},

		// Types.
		{`refered`, 0, `unknown name "refered"`},
		{`referred and customer.discount > 1`, 13, `unknown name "customer.discount"`},
		{`approve("finance")`, 0, `unknown function "approve"`},
		{`approval()`, 0, `approval takes 1 argument, got 0`},
		{`between(1, 2)`, 0, `between takes 3 arguments, got 2`},
		{`approval(1)`, 9, `approval expects text here, got a number`},
		{`billing > 1`, 8, `">" compares numbers, got text and a number`},
		{`billing == 1`, 8, `cannot compare text with a number`},
		{`referred != "yes"`, 9, `cannot compare true/false with text`},
		{`coupon_count and referred`, 0, `"and" needs true/false on both sides, got a number`},
		{`referred or billing`, 12, `"or" needs true/false on both sides, got text`},
		{`not coupon_count`, 4, `"not" needs true/false, got a number`},
		{`coupon_count`, 0, `a condition must be true/false, but this is a number`},
		{`"annual"`, 0, `a condition must be true/false, but this is text`},
	}
	for _, tc := range cases {
		_, err := Compile(tc.src, testSchema)
		got, ok := err.(*Error)
		if !ok {
			t.Errorf("Compile(%q) = %v, want an *Error", tc.src, err)
			continue
		}
		if got.Offset != tc.offset || got.Msg != tc.msg {
			t.Errorf("Compile(%q)\n got  offset %d: %s\n want offset %d: %s", tc.src, got.Offset, got.Msg, tc.offset, tc.msg)
		}
	}
}

func TestVars(t *testing.T) {
	cases := map[string]string{
		`approval("finance")`:                          "",
		`diff.touches("payment") or approval("legal")`: "",
		`legacy and invoice.new != invoice.old`:        "legacy invoice.new invoice.old",
		`between(coupon_count, 0, 1) and not referred`: "coupon_count referred",
	}
	for src, want := range cases {
		e, err := Compile(src, testSchema)
		if err != nil {
			t.Fatalf("Compile(%q): %v", src, err)
		}
		if got := strings.Join(e.Vars(), " "); got != want {
			t.Errorf("Vars(%q) = %q, want %q", src, got, want)
		}
	}
}

// Deep nesting within the length limit must parse, not overflow the stack.
func TestDeepNesting(t *testing.T) {
	depth := maxLen/2 - 4
	src := strings.Repeat("(", depth) + "referred" + strings.Repeat(")", depth)
	e, err := Compile(src, testSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Eval(c4) {
		t.Error("deeply parenthesised true evaluated to false")
	}
}
