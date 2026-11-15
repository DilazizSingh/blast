package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/load"
	"github.com/dilaziz/blast/internal/report"
	"github.com/dilaziz/blast/internal/rules"
)

// acme simulates the plan's example against one of its "after" files. With
// copies above one, every customer is repeated that many times, to stand in
// for a list too long for the screen.
func acme(t *testing.T, after string, copies int) report.Report {
	t.Helper()
	const dir = "../../examples/acme/"
	s, err := load.Open(load.Inputs{
		Before: dir + "pricing.old.yml", After: dir + after,
		Customers: dir + "customers.json", Rules: dir + "rules.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	customers := s.Customers
	for n := 1; n < copies; n++ {
		for _, c := range s.Customers {
			c.ID = fmt.Sprintf("%s-%d", c.ID, n)
			customers = append(customers, c)
		}
	}
	res, err := diff.Compare(customers, s.Before, s.After, s.Segments)
	if err != nil {
		t.Fatal(err)
	}
	changes := diff.Configs(s.Before, s.After)
	return report.Report{
		Before: "pricing.old.yml", After: after, Result: res, Changes: changes,
		Verdicts: rules.Check(s.Rules, res, rules.Context{Changes: changes}),
	}
}

func open(rep report.Report, rerun func() (report.Report, error)) board {
	b := newBoard(Options{Name: "blast", First: rep, Rerun: rerun}, nil)
	return send(b, tea.WindowSizeMsg{Width: 120, Height: 30})
}

func send(b board, msg tea.Msg) board {
	next, _ := b.Update(msg)
	return next.(board)
}

var special = map[string]rune{
	"up": tea.KeyUp, "down": tea.KeyDown, "enter": tea.KeyEnter, "esc": tea.KeyEscape, "end": tea.KeyEnd, "home": tea.KeyHome,
}

func press(t *testing.T, b board, names ...string) board {
	t.Helper()
	for _, name := range names {
		msg := tea.KeyPressMsg{Code: special[name]}
		if msg.Code == 0 {
			msg = tea.KeyPressMsg{Code: rune(name[0]), Text: name}
		}
		// Guards this test's own assumption about how Bubble Tea names keys.
		if msg.String() != name {
			t.Fatalf("key %q reads as %q", name, msg.String())
		}
		b = send(b, msg)
	}
	return b
}

func screenText(b board) string { return ansi.Strip(b.render()) }

func wantOnScreen(t *testing.T, b board, wants ...string) {
	t.Helper()
	text := screenText(b)
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("screen does not show %q:\n%s", want, text)
		}
	}
}

func TestOverviewShowsTheExample(t *testing.T) {
	b := open(acme(t, "pricing.new.yml", 1), nil)
	wantOnScreen(t, b,
		"╭─ blast ─ pricing.old.yml → pricing.new.yml ─",
		"▸ all customers", "annual + referred", "✗ max-one-discount", "✓ finance-approval",
		"5 of 10 affected", "50%",
		"revenue  $5,229.00 → $4,584.60  (-$644.40, -12.3%)",
		"c4        pro annual         $891.00  $712.80  -$178.20  ✗ max-one-discount",
		"enter drill in", "✗ 2 of 3 rules broken",
	)

	// In a narrower window the plan gives way before the rule names do.
	b = send(b, tea.WindowSizeMsg{Width: 100, Height: 24})
	wantOnScreen(t, b, "c4        $891.00  $712.80  -$178.20  ✗ max-one-discount")
}

func TestDrillDownToWhyThisPrice(t *testing.T) {
	b := open(acme(t, "pricing.new.yml", 1), nil)

	// Largest change first: c3, then c4.
	b = press(t, b, "enter", "down")
	wantOnScreen(t, b, "▸ c4", "enter why this price")

	b = press(t, b, "enter")
	wantOnScreen(t, b,
		"c4", "2 of 10", "pro annual",
		"today · pricing.old.yml",
		"list price            $990.00",
		"WELCOME10    -$99.00  $891.00",
		"proposed · pricing.new.yml",
		"REFERRAL20  -$178.20  $712.80",
		"invoice               $712.80",
		"change   -$178.20", "✗ max-one-discount",
	)

	// The invoice screen steps through the same list.
	b = press(t, b, "down")
	wantOnScreen(t, b, "c6", "3 of 10", "✗ no-legacy-repricing")

	b = press(t, b, "esc", "esc")
	if b.screen != overview {
		t.Errorf("after backing out twice the screen is %d, want the overview", b.screen)
	}
}

func TestSelectingARuleListsWhoBreaksIt(t *testing.T) {
	b := open(acme(t, "pricing.new.yml", 1), nil)
	b = press(t, b, "down", "down", "down", "down", "down") // past four segments
	wantOnScreen(t, b,
		"▸ ✗ max-one-discount", "✗ 2 customers: c4, c7",
		"never   customer.discount_count > 1",
	)
	if got := len(b.rows); got != 2 {
		t.Errorf("the rule lists %d customers, want the 2 who break it", got)
	}

	// A rule about the change as a whole has no customers to drill into.
	b = press(t, b, "end", "enter")
	wantOnScreen(t, b, "▸ ✓ finance-approval", "✓ not triggered", "no customer breaks this rule")
	if b.screen != overview {
		t.Error("enter drilled into an empty list")
	}
}

func TestConfigDiff(t *testing.T) {
	b := press(t, open(acme(t, "pricing.new.yml", 1), nil), "enter", "d")
	wantOnScreen(t, b,
		"Pricing settings that changed",
		"▸ discounts.REFERRAL20.percent          20%",
		`discounts.REFERRAL20.when             billing == "annual" and referred`,
	)
	if b = press(t, b, "esc"); b.screen != customers {
		t.Errorf("esc returned to screen %d, want the one d was pressed on", b.screen)
	}
}

// The plan's "done when" for the dashboard: fix the pricing file and watch
// the rules flip.
func TestRerunFlipsTheVerdict(t *testing.T) {
	fixed := acme(t, "pricing.fixed.yml", 1)
	b := open(acme(t, "pricing.new.yml", 1), func() (report.Report, error) { return fixed, nil })
	wantOnScreen(t, b, "✗ 2 of 3 rules broken")

	next, run := b.Update(filesChanged{})
	b = next.(board)
	wantOnScreen(t, b, "simulating…")
	// A second change while the first run is in flight starts nothing new...
	if again, cmd := b.simulate(); cmd != nil || !again.stale {
		t.Fatal("a run started while another was in flight")
	} else {
		b = again
	}

	next, rerun := b.Update(result{rep: fixed})
	b = next.(board)
	wantOnScreen(t, b, "✓ all rules pass", "2 of 10 affected", "revenue  $5,229.00 → $4,973.00  (-$256.00, -4.9%)")
	// ...but is not forgotten, and the flip is highlighted.
	if run == nil || rerun == nil || !b.busy {
		t.Error("the change that arrived mid-run was dropped")
	}
	if b.flash%2 != 1 {
		t.Error("the verdict flipped without a flash")
	}
	if b = send(b, flashOver(b.flash)); b.flash%2 != 0 {
		t.Error("the flash did not end")
	}
}

func TestFailedRerunKeepsTheLastGoodResult(t *testing.T) {
	b := open(acme(t, "pricing.new.yml", 1), nil)
	b = send(b, result{err: errors.New("pricing.new.yml line 3: unknown plan \x1b[2J\"pro\"\nplus")})
	wantOnScreen(t, b,
		`✗ pricing.new.yml line 3: unknown plan ?[2J"pro"?plus`, "showing the last good run",
		"revenue  $5,229.00 → $4,584.60", // still there
	)
	if strings.Contains(b.hint(), "\x1b[2J") {
		t.Error("an escape sequence from an error message reached the screen")
	}
	b = send(b, result{rep: acme(t, "pricing.fixed.yml", 1)})
	wantOnScreen(t, b, "✓ all rules pass", "enter drill in")
}

// Whatever is on screen, the frame must be exactly the size of the window:
// a line one cell too long wraps and tears the whole display.
func TestEveryFrameFitsTheWindow(t *testing.T) {
	views := map[string][]string{
		"overview":        nil,
		"a segment":       {"down", "down", "down"},
		"a rule":          {"end"},
		"customers":       {"enter", "end"},
		"why this price":  {"enter", "down", "enter"},
		"config diff":     {"d"},
		"long list, end":  {"enter", "end", "enter"},
		"long list, home": {"enter", "end", "home"},
	}
	for name, keys := range views {
		for _, size := range [][2]int{{64, 14}, {80, 24}, {100, 30}, {160, 50}} {
			copies := 1
			if strings.HasPrefix(name, "long") {
				copies = 60
			}
			b := open(acme(t, "pricing.new.yml", copies), nil)
			b = press(t, send(b, tea.WindowSizeMsg{Width: size[0], Height: size[1]}), keys...)
			lines := strings.Split(screenText(b), "\n")
			if len(lines) != size[1] {
				t.Errorf("%s at %dx%d: %d lines", name, size[0], size[1], len(lines))
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got != size[0] {
					t.Errorf("%s at %dx%d: line %d is %d cells wide: %q", name, size[0], size[1], i+1, got, line)
					break
				}
			}
		}
	}

	small := send(open(acme(t, "pricing.new.yml", 1), nil), tea.WindowSizeMsg{Width: 40, Height: 10})
	if got, want := small.render(), "blast needs a window of at least 64×14 cells; this one is 40×10."; got != want {
		t.Errorf("small window: %q, want %q", got, want)
	}
}

func TestLongListKeepsTheSelectionInView(t *testing.T) {
	b := press(t, open(acme(t, "pricing.new.yml", 60), nil), "enter", "end")
	if b.row != 599 {
		t.Fatalf("end moved to row %d of 600", b.row)
	}
	last := b.rep.Result.Rows[b.rows[b.row]].Customer.ID
	wantOnScreen(t, b, "▸ "+last)
}

func TestWatchReportsSavesHoweverTheEditorWrites(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pricing.new.yml")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(file, []byte("a"), 0o644))
	changed, stop, err := watch([]string{file})
	must(err)
	defer stop()
	reported := func(within time.Duration) bool {
		select {
		case <-changed:
			return true
		case <-time.After(within):
			return false
		}
	}

	must(os.WriteFile(file, []byte("b"), 0o644))
	if !reported(5 * time.Second) {
		t.Error("a write in place went unreported")
	}
	// How most editors save: write a new file, rename it over the old one.
	must(os.WriteFile(file+".tmp", []byte("c"), 0o644))
	must(os.Rename(file+".tmp", file))
	if !reported(5 * time.Second) {
		t.Error("a save by rename went unreported")
	}
	must(os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644))
	if reported(4 * settle) {
		t.Error("a change to a file nobody asked about was reported")
	}
}
