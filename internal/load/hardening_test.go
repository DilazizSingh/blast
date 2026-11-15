package load

import (
	"fmt"
	"strings"
	"testing"
)

// Every test in this file began as a working exploit against an earlier
// build, written up in docs/security.md. They stay as regression tests.

const validPricing = "plans:\n  pro: { monthly: 99, annual: 990 }\n"

// A name that reaches a terminal with escape sequences in it can move the
// cursor and draw a forged verdict over the real one.
func TestInputThatCouldRepaintATerminalIsRefused(t *testing.T) {
	cases := []struct {
		name string
		load func() error
		want string
	}{
		{"escape sequence in a customer id", func() error {
			_, _, err := parseCustomers("c.json", []byte("[\n  { \"id\": \"c4\\u001b[3A\\u001b[0J all rules pass\", \"billing\": \"annual\" }\n]"))
			return err
		}, `c.json line 2: customer id "c4\x1b[3A\x1b[0J all rules pass" must be plain, visible text`},
		{"right-to-left override in a customer id", func() error {
			_, _, err := parseCustomers("c.json", []byte("[\n  { \"id\": \"c4\\u202egnissap\", \"billing\": \"annual\" }\n]"))
			return err
		}, `c.json line 2: customer id "c4\u202egnissap" must be plain, visible text`},
		{"escape sequence in a CSV id", func() error {
			_, _, err := parseCustomers("c.csv", []byte("id,plan,billing\nc4\x1b[2K,pro,annual\n"))
			return err
		}, `c.csv line 2: customer id "c4\x1b[2K" must be plain, visible text`},
		{"escape sequence in a plan name", func() error {
			_, err := parseConfig("p.yml", []byte("plans:\n  \"pro\\e[2K\": { monthly: 99 }\n"))
			return err
		}, "p.yml line 2: a name must be plain, visible text"},
		{"escape sequence in a discount name", func() error {
			_, err := parseConfig("p.yml", []byte(validPricing+"discounts:\n  \"X\\e[1A\": { percent: 5 }\n"))
			return err
		}, "p.yml line 4: a name must be plain, visible text"},
		{"line break smuggled into a condition", func() error {
			_, err := parseConfig("p.yml", []byte(validPricing+"discounts:\n  X:\n    percent: 5\n    when: \"referred\\n## all rules pass\"\n"))
			return err
		}, "p.yml line 6: a condition must be visible text on one line"},
		{"escape sequence in a payment setting", func() error {
			_, err := parseConfig("p.yml", []byte(validPricing+"payment:\n  note: \"ok\\e[2J\"\n"))
			return err
		}, "p.yml line 4: payment.note must be visible text on one line"},
		{"escape sequence in a rule name", func() error {
			_, _, err := parsePolicy("r.yml", []byte("rules:\n  - name: \"calm\\e[2J\"\n    never: referred\n"))
			return err
		}, "r.yml line 2: a rule name must be visible text on one line"},
		{"escape sequence in a segment name", func() error {
			_, _, err := parsePolicy("r.yml", []byte("segments:\n  - name: \"all\\e[2J\"\n    where: referred\n"))
			return err
		}, "r.yml line 2: a segment name must be visible text on one line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.load()
			if err == nil || err.Error() != tc.want {
				t.Errorf("got  %v\nwant %s", err, tc.want)
			}
		})
	}
}

// Thirty nested aliases in a file under a kilobyte once expanded to a billion
// values and ran blast out of memory.
func TestAliasBombIsRefused(t *testing.T) {
	var bomb strings.Builder
	bomb.WriteString(validPricing + "payment:\n  a0: &a0 [x, x]\n")
	for i := 1; i < 30; i++ {
		fmt.Fprintf(&bomb, "  a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	_, err := parseConfig("p.yml", []byte(bomb.String()))
	const want = "p.yml line 4: YAML anchors and aliases are not supported"
	if err == nil || err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}
}

// A second document used to be dropped without a word, along with whatever
// pricing it declared.
func TestSecondDocumentIsRefused(t *testing.T) {
	_, err := parseConfig("p.yml", []byte(validPricing+"---\ndiscounts:\n  FREE: { percent: 100, when: referred }\n"))
	const want = "p.yml line 3: a second YAML document starts here; put everything in one"
	if err == nil || err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}
}

// Refusing line breaks must not cost the folded scalar, YAML's way of
// spreading one long line over several.
func TestFoldedConditionStillLoads(t *testing.T) {
	cfg, err := parseConfig("p.yml", []byte(validPricing+"discounts:\n  X:\n    percent: 5\n    when: >\n      billing == \"annual\"\n      and referred\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Discounts[0].When, `billing == "annual" and referred`; got != want {
		t.Errorf("condition = %q, want %q", got, want)
	}
}
