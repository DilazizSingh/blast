package report

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/rules"
	"github.com/dilaziz/blast/internal/rules/expr"
	"github.com/dilaziz/blast/internal/sim"
)

// The Markdown report is posted to pull requests by a bot, and the names in
// it can come from the pull request's author. A name must not be able to
// break out of its table cell, render HTML, ping a user or plant a link.
func TestMarkdownNeutralisesNamesFromInputFiles(t *testing.T) {
	const hostile = "x`|<img src=x>@octocat[a](http://evil.example)"
	cond, err := expr.Compile(`approval("x")`, rules.Schema())
	if err != nil {
		t.Fatal(err)
	}
	row := diff.Row{
		Customer: model.Customer{ID: hostile, Plan: hostile, Billing: model.Annual},
		Old:      sim.Invoice{TotalCents: 99000},
		New:      sim.Invoice{TotalCents: 79200},
	}
	r := Report{
		Before: hostile, After: hostile,
		Result: diff.Result{
			Rows:     []diff.Row{row},
			Totals:   diff.Totals{Customers: 1, Affected: 1, OldCents: 99000, NewCents: 79200},
			Segments: []diff.Segment{{Name: hostile}},
		},
		Changes: []diff.Change{{Path: hostile, Old: hostile, New: hostile}},
		Verdicts: []rules.Verdict{
			{Rule: rules.Rule{Name: hostile, Kind: rules.Never, Cond: cond}, Status: rules.Broken, Customers: []string{hostile}},
		},
	}
	var out bytes.Buffer
	if err := Markdown(&out, r); err != nil {
		t.Fatal(err)
	}
	md := out.String()

	if strings.Contains(md, hostile) {
		t.Errorf("the name appears exactly as written, backtick and pipe included:\n%s", md)
	}
	const safe = "`x'\\|<img src=x>@octocat[a](http://evil.example)`"
	if got := strings.Count(md, safe); got < 8 {
		t.Errorf("found the neutralised form %d times, want every one of the 8 or more uses:\n%s", got, md)
	}

	// With the code spans removed, nothing of the name may be left, and every
	// table row must still have the number of cells its table declares.
	prose := regexp.MustCompile("`[^`]*`").ReplaceAllString(md, "")
	for _, leak := range []string{"<img", "@octocat", "](http", "evil.example"} {
		if strings.Contains(prose, leak) {
			t.Errorf("%q escaped its code span:\n%s", leak, md)
		}
	}
	columns := 0
	for _, line := range strings.Split(prose, "\n") {
		switch {
		case !strings.HasPrefix(line, "|"):
			columns = 0
		case columns == 0:
			columns = strings.Count(line, "|") // a table's header row sets the width
		case strings.Count(line, "|") != columns:
			t.Errorf("row has %d cell borders, its table has %d: %s", strings.Count(line, "|"), columns, line)
		}
	}
}
