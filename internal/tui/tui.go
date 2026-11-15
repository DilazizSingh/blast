// Package tui is the full-screen dashboard: the results `blast check` prints,
// made browsable, and simulated again whenever an input file changes.
package tui

import (
	"cmp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/report"
	"github.com/dilaziz/blast/internal/rules"
)

// Options is what the dashboard needs from its caller.
type Options struct {
	Name  string                        // the tool's name, shown in the frame
	First report.Report                 // what to show on opening
	Rerun func() (report.Report, error) // simulates again from the files
	Watch []string                      // files whose change calls for a rerun
}

// Run shows the dashboard until the user quits.
func Run(o Options) error {
	// Without a watcher the dashboard still works; it just waits for `r`.
	changed, stop, err := watch(o.Watch)
	if err == nil {
		defer stop()
	}
	_, err = tea.NewProgram(newBoard(o, changed)).Run()
	return err
}

type screen int

const (
	overview  screen = iota // choosing a segment or a rule on the left
	customers               // moving through the customers behind that choice
	invoice                 // why one customer pays what they pay
	changes                 // the pricing settings that differ
)

// flashFor is how long the verdict stays highlighted after it flips.
const flashFor = 900 * time.Millisecond

type (
	filesChanged struct{}
	result       struct {
		rep report.Report
		err error
	}
	flashOver int // the flash it ends, so a stale timer cannot end a newer one
)

type board struct {
	name    string
	rerun   func() (report.Report, error)
	changed <-chan struct{} // nil when the files could not be watched

	rep   report.Report
	broke map[string][]string // rep.BrokenBy(), kept rather than rebuilt per frame
	err   error               // why the latest run failed; rep is then the last good one
	busy  bool                // a run is in flight
	stale bool                // the files changed again during that run
	flash int                 // counts verdict flips; odd while one is highlighted

	screen screen
	back   screen // where esc returns to from the changes screen
	left   int    // selection on the left: all customers, each segment, then each rule
	rows   []int  // rep.Result.Rows behind that selection, largest change first
	widths []int  // column widths of the table of those rows
	row    int    // selection within rows
	line   int    // selection within the changed settings

	width, height int
}

func newBoard(o Options, changed <-chan struct{}) board {
	b := board{name: o.Name, rerun: o.Rerun, changed: changed}
	b.show(o.First)
	return b
}

func (b board) Init() tea.Cmd { return b.await() }

func (b board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return b.key(msg.String())
	case filesChanged:
		next, cmd := b.simulate()
		return next, tea.Batch(cmd, b.await())
	case result:
		return b.accept(msg)
	case flashOver:
		if int(msg) == b.flash {
			b.flash++
		}
	}
	return b, nil
}

func (b board) key(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "q", "ctrl+c":
		return b, tea.Quit
	case "r":
		return b.simulate()
	case "d":
		if b.screen != changes {
			b.back, b.screen = b.screen, changes
		}
	case "up", "k":
		b.move(-1)
	case "down", "j":
		b.move(1)
	case "pgup":
		b.move(-b.page())
	case "pgdown":
		b.move(b.page())
	case "home", "g":
		b.move(-len(b.rep.Result.Rows) - b.entries())
	case "end", "G":
		b.move(len(b.rep.Result.Rows) + b.entries())
	case "enter", "right", "l":
		switch {
		case b.screen == overview && len(b.rows) > 0:
			b.screen = customers
		case b.screen == customers:
			b.screen = invoice
		}
	case "esc", "left", "h", "backspace":
		switch b.screen {
		case changes:
			b.screen = b.back
		case invoice:
			b.screen = customers
		case customers:
			b.screen = overview
		}
	}
	return b, nil
}

// move shifts whichever selection the current screen is about.
func (b *board) move(by int) {
	switch b.screen {
	case overview:
		b.left = clamp(b.left+by, b.entries())
		b.choose()
		b.row = 0
	case changes:
		b.line = clamp(b.line+by, len(b.rep.Changes))
	default: // the invoice screen steps through the same customers
		b.row = clamp(b.row+by, len(b.rows))
	}
}

func (b board) page() int { return max(b.height-10, 1) }

func clamp(i, n int) int { return max(min(i, n-1), 0) }

// simulate starts a run. While one is in flight it only notes that another
// is due, so results can never arrive out of order.
func (b board) simulate() (board, tea.Cmd) {
	if b.busy {
		b.stale = true
		return b, nil
	}
	b.busy = true
	return b, func() tea.Msg {
		rep, err := b.rerun()
		return result{rep, err}
	}
}

// await waits for the watcher to report a change.
func (b board) await() tea.Cmd {
	if b.changed == nil {
		return nil
	}
	return func() tea.Msg {
		<-b.changed
		return filesChanged{}
	}
}

// accept takes in a finished run. A failed one leaves the last good result
// on screen under its error: half-saved files are normal while editing.
func (b board) accept(res result) (tea.Model, tea.Cmd) {
	b.busy = false
	var again tea.Cmd
	if b.stale {
		b.stale = false
		b, again = b.simulate()
	}
	if b.err = res.err; res.err != nil {
		return b, again
	}

	wasBroken := b.rep.Broken() > 0
	b.show(res.rep)
	if wasBroken == (b.rep.Broken() > 0) {
		return b, again
	}
	b.flash += 1 + b.flash%2 // to the next odd number
	flash := flashOver(b.flash)
	return b, tea.Batch(again, tea.Tick(flashFor, func(time.Time) tea.Msg { return flash }))
}

// show replaces the result on screen, keeping every selection inside it.
func (b *board) show(rep report.Report) {
	b.rep, b.broke = rep, rep.BrokenBy()
	b.left = clamp(b.left, b.entries())
	b.choose()
	b.row = clamp(b.row, len(b.rows))
	b.line = clamp(b.line, len(rep.Changes))
	if len(b.rows) == 0 && (b.screen == customers || b.screen == invoice) {
		b.screen = overview
	}
}

// entries is how many things the left-hand side lists.
func (b board) entries() int { return 1 + len(b.rep.Result.Segments) + len(b.rep.Verdicts) }

// segment is the segment selected on the left. ok is false when a rule is
// selected instead; verdict returns that.
func (b board) segment() (seg diff.Segment, ok bool) {
	res := b.rep.Result
	switch {
	case b.left == 0:
		return diff.Segment{Name: "all customers", Totals: res.Totals}, true
	case b.left <= len(res.Segments):
		return res.Segments[b.left-1], true
	}
	return diff.Segment{}, false
}

func (b board) verdict() rules.Verdict {
	return b.rep.Verdicts[b.left-1-len(b.rep.Result.Segments)]
}

// choose lists the customers behind the selection on the left: a segment's
// members, or the customers who break a rule.
func (b *board) choose() {
	all := b.rep.Result.Rows
	var rows []int
	switch seg, ok := b.segment(); {
	case b.left == 0:
		rows = make([]int, len(all))
		for i := range rows {
			rows[i] = i
		}
	case ok:
		rows = slices.Clone(seg.Rows)
	default:
		breaks := make(map[string]bool)
		for _, id := range b.verdict().Customers {
			breaks[id] = true
		}
		for i, row := range all {
			if breaks[row.Customer.ID] {
				rows = append(rows, i)
			}
		}
	}
	slices.SortStableFunc(rows, func(x, y int) int {
		return cmp.Compare(abs(all[y].ChangeCents()), abs(all[x].ChangeCents()))
	})

	b.rows = rows
	// Widths come from every row, not only those in view, so the columns
	// hold still while the list scrolls.
	b.widths = widthsOf(customerHeader)
	for _, i := range rows {
		for col, cell := range b.cells(i) {
			b.widths[col] = max(b.widths[col], lipgloss.Width(cell))
		}
	}
}

func abs(n int64) int64 { return max(n, -n) }

var (
	customerHeader = []string{"customer", "plan", "old", "new", "change", ""}
	moneyColumns   = []bool{false, false, true, true, true, false}
)

// cells is one customer's line in the table.
func (b board) cells(i int) []string {
	row := b.rep.Result.Rows[i]
	broken := ""
	if names := b.broke[row.Customer.ID]; len(names) > 0 {
		broken = "✗ " + strings.Join(names, ", ")
	}
	return []string{
		row.Customer.ID, report.PlanLabel(row.Customer),
		model.Dollars(row.Old.TotalCents), model.Dollars(row.New.TotalCents), model.Dollars(row.ChangeCents()),
		broken,
	}
}
