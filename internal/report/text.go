package report

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/rules"
)

const (
	maxListed  = 10 // changed customers listed before the rest are summarised
	barCells   = 20 // width of the blast-radius bar
	minRuleLen = 56 // the horizontal rules are at least this wide
)

// Text writes the report for a person at a terminal. The output carries
// colour codes; a writer that is not a terminal is expected to strip them,
// which cmd/blast arranges.
func Text(w io.Writer, r Report) error {
	var (
		t         = r.Result.Totals
		segments  = segmentLines(r.Result.Segments)
		customers = customerLines(r)
		rule      = hairline(segments, customers)
		b         strings.Builder
	)
	section := func(lines []string) {
		if len(lines) == 0 {
			return
		}
		b.WriteString(rule + "\n" + strings.Join(lines, "\n") + "\n")
	}

	fmt.Fprintf(&b, " %s\n", accent.Render(r.Before+" → "+r.After))
	fmt.Fprintf(&b, " %s simulated\n", plural(t.Customers, "customer"))
	section(segments)
	section(customers)
	section([]string{
		fmt.Sprintf("  Blast radius  %s  %s of %s (%s)",
			radiusBar(t), model.Thousands(int64(t.Affected)), plural(t.Customers, "customer"), Radius(t)),
		fmt.Sprintf("  Revenue       %s -> %s  (%s, %s)",
			model.Dollars(t.OldCents), model.Dollars(t.NewCents), model.Dollars(t.ChangeCents()), ChangePercent(t)),
	})
	if len(r.Verdicts) > 0 {
		b.WriteString("\n" + strings.Join(ruleLines(r.Verdicts), "\n") + "\n")
	}
	fmt.Fprintf(&b, "\n %s\n", footer(r))

	_, err := io.WriteString(w, b.String())
	return err
}

func segmentLines(segments []diff.Segment) []string {
	if len(segments) == 0 {
		return nil
	}
	t := table{right: []bool{false, true, true}}
	t.add("Segment", "Affected", "Revenue change")
	for _, s := range segments {
		t.add(s.Name, model.Thousands(int64(s.Affected)), model.Dollars(s.ChangeCents()))
	}
	return t.lines(func(row int, cells []string) {
		if segments[row].Affected > 0 {
			cells[2] = warn.Render(cells[2])
		}
	})
}

// customerLines lists who pays a different amount, largest change first.
func customerLines(r Report) []string {
	changed := r.Result.Changed()
	if len(changed) == 0 {
		return nil
	}
	broke := r.brokenBy()
	t := table{right: []bool{false, false, true, true, true, false}}
	t.add("Customer", "Plan", "Old", "New", "Change", "")
	for _, row := range changed[:min(len(changed), maxListed)] {
		c := row.Customer
		broken := ""
		if names := broke[c.ID]; len(names) > 0 {
			broken = "✗ " + strings.Join(names, ", ")
		}
		t.add(c.ID, c.Plan+" "+string(c.Billing),
			model.Dollars(row.Old.TotalCents), model.Dollars(row.New.TotalCents), model.Dollars(row.ChangeCents()), broken)
	}
	lines := t.lines(func(_ int, cells []string) {
		cells[4] = warn.Render(cells[4])
		if cells[5] != "" {
			cells[5] = bad.Render(cells[5])
		}
	})
	if rest := len(changed) - maxListed; rest > 0 {
		lines = append(lines, muted.Render(fmt.Sprintf("  … and %s more, with smaller changes", model.Thousands(int64(rest)))))
	}
	return lines
}

func ruleLines(verdicts []rules.Verdict) []string {
	width := 0
	for _, v := range verdicts {
		width = max(width, lipgloss.Width(v.Rule.Name))
	}
	lines := make([]string, len(verdicts))
	for i, v := range verdicts {
		mark, detail := good.Render("✓"), muted.Render(Detail(v))
		if v.Status == rules.Broken {
			mark, detail = bad.Render("✗"), Detail(v)
		}
		lines[i] = fmt.Sprintf("  %s %s   %s", mark, pad(v.Rule.Name, width, false), detail)
	}
	return lines
}

// radiusBar draws the share of customers affected. Any affected customer
// lights at least one cell, so a small radius never looks like none.
func radiusBar(t diff.Totals) string {
	lit := 0
	if t.Customers > 0 {
		lit = (t.Affected*barCells*2 + t.Customers) / (2 * t.Customers)
	}
	if lit == 0 && t.Affected > 0 {
		lit = 1
	}
	return warn.Render(strings.Repeat("█", lit)) + muted.Render(strings.Repeat("░", barCells-lit))
}

func footer(r Report) string {
	total := len(r.Verdicts)
	switch broken := r.Broken(); {
	case total == 0:
		return muted.Render("no rules defined · exit code 0")
	case broken == 0:
		return good.Render("all rules pass") + muted.Render(fmt.Sprintf(" (%d checked) · exit code 0", total))
	default:
		return bad.Render(fmt.Sprintf("%d of %s broken", broken, plural(total, "rule"))) + muted.Render(" · exit code 1")
	}
}

// hairline is a horizontal rule as wide as the widest table header.
func hairline(sections ...[]string) string {
	width := minRuleLen
	for _, lines := range sections {
		if len(lines) > 0 {
			width = max(width, lipgloss.Width(lines[0])-1)
		}
	}
	return " " + muted.Render(strings.Repeat("─", width))
}

// table lays cells out in aligned columns. The first row is the header.
type table struct {
	right []bool // per column: right-align, as numbers should be
	rows  [][]string
}

func (t *table) add(cells ...string) { t.rows = append(t.rows, cells) }

// lines pads every cell while it is still plain text, then lets style colour
// the cells of each body row in place, so colour codes never disturb the
// alignment. style's row index counts body rows from zero.
func (t *table) lines(style func(row int, cells []string)) []string {
	widths := make([]int, len(t.right))
	for _, row := range t.rows {
		for i, cell := range row {
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	last := len(t.right) - 1
	out := make([]string, len(t.rows))
	for r, row := range t.rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = cell
			// A left-aligned final column needs no padding, and padding it
			// would leave trailing spaces on every line.
			if i < last || t.right[i] {
				cells[i] = pad(cell, widths[i], t.right[i])
			}
		}
		if r == 0 {
			out[r] = muted.Render(strings.TrimRight("  "+strings.Join(cells, "   "), " "))
			continue
		}
		style(r-1, cells)
		out[r] = strings.TrimRight("  "+strings.Join(cells, "   "), " ")
	}
	return out
}

// pad widens s to width cells, measuring what a terminal would display.
func pad(s string, width int, right bool) string {
	fill := strings.Repeat(" ", max(width-lipgloss.Width(s), 0))
	if right {
		return fill + s
	}
	return s + fill
}
