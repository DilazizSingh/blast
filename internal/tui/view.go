package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/report"
	"github.com/dilaziz/blast/internal/rules"
	"github.com/dilaziz/blast/internal/sim"
)

const (
	minWidth, minHeight = 64, 14
	// divider is a left-hand line the frame draws as a rule joined to its borders.
	divider = "\x00"
)

var (
	accent = lipgloss.NewStyle().Foreground(report.Accent)
	focus  = accent.Bold(true)
	good   = lipgloss.NewStyle().Foreground(report.Green)
	bad    = lipgloss.NewStyle().Foreground(report.Red)
	warn   = lipgloss.NewStyle().Foreground(report.Amber)
	muted  = lipgloss.NewStyle().Foreground(report.Muted)
	bold   = lipgloss.NewStyle().Bold(true)
)

var hints = map[screen]string{
	overview:  "↑↓ move  enter drill in  d diff config  r rerun  q quit",
	customers: "↑↓ move  enter why this price  esc back  d diff config  r rerun  q quit",
	invoice:   "↑↓ next customer  esc back  d diff config  r rerun  q quit",
	changes:   "↑↓ move  esc back  r rerun  q quit",
}

func (b board) View() tea.View {
	v := tea.NewView(b.render())
	v.AltScreen = true
	v.WindowTitle = b.name
	return v
}

// render draws the whole screen: exactly b.height lines of b.width cells.
func (b board) render() string {
	if b.width < minWidth || b.height < minHeight {
		return fmt.Sprintf("%s needs a window of at least %d×%d cells; this one is %d×%d.",
			b.name, minWidth, minHeight, b.width, b.height)
	}
	leftW := b.leftWidth()
	rightW := b.width - leftW - 7 // three borders and four spaces of padding
	bodyH := b.height - 4         // top border, footer rule, footer, bottom border
	left, right := b.leftLines(leftW, bodyH), b.rightLines(rightW, bodyH)

	edge := b.edge()
	bar := edge.Render("│")
	files := ansi.Truncate(b.rep.Before+" → "+b.rep.After, b.width-8-lipgloss.Width(b.name), "…")
	fill := b.width - 8 - lipgloss.Width(b.name) - lipgloss.Width(files)

	lines := make([]string, 0, b.height)
	lines = append(lines, edge.Render("╭─ ")+focus.Render(b.name)+edge.Render(" ─ ")+files+" "+edge.Render(strings.Repeat("─", fill)+"╮"))
	for i := range bodyH {
		l, r := at(left, i), at(right, i)
		if l == divider {
			lines = append(lines, edge.Render("├"+strings.Repeat("─", leftW+2)+"┤")+" "+fit(r, rightW)+" "+bar)
			continue
		}
		lines = append(lines, bar+" "+fit(l, leftW)+" "+bar+" "+fit(r, rightW)+" "+bar)
	}
	lines = append(lines,
		edge.Render("├"+strings.Repeat("─", leftW+2)+"┴"+strings.Repeat("─", rightW+2)+"┤"),
		bar+" "+spread(b.hint(), b.status(), b.width-4)+" "+bar,
		edge.Render("╰"+strings.Repeat("─", b.width-2)+"╯"))
	return strings.Join(lines, "\n")
}

// edge colours the frame by the verdict, so the state of the change is
// visible from across the room.
func (b board) edge() lipgloss.Style {
	switch {
	case len(b.rep.Verdicts) == 0:
		return muted
	case b.rep.Broken() > 0:
		return bad
	}
	return good
}

func (b board) hint() string {
	if b.err != nil {
		return bad.Render("✗ " + oneLine(b.err.Error()))
	}
	return muted.Render(hints[b.screen])
}

// status is the verdict, highlighted for a moment after it flips.
func (b board) status() string {
	var verdict string
	style := b.edge()
	switch total, broken := len(b.rep.Verdicts), b.rep.Broken(); {
	case b.err != nil:
		return muted.Render("showing the last good run")
	case total == 0:
		verdict = "no rules defined"
	case broken == 0:
		verdict = "✓ all rules pass"
	default:
		verdict = fmt.Sprintf("✗ %d of %d rules broken", broken, total)
	}
	if b.flash%2 == 1 {
		style = style.Reverse(true).Bold(true)
	}
	switch {
	case b.busy:
		return muted.Render("simulating… ") + style.Render(verdict)
	case b.changed != nil:
		return muted.Render("live ") + style.Render(verdict)
	}
	return style.Render(verdict)
}

func (b board) leftWidth() int {
	longest := len("all customers")
	for _, s := range b.rep.Result.Segments {
		longest = max(longest, lipgloss.Width(s.Name))
	}
	for _, v := range b.rep.Verdicts {
		longest = max(longest, lipgloss.Width(v.Rule.Name)+2)
	}
	// Room for the cursor and a count, but never more than a third of the window.
	return min(max(longest+8, 22), b.width/3)
}

// leftLines lists the segments and then the rules, with one cursor running
// through both.
func (b board) leftLines(w, h int) []string {
	res := b.rep.Result
	lines := []string{muted.Render("SEGMENTS"), b.entry(0, "all customers", res.Totals.Affected, w)}
	for i, s := range res.Segments {
		lines = append(lines, b.entry(1+i, s.Name, s.Affected, w))
	}
	lines = append(lines, divider, muted.Render("RULES"))
	for i, v := range b.rep.Verdicts {
		mark := good.Render("✓")
		if v.Status == rules.Broken {
			mark = bad.Render("✗")
		}
		lines = append(lines, b.entry(1+len(res.Segments)+i, mark+" "+v.Rule.Name, len(v.Customers), w))
	}
	cursor := 1 + b.left
	if b.left > len(res.Segments) {
		cursor += 2 // past the divider and the RULES heading
	}
	return window(lines, cursor, h)
}

// entry is one selectable line on the left: a label, and how many customers
// are affected or break the rule.
func (b board) entry(index int, label string, count, w int) string {
	number := ""
	if count > 0 {
		number = model.Thousands(int64(count))
	}
	if index != b.left {
		return spread("  "+label, number, w)
	}
	chosen := accent
	if b.screen == overview {
		chosen = focus
	}
	return chosen.Render(spread("▸ "+ansi.Strip(label), number, w))
}

func (b board) rightLines(w, h int) []string {
	switch b.screen {
	case invoice:
		return b.invoiceLines(w)
	case changes:
		return b.changeLines(w, h)
	}
	head := b.summary(w)
	return append(head, b.customerLines(w, h-len(head))...)
}

// summary heads the right-hand side with the selected segment or rule.
func (b board) summary(w int) []string {
	rule := muted.Render(strings.Repeat("─", w))
	if seg, ok := b.segment(); ok {
		t := seg.Totals
		return []string{
			spread(bold.Render(seg.Name), muted.Render(fmt.Sprintf("%s of %s affected",
				model.Thousands(int64(t.Affected)), model.Thousands(int64(t.Customers)))), w),
			rule,
			"radius   " + report.RadiusBar(t) + "  " + report.Radius(t),
			fmt.Sprintf("revenue  %s → %s  (%s, %s)", model.Dollars(t.OldCents), model.Dollars(t.NewCents),
				changeStyle(t.ChangeCents()).Render(model.Dollars(t.ChangeCents())), report.ChangePercent(t)),
			"",
		}
	}
	v := b.verdict()
	outcome := good.Render("✓ " + report.Detail(v))
	if v.Status == rules.Broken {
		outcome = bad.Render("✗ " + report.Detail(v))
	}
	lines := []string{bold.Render(v.Rule.Name), rule, outcome,
		muted.Render(fmt.Sprintf("%-8s", v.Rule.Kind)) + v.Rule.Cond.String()}
	if v.Rule.When != nil {
		lines = append(lines, muted.Render("when    ")+v.Rule.When.String())
	}
	return append(lines, "")
}

func changeStyle(cents int64) lipgloss.Style {
	if cents == 0 {
		return muted
	}
	return warn
}

// customerLines is the table of customers behind the selection, scrolled so
// the selected one is in view.
func (b board) customerLines(w, h int) []string {
	if len(b.rows) == 0 {
		if _, ok := b.segment(); ok {
			return []string{muted.Render("no customers here")}
		}
		return []string{muted.Render("no customer breaks this rule")}
	}
	cols := b.columns(w - 2)
	lines := []string{muted.Render("  " + tableLine(customerHeader, b.widths, cols, moneyColumns))}
	from := max(min(b.row-(h-1)/2, len(b.rows)-(h-1)), 0)
	for at := from; at < min(from+h-1, len(b.rows)); at++ {
		i := b.rows[at]
		cells := b.cells(i)
		if b.screen != overview && at == b.row {
			lines = append(lines, focus.Render("▸ "+tableLine(cells, b.widths, cols, moneyColumns)))
			continue
		}
		cells[4] = changeStyle(b.rep.Result.Rows[i].ChangeCents()).Render(cells[4])
		cells[5] = bad.Render(cells[5])
		lines = append(lines, "  "+tableLine(cells, b.widths, cols, moneyColumns))
	}
	return lines
}

// columns picks the table columns that fit in w cells, giving up the plan
// first and then the old price: which rule a customer breaks matters more
// than either. Only very long rule names are cut short.
func (b board) columns(w int) []int {
	cols := []int{0, 1, 2, 3, 4, 5}
	for _, drop := range []int{1, 2} {
		need := min(b.widths[5], 24)
		for _, c := range cols[:len(cols)-1] {
			need += b.widths[c] + 2
		}
		if need <= w {
			break
		}
		cols = slices.DeleteFunc(cols, func(c int) bool { return c == drop })
	}
	return cols
}

// invoiceLines explains one customer's price: the list price, then every
// discount in the order it was taken, today and as proposed.
func (b board) invoiceLines(w int) []string {
	row := b.rep.Result.Rows[b.rows[b.row]]
	c := row.Customer
	today, proposed := steps(row.Old), steps(row.New)
	widths := widthsOf(append(slices.Clone(today), proposed...)...)
	block := func(title string, rows [][]string) []string {
		out := []string{muted.Render(title)}
		for _, cells := range rows {
			out = append(out, "  "+tableLine(cells, widths, []int{0, 1, 2}, []bool{false, true, true}))
		}
		return out
	}

	lines := []string{
		spread(bold.Render(c.ID), muted.Render(fmt.Sprintf("%d of %d", b.row+1, len(b.rows))), w),
		muted.Render(strings.Repeat("─", w)),
		report.PlanLabel(c),
		"",
	}
	lines = append(lines, block("today · "+b.rep.Before, today)...)
	lines = append(lines, "")
	lines = append(lines, block("proposed · "+b.rep.After, proposed)...)
	lines = append(lines, "", "change   "+changeStyle(row.ChangeCents()).Render(model.Dollars(row.ChangeCents())))
	for _, name := range b.broke[c.ID] {
		lines = append(lines, bad.Render("✗ "+name))
	}
	return lines
}

// steps lays an invoice out as rows of what, how much off, and running total.
func steps(inv sim.Invoice) [][]string {
	rows := [][]string{{"list price", "", model.Dollars(inv.BaseCents)}}
	for _, s := range inv.Steps {
		rows = append(rows, []string{s.Discount, model.Dollars(s.AfterCents - s.BeforeCents), model.Dollars(s.AfterCents)})
	}
	return append(rows, []string{"invoice", "", model.Dollars(inv.TotalCents)})
}

// changeLines lists the pricing settings that differ between the two files.
func (b board) changeLines(w, h int) []string {
	all := b.rep.Changes
	lines := []string{
		spread(bold.Render("Pricing settings that changed"), muted.Render(model.Thousands(int64(len(all)))), w),
		muted.Render(strings.Repeat("─", w)),
	}
	if len(all) == 0 {
		return append(lines, muted.Render("the two pricing files say the same thing"))
	}
	header := []string{"setting", "before", "after"}
	rows := make([][]string, len(all))
	for i, c := range all {
		rows[i] = []string{c.Path, c.Old, c.New}
	}
	widths, cols, left := widthsOf(append(rows, header)...), []int{0, 1, 2}, make([]bool, 3)
	lines = append(lines, muted.Render("  "+tableLine(header, widths, cols, left)))

	room := h - len(lines)
	from := max(min(b.line-room/2, len(all)-room), 0)
	for at := from; at < min(from+room, len(all)); at++ {
		text := tableLine(rows[at], widths, cols, left)
		if at == b.line {
			lines = append(lines, focus.Render("▸ "+text))
			continue
		}
		lines = append(lines, "  "+text)
	}
	return lines
}

func widthsOf(rows ...[]string) []int {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	return widths
}

// tableLine joins the chosen columns of one row, right-aligning the columns
// that right marks. Cells are measured as a terminal shows them, so they may
// already be coloured.
func tableLine(cells []string, widths []int, cols []int, right []bool) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = pad(cells[c], widths[c], right[c])
	}
	return strings.TrimRight(strings.Join(parts, "  "), " ")
}

func pad(s string, w int, right bool) string {
	fill := strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
	if right {
		return fill + s
	}
	return s + fill
}

// fit makes s exactly w cells wide: cut with an ellipsis, or padded.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

// spread sets left and right at opposite ends of w cells. left gives way
// when the two do not fit.
func spread(left, right string, w int) string {
	if right == "" {
		return fit(left, w)
	}
	return fit(fit(left, max(w-lipgloss.Width(right)-1, 0))+" "+right, w)
}

// window is the h lines around line at, so a cursor there stays in view.
func window(lines []string, at, h int) []string {
	if len(lines) <= h {
		return lines
	}
	from := min(max(at-h/2, 0), len(lines)-h)
	return lines[from : from+h]
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// oneLine makes an error safe for the footer. Errors quote from input files,
// so anything that is not visible text, line breaks included, is replaced.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return '?'
	}, s)
}
