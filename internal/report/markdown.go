package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/rules"
)

// maxChanges is how many changed pricing settings the Markdown report lists.
// With the cap on customers it keeps a comment well inside GitHub's limit.
const maxChanges = 40

// Markdown writes the report as a pull-request comment.
//
// The comment is posted with a bot's authority, while names in the pricing,
// rules and customer files may come from the pull request's author. Every
// such value therefore goes through mdCode, where Markdown, HTML and
// @mentions are inert.
func Markdown(w io.Writer, r Report) error {
	t := r.Result.Totals
	var b strings.Builder

	fmt.Fprintf(&b, "### %s\n\n", mdTitle(r))
	fmt.Fprintf(&b, "%s → %s · %s simulated\n\n", mdCode(r.Before), mdCode(r.After), plural(t.Customers, "customer"))
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Blast radius** | %s of %s (%s) |\n",
		model.Thousands(int64(t.Affected)), plural(t.Customers, "customer"), strings.ReplaceAll(Radius(t), "<", "&lt;"))
	fmt.Fprintf(&b, "| **Revenue per cycle** | %s → %s (%s, %s) |\n",
		mdMoney(t.OldCents), mdMoney(t.NewCents), mdMoney(t.ChangeCents()), ChangePercent(t))

	if len(r.Verdicts) > 0 {
		b.WriteString("\n#### Rules\n\n| | Rule | Result |\n|:-:|---|---|\n")
		for _, v := range r.Verdicts {
			mark := "✅"
			if v.Status == rules.Broken {
				mark = "❌"
			}
			phrase, quoted, more := outcome(v)
			fmt.Fprintf(&b, "| %s | %s | %s%s%s |\n", mark, mdCode(v.Rule.Name), phrase, mdCodes(quoted), andMore(more))
		}
	}
	if len(r.Result.Segments) > 0 {
		b.WriteString("\n#### Segments\n\n| Segment | Affected | Revenue change |\n|---|--:|--:|\n")
		for _, s := range r.Result.Segments {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", mdCode(s.Name), model.Thousands(int64(s.Affected)), mdMoney(s.ChangeCents()))
		}
	}
	mdCustomers(&b, r)
	mdChanges(&b, r)

	b.WriteString("\n<sub>Every number here was computed from the pricing files and the customer list. None is estimated.</sub>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func mdTitle(r Report) string {
	total := len(r.Verdicts)
	switch broken := r.Broken(); {
	case total == 0:
		return "✅ Pricing change simulated (no rules defined)"
	case broken == 0:
		return fmt.Sprintf("✅ Pricing change passes all %s", plural(total, "rule"))
	default:
		return fmt.Sprintf("❌ Pricing change breaks %d of %s", broken, plural(total, "rule"))
	}
}

func mdCustomers(b *strings.Builder, r Report) {
	rows := r.notable()
	if len(rows) == 0 {
		return
	}
	broke := r.BrokenBy()
	b.WriteString("\n#### Customers, largest change first\n\n| Customer | Plan | Old | New | Change | Rules broken |\n|---|---|--:|--:|--:|---|\n")
	for _, row := range rows[:min(len(rows), maxListed)] {
		c := row.Customer
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n", mdCode(c.ID), mdCode(PlanLabel(c)),
			mdMoney(row.Old.TotalCents), mdMoney(row.New.TotalCents), mdMoney(row.ChangeCents()), mdCodes(broke[c.ID]))
	}
	if rest := len(rows) - maxListed; rest > 0 {
		fmt.Fprintf(b, "\n…and %s more, with smaller changes.\n", model.Thousands(int64(rest)))
	}
}

func mdChanges(b *strings.Builder, r Report) {
	if len(r.Changes) == 0 {
		return
	}
	fmt.Fprintf(b, "\n<details><summary>Pricing settings that changed (%d)</summary>\n\n| Setting | Before | After |\n|---|---|---|\n", len(r.Changes))
	for _, c := range r.Changes[:min(len(r.Changes), maxChanges)] {
		fmt.Fprintf(b, "| %s | %s | %s |\n", mdCode(c.Path), mdCode(c.Old), mdCode(c.New))
	}
	if rest := len(r.Changes) - maxChanges; rest > 0 {
		fmt.Fprintf(b, "\n…and %d more.\n", rest)
	}
	b.WriteString("\n</details>\n")
}

// mdCode shows a value from an input file as inline code. A backtick would
// end the code span and a bare pipe would end the table cell, so neither
// survives as written.
func mdCode(s string) string {
	if s == "" {
		return ""
	}
	return "`" + strings.NewReplacer("`", "'", "|", `\|`).Replace(s) + "`"
}

func mdCodes(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = mdCode(v)
	}
	return strings.Join(quoted, ", ")
}

// mdMoney escapes the dollar sign, which some Markdown renderers read as the
// start of a formula.
func mdMoney(cents int64) string {
	return strings.ReplaceAll(model.Dollars(cents), "$", `\$`)
}
