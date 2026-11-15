package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/dilaziz/blast/examples"
)

// blast runs the command in-process and returns what it printed and its exit code.
func blast(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func wantLines(t *testing.T, output string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(output, line) {
			t.Errorf("output is missing %q", line)
		}
	}
	if t.Failed() {
		t.Logf("full output:\n%s", output)
	}
}

// The plan's first milestone, end to end: init, a red check, the fix from
// section 5.7 applied by hand, a green check.
func TestInitThenCheckThenFix(t *testing.T) {
	t.Chdir(t.TempDir())

	if _, stderr, code := blast(t, "init"); code != exitOK {
		t.Fatalf("init: exit code %d, stderr %q", code, stderr)
	}

	stdout, stderr, code := blast(t, "check")
	if code != exitBroken || stderr != "" {
		t.Errorf("check on the proposed change: exit code %d, stderr %q; want %d and silence", code, stderr, exitBroken)
	}
	wantLines(t, stdout,
		"10 customers simulated",
		"annual + referred          5         -$644.40",
		"monthly                    0            $0.00",
		"legacy plans               1         -$158.00",
		"c4         pro annual          $891.00   $712.80   -$178.20   ✗ max-one-discount",
		"c6         legacy-pro annual   $790.00   $632.00   -$158.00   ✗ no-legacy-repricing",
		"5 of 10 customers (50%)",
		"$5,229.00 -> $4,584.60  (-$644.40, -12.3%)",
		"✗ max-one-discount      2 customers: c4, c7",
		"✗ no-legacy-repricing   1 customer: c6",
		"✓ finance-approval      not triggered",
		"2 of 3 rules broken · exit code 1",
	)
	if strings.Contains(stdout, "\x1b") {
		t.Error("output written to something other than a terminal contains colour codes")
	}

	// Section 5.7: narrow the new discount so it skips legacy plans and
	// customers who already hold a coupon.
	const before = `when: billing == "annual" and referred`
	const after = before + ` and not legacy and coupon_count == 0`
	pricing, err := os.ReadFile("pricing.new.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pricing, []byte(before+"\n")) {
		t.Fatalf("pricing.new.yml does not contain the condition the fix edits:\n%s", pricing)
	}
	fixed := bytes.Replace(pricing, []byte(before), []byte(after), 1)
	if err := os.WriteFile("pricing.new.yml", fixed, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code = blast(t, "check")
	if code != exitOK || stderr != "" {
		t.Errorf("check after the fix: exit code %d, stderr %q; want %d and silence", code, stderr, exitOK)
	}
	wantLines(t, stdout,
		"c3         pro annual       $990.00   $792.00   -$198.00",
		"c8         starter annual   $290.00   $232.00    -$58.00",
		"2 of 10 customers (20%)",
		"$5,229.00 -> $4,973.00  (-$256.00, -4.9%)",
		"✓ max-one-discount      ok",
		"✓ no-legacy-repricing   ok",
		"✓ finance-approval      not triggered",
		"all rules pass (3 checked) · exit code 0",
	)
	for _, id := range []string{"c4", "c6", "c7"} {
		if strings.Contains(stdout, id+" ") {
			t.Errorf("%s still appears among the changed customers after the fix", id)
		}
	}

	// The shipped pricing.fixed.yml must be that same fix, not a variant.
	shipped, err := os.ReadFile("pricing.fixed.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shipped, fixed) {
		t.Errorf("pricing.fixed.yml differs from pricing.new.yml with the section 5.7 fix applied")
	}
}

func TestApprovalFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	blast(t, "init")
	pricing, err := os.ReadFile("pricing.fixed.yml")
	if err != nil {
		t.Fatal(err)
	}
	// Touch how payments are captured, which the example rules gate on finance.
	if err := os.WriteFile("pricing.fixed.yml", append(pricing, "\npayment:\n  capture: manual\n"...), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := blast(t, "check", "--after", "pricing.fixed.yml")
	if code != exitBroken {
		t.Errorf("payment change without approval: exit code %d, want %d", code, exitBroken)
	}
	wantLines(t, stdout, `✗ finance-approval      needs approval("finance")`)

	stdout, _, code = blast(t, "check", "--after", "pricing.fixed.yml", "--approve", "legal,finance")
	if code != exitOK {
		t.Errorf("payment change with approval: exit code %d, want %d", code, exitOK)
	}
	wantLines(t, stdout, "✓ finance-approval      ok")
}

func TestBadInputExitsWithTwo(t *testing.T) {
	t.Chdir(t.TempDir())
	blast(t, "init")
	if err := os.WriteFile("broken.yml", []byte("plans:\n  pro: { monthly: 99 }\ndiscounts:\n  X:\n    percent: 20\n    when: refered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
		want string // expected on stderr
	}{
		{"unknown command", []string{"chekc"}, `blast: unknown command "chekc"`},
		{"unknown flag", []string{"check", "--befor", "x"}, "flag provided but not defined: -befor"},
		{"stray argument", []string{"check", "pricing.new.yml"}, `blast: check takes flags only, not "pricing.new.yml"`},
		{"unknown format", []string{"check", "--format", "xml"}, `blast: unknown format "xml" (available: `},
		{"missing file", []string{"check", "--after", "nope.yml"}, "blast: open nope.yml: no such file or directory"},
		{"error names file, line and column", []string{"check", "--after", "broken.yml"}, `blast: broken.yml:6:11 unknown name "refered"`},
		{"init will not overwrite", []string{"init"}, "blast: pricing.old.yml already exists (use --force to overwrite)"},
		{"init with two directories", []string{"init", "a", "b"}, "blast: init takes at most one directory"},
	}
	for _, tc := range cases {
		stdout, stderr, code := blast(t, tc.args...)
		if code != exitBadInput || !strings.Contains(stderr, tc.want) || stdout != "" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q\nwant exit code %d, nothing on stdout, stderr containing %q",
				tc.name, code, stdout, stderr, exitBadInput, tc.want)
		}
	}
}

func TestInitIntoDirectoryAndForce(t *testing.T) {
	t.Chdir(t.TempDir())
	stdout, _, code := blast(t, "init", "demo")
	if code != exitOK {
		t.Fatalf("init demo: exit code %d", code)
	}
	wantLines(t, stdout, "cd demo", "blast check")
	if err := os.WriteFile("demo/rules.yml", []byte("rules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := blast(t, "init", "--force", "demo"); code != exitOK {
		t.Fatalf("init --force demo: exit code %d", code)
	}
	restored, err := os.ReadFile("demo/rules.yml")
	if err != nil || !bytes.Contains(restored, []byte("max-one-discount")) {
		t.Errorf("init --force did not restore rules.yml: %v", err)
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"--help"}} {
		stdout, _, code := blast(t, args...)
		if code != exitOK {
			t.Errorf("blast %v: exit code %d", args, code)
		}
		wantLines(t, stdout, "See which customers a pricing change hits before you ship it.", "blast check [flags]", "Exit codes")
	}
	if stdout, _, code := blast(t, "version"); code != exitOK || stdout != "blast dev\n" {
		t.Errorf("version: %q, exit code %d", stdout, code)
	}
	if _, stderr, code := blast(t, "check", "--help"); code != exitOK || !strings.Contains(stderr, "-before") {
		t.Errorf("check --help: exit code %d, stderr %q", code, stderr)
	}
}

// init must write every file the example directory holds.
func TestInitCoversTheWholeExample(t *testing.T) {
	entries, err := examples.Acme.ReadDir("acme")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, f := range exampleFiles {
		listed[f.name] = true
	}
	for _, e := range entries {
		if !listed[e.Name()] {
			t.Errorf("examples/acme/%s is embedded but init does not write it", e.Name())
		}
	}
	if len(entries) != len(exampleFiles) {
		t.Errorf("init lists %d files, examples/acme holds %d", len(exampleFiles), len(entries))
	}
}

// A crafted input once drew a forged verdict over the real one. It must now
// be refused, and nothing it contains may reach the terminal on the way out.
func TestCraftedInputCannotReachTheTerminal(t *testing.T) {
	t.Chdir(t.TempDir())
	blast(t, "init")
	forged := "[\n  { \"id\": \"c4\\u001b[3A\\u001b[0J all rules pass\", \"plan\": \"pro\", \"billing\": \"annual\" }\n]\n"
	if err := os.WriteFile("customers.json", []byte(forged), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := blast(t, "check")
	if code != exitBadInput || stdout != "" {
		t.Errorf("exit code %d, stdout %q; want %d and nothing", code, stdout, exitBadInput)
	}
	if strings.ContainsRune(stderr, '\x1b') {
		t.Errorf("an escape character reached stderr: %q", stderr)
	}
	wantLines(t, stderr, "customers.json line 2: customer id")

	if got := visible("ok\x1b[2J‮\x07 fine\n\ttab"); got != "ok?[2J?? fine\n\ttab" {
		t.Errorf("visible = %q", got)
	}
}

func TestCSVCustomersEndToEnd(t *testing.T) {
	t.Chdir(t.TempDir())
	blast(t, "init")
	csv := "id,plan,billing,coupons,referred,quantity\n" +
		"acme,pro,annual,WELCOME10,yes,25\n" +
		"solo,starter,monthly,,no,\n"
	if err := os.WriteFile("customers.csv", []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := blast(t, "check", "--customers", "customers.csv")
	if code != exitBroken || stderr != "" {
		t.Errorf("exit code %d, stderr %q", code, stderr)
	}
	// 25 seats of pro annual: $24,750 list, 10% off, then the new 20%.
	wantLines(t, stdout,
		"2 customers simulated",
		"acme       pro annual ×25   $22,275.00   $17,820.00   -$4,455.00   ✗ max-one-discount",
		"1 of 2 customers (50%)",
	)
}

func TestDashRefusesToRunWithoutATerminal(t *testing.T) {
	t.Chdir(t.TempDir())
	blast(t, "init")
	stdout, stderr, code := blast(t, "dash")
	if code != exitBadInput || stdout != "" {
		t.Errorf("exit code %d, stdout %q; want %d and nothing", code, stdout, exitBadInput)
	}
	wantLines(t, stderr, `blast: dash needs a terminal; in scripts and CI use "blast check"`)
}
