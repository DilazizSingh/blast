// Package explain asks a language model running on this machine to put a
// finished report into a few plain sentences. The model is given results
// blast has already computed and nothing else; a summary that states a
// number blast did not compute is thrown away.
package explain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/report"
	"github.com/dilaziz/blast/internal/rules"
)

const (
	// Label heads every summary shown to a person.
	Label = "AI summary (generated, verify against the numbers above)"

	// LocalURL is Ollama on this machine, at its default port.
	// shortcut: the address is fixed, so an Ollama moved to another port
	// with OLLAMA_HOST is not found; honour that variable (loopback
	// addresses only) if anyone needs it.
	LocalURL = "http://127.0.0.1:11434"

	// DefaultModel is the model Ollama's own documentation uses.
	DefaultModel = "gemma4"

	maxAnswer = 1 << 20
)

const instructions = `You explain the result of a pricing-change simulation to a busy manager.
Write at most four plain sentences. Use only the facts you are given.
Copy every number exactly as written. Never calculate, estimate, round or invent a number.
Do not give advice. No Markdown, no lists.`

// Summary returns a few sentences about r from the model named, served by
// the Ollama at url. The error, when there is one, is written to be shown
// to the user as it stands.
func Summary(url, name string, r report.Report) (string, error) {
	given := facts(r)
	answer, err := generate(url, name, given)
	if err != nil {
		return "", err
	}
	answer = clean(answer)
	if answer == "" {
		return "", errors.New("the model returned nothing")
	}
	if n := inventedNumber(answer, given); n != "" {
		return "", fmt.Errorf("the summary was withheld: it states a number (%s) that blast did not compute", n)
	}
	return answer, nil
}

// facts is everything the model is told. It holds totals and counts only:
// no customer, and nothing from a customer's row, appears in it.
func facts(r report.Report) string {
	t := r.Result.Totals
	var b strings.Builder
	fmt.Fprintf(&b, "Pricing change: %s -> %s\n", r.Before, r.After)
	fmt.Fprintf(&b, "Customers simulated: %s\n", model.Thousands(int64(t.Customers)))
	fmt.Fprintf(&b, "Customers whose invoice changes: %s (%s)\n", model.Thousands(int64(t.Affected)), report.Radius(t))
	fmt.Fprintf(&b, "Revenue per billing cycle: %s before, %s after, change %s (%s)\n",
		model.Dollars(t.OldCents), model.Dollars(t.NewCents), model.Dollars(t.ChangeCents()), report.ChangePercent(t))
	b.WriteString("Segments:\n")
	for _, s := range r.Result.Segments {
		fmt.Fprintf(&b, "- %s: %s of %s customers affected, revenue change %s\n", s.Name,
			model.Thousands(int64(s.Affected)), model.Thousands(int64(s.Customers)), model.Dollars(s.ChangeCents()))
	}
	b.WriteString("Rules:\n")
	for _, v := range r.Verdicts {
		outcome := "passes"
		switch {
		case v.Status == rules.NotTriggered:
			outcome = "did not apply to this change"
		case v.Status == rules.Broken && len(v.Customers) > 0:
			outcome = "BROKEN by " + report.Plural(len(v.Customers), "customer")
		case v.Status == rules.Broken:
			outcome = "BROKEN"
		}
		fmt.Fprintf(&b, "- %s: %s\n", v.Rule.Name, outcome)
	}
	if len(r.Verdicts) == 0 {
		b.WriteString("- none defined\n")
	}
	return b.String()
}

// generate sends one request to Ollama and returns the model's text.
func generate(url, name, prompt string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": name, "system": instructions, "prompt": prompt,
		"stream": false, "options": map[string]any{"temperature": 0},
	})
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: 2 * time.Minute, // a first run loads the model from disk
		// The facts go to this machine and no further: no proxy, no redirect.
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Post(url+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		// Almost always this means it is not installed or not started.
		return "", fmt.Errorf("no answer from Ollama on this machine; "+
			"if it is not installed, get it from ollama.com, then run \"ollama pull %s\"", name)
	}
	defer resp.Body.Close()

	var answer struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err == nil {
		err = json.Unmarshal(raw, &answer)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", fmt.Errorf("the model %q is not in Ollama; run \"ollama pull %s\" or choose another with --explain-model", name, name)
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("refused by Ollama (HTTP %d): %s", resp.StatusCode, clean(answer.Error))
	case err != nil:
		return "", fmt.Errorf("unreadable answer from Ollama: %w", err)
	}
	return answer.Response, nil
}

var number = regexp.MustCompile(`\d+(?:,\d{3})*(?:\.\d+)?`)

// inventedNumber returns the first number in answer that does not appear in
// given, or "". Formatting may differ ($5,229.00 and 5229 are the same
// number); the value may not.
func inventedNumber(answer, given string) string {
	known := map[string]bool{}
	for _, n := range number.FindAllString(given, -1) {
		known[canonical(n)] = true
	}
	for _, n := range number.FindAllString(answer, -1) {
		if !known[canonical(n)] {
			return n
		}
	}
	return ""
}

func canonical(n string) string {
	n = strings.TrimLeft(strings.ReplaceAll(n, ",", ""), "0")
	if strings.Contains(n, ".") {
		n = strings.TrimSuffix(strings.TrimRight(n, "0"), ".")
	}
	if n == "" || n[0] == '.' {
		n = "0" + n
	}
	return n
}

// clean makes the model's text safe and tidy to print: one paragraph of
// visible characters. What a model writes is no more trusted than a file.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsPrint(r):
			return r
		}
		return -1
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
