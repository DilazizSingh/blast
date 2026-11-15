package explain

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/load"
	"github.com/dilaziz/blast/internal/report"
	"github.com/dilaziz/blast/internal/rules"
)

func acme(t *testing.T) report.Report {
	t.Helper()
	const dir = "../../examples/acme/"
	s, err := load.Open(load.Inputs{
		Before: dir + "pricing.old.yml", After: dir + "pricing.new.yml",
		Customers: dir + "customers.json", Rules: dir + "rules.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := diff.Compare(s.Customers, s.Before, s.After, s.Segments)
	if err != nil {
		t.Fatal(err)
	}
	return report.Report{Before: "pricing.old.yml", After: "pricing.new.yml", Result: res,
		Verdicts: rules.Check(s.Rules, res, rules.Context{})}
}

// ollama stands in for a local Ollama that answers every prompt with reply.
func ollama(t *testing.T, status int, reply string) (url string, prompt *string) {
	t.Helper()
	prompt = new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model, Prompt, System string
			Stream                *bool
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil || r.Method != http.MethodPost || r.URL.Path != "/api/generate" {
			t.Errorf("%s %s with body %s: %v", r.Method, r.URL.Path, body, err)
		}
		if req.Model != "gemma4" || req.Stream == nil || *req.Stream || req.System != instructions {
			t.Errorf("unexpected request: %s", body)
		}
		*prompt = req.Prompt
		w.WriteHeader(status)
		fmt.Fprint(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, prompt
}

func answer(text string) string {
	out, _ := json.Marshal(map[string]any{"response": text, "done": true})
	return string(out)
}

func TestSummaryIsGivenResultsAndNeverCustomers(t *testing.T) {
	const said = "This change lowers revenue by $644.40 (-12.3%) and touches 5 of 10 customers. Two rules are broken."
	url, prompt := ollama(t, http.StatusOK, answer("  "+said+"\n\n"))
	got, err := Summary(url, "gemma4", acme(t))
	if err != nil || got != said {
		t.Fatalf("got %q, %v", got, err)
	}

	const want = `Pricing change: pricing.old.yml -> pricing.new.yml
Customers simulated: 10
Customers whose invoice changes: 5 (50%)
Revenue per billing cycle: $5,229.00 before, $4,584.60 after, change -$644.40 (-12.3%)
Segments:
- annual + referred: 5 of 5 customers affected, revenue change -$644.40
- monthly: 0 of 3 customers affected, revenue change $0.00
- legacy plans: 1 of 2 customers affected, revenue change -$158.00
- has coupon: 2 of 2 customers affected, revenue change -$230.40
Rules:
- max-one-discount: BROKEN by 2 customers
- no-legacy-repricing: BROKEN by 1 customer
- finance-approval: did not apply to this change
`
	if *prompt != want {
		t.Errorf("the model was told:\n%s\nwant:\n%s", *prompt, want)
	}
	// The example's customers are c1 to c10. None may be named to the model.
	if id := regexp.MustCompile(`\bc\d+\b`).FindString(*prompt); id != "" {
		t.Errorf("customer %s was sent to the model", id)
	}
}

func TestSummaryWithAnInventedNumberIsWithheld(t *testing.T) {
	cases := map[string]string{
		"This will cost about $650 per cycle.":                 "650",
		"Revenue falls $644.40, or $7,732.80 a year.":          "7,732.80",
		"5 of 10 customers are affected, which is 51%.":        "51",
		"Expect churn to rise 7% because 5 customers pay less": "7",
	}
	for said, invented := range cases {
		url, _ := ollama(t, http.StatusOK, answer(said))
		_, err := Summary(url, "gemma4", acme(t))
		want := fmt.Sprintf("the summary was withheld: it states a number (%s) that blast did not compute", invented)
		if err == nil || err.Error() != want {
			t.Errorf("%q:\n got  %v\n want %s", said, err, want)
		}
	}

	// The guard knows which numbers blast computed, not what each one means:
	// the monthly segment has 3 customers, so a made-up "3%" gets through.
	// That limit is why every summary carries its label.
	url, _ := ollama(t, http.StatusOK, answer("Expect churn to rise 3%."))
	if _, err := Summary(url, "gemma4", acme(t)); err != nil {
		t.Errorf("the guard is stricter than documented: %v", err)
	}

	// The same value written another way is not an invention.
	for _, said := range []string{"Revenue goes from $5229 to $4,584.6.", "It changes 5 of 10 customers (50%), or -12.30%."} {
		url, _ := ollama(t, http.StatusOK, answer(said))
		if _, err := Summary(url, "gemma4", acme(t)); err != nil {
			t.Errorf("%q was withheld: %v", said, err)
		}
	}
}

func TestModelOutputCannotReachTheTerminalRaw(t *testing.T) {
	url, _ := ollama(t, http.StatusOK, answer("Fine.\x1b[2J\x1b[H All rules pass.\u202e\r\nTruly."))
	got, err := Summary(url, "gemma4", acme(t))
	if want := "Fine.[2J[H All rules pass. Truly."; err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
}

func TestOllamaProblemsAreExplained(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	_, err := Summary(closed.URL, "gemma4", acme(t))
	if want := `Ollama is not running on this machine; install it from ollama.com, then run "ollama pull gemma4"`; err == nil || err.Error() != want {
		t.Errorf("not running:\n got  %v\n want %s", err, want)
	}

	cases := []struct {
		status      int
		reply, want string
	}{
		{http.StatusNotFound, `{"error":"model 'gemma4' not found"}`,
			`Ollama does not have the model "gemma4"; run "ollama pull gemma4" or choose another with --explain-model`},
		{http.StatusInternalServerError, `{"error":"out of memory\u001b[2J"}`, "Ollama refused (HTTP 500): out of memory[2J"},
		{http.StatusOK, answer(" \n "), "the model returned nothing"},
		{http.StatusOK, "<html>", "Ollama gave an answer that could not be read: "},
	}
	for _, tc := range cases {
		url, _ := ollama(t, tc.status, tc.reply)
		if _, err := Summary(url, "gemma4", acme(t)); err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("HTTP %d %s:\n got  %v\n want %s", tc.status, tc.reply, err, tc.want)
		}
	}
}

func TestCanonical(t *testing.T) {
	for in, want := range map[string]string{
		"5,229.00": "5229", "5229": "5229", "644.40": "644.4", "0.50": "0.5", "0": "0", "0.0": "0", "007": "7", "10": "10", "12.3": "12.3",
	} {
		if got := canonical(in); got != want {
			t.Errorf("canonical(%q) = %q, want %q", in, got, want)
		}
	}
}
