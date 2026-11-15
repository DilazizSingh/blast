package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// repo turns the current directory into a git repository with the example's
// current pricing committed as pricing.yml, and the proposed change sitting
// uncommitted in the working copy: what a developer has mid-change.
func repo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	blast(t, "init")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Rename("pricing.old.yml", "pricing.yml"))
	git("init", "-q", "-b", "main")
	git("add", "pricing.yml")
	git("commit", "-q", "-m", "pricing as it is today")
	proposed, err := os.ReadFile("pricing.new.yml")
	must(err)
	must(os.WriteFile("pricing.yml", proposed, 0o644))
}

func TestBaseComparesAgainstAGitRevision(t *testing.T) {
	t.Chdir(t.TempDir())
	repo(t)

	stdout, stderr, code := blast(t, "check", "--base", "main", "--after", "pricing.yml")
	if code != exitBroken || stderr != "" {
		t.Fatalf("exit code %d, stderr %q; want %d and silence", code, stderr, exitBroken)
	}
	wantLines(t, stdout, "main:pricing.yml → pricing.yml", "5 of 10 customers (50%)", "2 of 3 rules broken")

	// With the change committed there is nothing left to compare.
	stdout, _, code = blast(t, "check", "--base", "HEAD", "--after", "pricing.new.yml")
	if code != exitBadInput {
		t.Errorf("a file the revision does not have: exit code %d, want %d", code, exitBadInput)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

func TestProjectFileMakesFlagsUnnecessary(t *testing.T) {
	t.Chdir(t.TempDir())
	repo(t)
	if err := os.WriteFile("blast.yml", []byte("after: pricing.yml\nbase: main\ncustomers: customers.json\nrules: rules.yml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := blast(t, "check")
	if code != exitBroken || stderr != "" {
		t.Fatalf("exit code %d, stderr %q; want %d and silence", code, stderr, exitBroken)
	}
	wantLines(t, stdout, "main:pricing.yml → pricing.yml", "2 of 3 rules broken")

	// A file named on the command line beats the project's base revision.
	// No invoice moves when a file is compared with itself, but c4 and c7
	// still hold two discounts under it, which the rule forbids.
	stdout, _, code = blast(t, "check", "--before", "pricing.yml")
	if code != exitBroken {
		t.Errorf("comparing the file with itself: exit code %d, want %d", code, exitBroken)
	}
	wantLines(t, stdout, "pricing.yml → pricing.yml", "0 of 10 customers (0%)", "✗ max-one-discount      2 customers: c4, c7")
}

func TestBaseAndProjectFileErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	repo(t)
	cases := []struct {
		name    string
		project string
		args    []string
		want    string
	}{
		{"both flags", "", []string{"check", "--before", "pricing.yml", "--base", "main"}, "blast: use --before or --base, not both"},
		{"revision that is an option", "", []string{"check", "--base=--output=/tmp/x", "--after", "pricing.yml"},
			`blast: "--output=/tmp/x" is not a git revision`},
		{"unknown revision", "", []string{"check", "--base", "nope", "--after", "pricing.yml"},
			"blast: cannot read pricing.yml at revision nope: "},
		{"file not in the revision", "", []string{"check", "--base", "main", "--after", "pricing.new.yml"},
			"blast: cannot read pricing.new.yml at revision main: "},
		{"unknown project setting", "pricing: pricing.yml\n", []string{"check"},
			`blast: blast.yml line 1: unknown setting "pricing" (expected before, base, after, customers or rules)`},
		{"project with before and base", "before: a.yml\nbase: main\n", []string{"check"}, `blast: blast.yml: set "before" or "base", not both`},
		{"project value that is not text", "after: [a, b]\n", []string{"check"}, "blast: blast.yml line 1: expected a value"},
	}
	for _, tc := range cases {
		os.Remove("blast.yml")
		if tc.project != "" {
			if err := os.WriteFile("blast.yml", []byte(tc.project), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		stdout, stderr, code := blast(t, tc.args...)
		if code != exitBadInput || stdout != "" || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q\nwant exit code %d and stderr containing %q",
				tc.name, code, stdout, stderr, exitBadInput, tc.want)
		}
	}
}
