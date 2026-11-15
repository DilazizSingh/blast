package report

import (
	"encoding/json"
	"io"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/sim"
)

// jsonVersion is bumped whenever a field is removed or changes meaning.
const jsonVersion = 1

type jsonReport struct {
	Version       int            `json:"version"`
	Before        string         `json:"before"`
	After         string         `json:"after"`
	ExitCode      int            `json:"exit_code"`
	Totals        jsonTotals     `json:"totals"`
	Segments      []jsonSegment  `json:"segments"`
	Rules         []jsonRule     `json:"rules"`
	Customers     []jsonCustomer `json:"customers"`
	ConfigChanges []jsonChange   `json:"config_changes"`
}

// Money is integer cents here as everywhere else: a consumer never has to
// round-trip a float.
type jsonTotals struct {
	Customers   int   `json:"customers"`
	Affected    int   `json:"affected"`
	OldCents    int64 `json:"old_cents"`
	NewCents    int64 `json:"new_cents"`
	ChangeCents int64 `json:"change_cents"`
}

type jsonSegment struct {
	Name string `json:"name"`
	jsonTotals
}

type jsonRule struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Condition string   `json:"condition"`
	When      string   `json:"when,omitempty"`
	Status    string   `json:"status"`
	Customers []string `json:"customers"`
}

type jsonCustomer struct {
	ID           string   `json:"id"`
	Plan         string   `json:"plan"`
	Billing      string   `json:"billing"`
	Quantity     int64    `json:"quantity"`
	OldCents     int64    `json:"old_cents"`
	NewCents     int64    `json:"new_cents"`
	ChangeCents  int64    `json:"change_cents"`
	OldDiscounts []string `json:"old_discounts"`
	NewDiscounts []string `json:"new_discounts"`
	BrokenRules  []string `json:"broken_rules"`
}

type jsonChange struct {
	Path string  `json:"path"`
	Old  *string `json:"old"` // null when the setting did not exist before
	New  *string `json:"new"` // null when the setting no longer exists
}

// JSON writes the report for other programs. Unlike the text and Markdown
// reports it lists every customer whose invoice changes or who breaks a rule,
// however many there are.
func JSON(w io.Writer, r Report) error {
	out := jsonReport{
		Version:       jsonVersion,
		Before:        r.Before,
		After:         r.After,
		ExitCode:      r.ExitCode(),
		Totals:        totals(r.Result.Totals),
		Segments:      make([]jsonSegment, len(r.Result.Segments)),
		Rules:         make([]jsonRule, len(r.Verdicts)),
		Customers:     []jsonCustomer{},
		ConfigChanges: make([]jsonChange, len(r.Changes)),
	}
	for i, s := range r.Result.Segments {
		out.Segments[i] = jsonSegment{Name: s.Name, jsonTotals: totals(s.Totals)}
	}
	for i, v := range r.Verdicts {
		rule := jsonRule{
			Name:      v.Rule.Name,
			Kind:      v.Rule.Kind.String(),
			Condition: v.Rule.Cond.String(),
			Status:    v.Status.String(),
			Customers: orEmpty(v.Customers),
		}
		if v.Rule.When != nil {
			rule.When = v.Rule.When.String()
		}
		out.Rules[i] = rule
	}
	broke := r.BrokenBy()
	for _, row := range r.notable() {
		c := row.Customer
		out.Customers = append(out.Customers, jsonCustomer{
			ID: c.ID, Plan: c.Plan, Billing: string(c.Billing), Quantity: c.Units(),
			OldCents: row.Old.TotalCents, NewCents: row.New.TotalCents, ChangeCents: row.ChangeCents(),
			OldDiscounts: discounts(row.Old), NewDiscounts: discounts(row.New),
			BrokenRules: orEmpty(broke[c.ID]),
		})
	}
	for i, c := range r.Changes {
		out.ConfigChanges[i] = jsonChange{Path: c.Path, Old: orNull(c.Old), New: orNull(c.New)}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func totals(t diff.Totals) jsonTotals {
	return jsonTotals{
		Customers: t.Customers, Affected: t.Affected,
		OldCents: t.OldCents, NewCents: t.NewCents, ChangeCents: t.ChangeCents(),
	}
}

func discounts(inv sim.Invoice) []string {
	names := make([]string, len(inv.Steps))
	for i, s := range inv.Steps {
		names[i] = s.Discount
	}
	return names
}

// orEmpty keeps an absent list as [] rather than null.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
