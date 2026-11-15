package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dilaziz/blast/internal/explain"
)

func TestExplainAddsALabelledSummaryAndChangesNothingElse(t *testing.T) {
	t.Chdir(t.TempDir())
	blast(t, "init")
	plain, _, plainCode := blast(t, "check")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"response":"Revenue falls by $644.40 (-12.3%) across 5 of 10 customers, and the change breaks two of your rules: one about stacking discounts and one about repricing legacy plans."}`)
	}))
	ollamaURL = srv.URL
	t.Cleanup(func() { ollamaURL = explain.LocalURL })

	stdout, stderr, code := blast(t, "check", "--explain")
	if code != plainCode || stderr != "" {
		t.Errorf("exit code %d, stderr %q; want %d and silence", code, stderr, plainCode)
	}
	const summary = `
 AI summary (generated, verify against the numbers above)
 Revenue falls by $644.40 (-12.3%) across 5 of 10 customers, and the change
 breaks two of your rules: one about stacking discounts and one about
 repricing legacy plans.
`
	if stdout != plain+summary {
		t.Errorf("got:\n%s\nwant the plain report followed by:\n%s", stdout, summary)
	}

	// Without Ollama the report and the exit code are exactly what they were.
	srv.Close()
	stdout, stderr, code = blast(t, "check", "--explain")
	if stdout != plain || code != plainCode {
		t.Errorf("with Ollama gone: exit code %d, output changed:\n%s", code, stdout)
	}
	wantLines(t, stderr, "blast: no AI summary: no answer from Ollama on this machine")

	_, stderr, code = blast(t, "check", "--explain", "--format", "md")
	if code != exitBadInput || !strings.Contains(stderr, "--explain works with the text format only") {
		t.Errorf("--explain with Markdown: exit code %d, stderr %q", code, stderr)
	}
}
