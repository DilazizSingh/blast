package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dilaziz/blast/internal/diff"
	"github.com/dilaziz/blast/internal/explain"
	"github.com/dilaziz/blast/internal/load"
	"github.com/dilaziz/blast/internal/report"
	"github.com/dilaziz/blast/internal/rules"
)

// formats are the ways a report can be written.
var formats = map[string]func(io.Writer, report.Report) error{
	"text": report.Text,
	"md":   report.Markdown,
	"json": report.JSON,
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	available := strings.Join(slices.Sorted(maps.Keys(formats)), ", ")

	fs := flag.NewFlagSet(name+" check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sel, err := selectInputs(fs)
	if err != nil {
		return fail(stderr, err)
	}
	format := fs.String("format", "text", "output format: "+available)
	noColor := fs.Bool("no-color", false, "plain output, even on a terminal")
	explainIt := fs.Bool("explain", false, "add a short AI summary from an Ollama on this machine (optional, text format only)")
	explainModel := fs.String("explain-model", explain.DefaultModel, "the Ollama model --explain asks")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return fail(stderr, fmt.Errorf("check takes flags only, not %q", fs.Arg(0)))
	}
	render, ok := formats[*format]
	switch {
	case !ok:
		return fail(stderr, fmt.Errorf("unknown format %q (available: %s)", *format, available))
	case *explainIt && *format != "text":
		// Generated prose has no place in output other programs read or
		// that a bot posts under its own name.
		return fail(stderr, errors.New("--explain works with the text format only"))
	}

	if err := sel.resolve(fs); err != nil {
		return fail(stderr, err)
	}
	rep, err := check(sel.Inputs, sel.approvals())
	if err != nil {
		return fail(stderr, err)
	}
	out := terminal(stdout, *noColor)
	if err := render(out, rep); err != nil {
		return fail(stderr, err)
	}
	if *explainIt {
		summarize(out, stderr, rep, *explainModel)
	}
	return rep.ExitCode()
}

// ollamaURL is where --explain looks for a model: this machine. Tests point
// it at a stand-in; nothing else can.
var ollamaURL = explain.LocalURL

// summarize adds the AI paragraph under the report. Not getting one is
// reported and changes nothing else: not the report, not the exit code.
func summarize(stdout, stderr io.Writer, rep report.Report, model string) {
	text, err := explain.Summary(ollamaURL, model, rep)
	if err != nil {
		fmt.Fprintf(stderr, "%s: no AI summary: %s\n", name, visible(err.Error()))
		return
	}
	fmt.Fprintf(stdout, "\n %s\n%s\n", explain.Label, wrap(text, 76))
}

// wrap breaks text into lines of at most width characters, each indented by
// a space like the rest of the report.
func wrap(text string, width int) string {
	var b strings.Builder
	used := 0
	for _, word := range strings.Fields(text) {
		n := utf8.RuneCountInString(word)
		switch {
		case used == 0:
			b.WriteByte(' ')
		case used+1+n > width:
			b.WriteString("\n ")
			used = 0
		default:
			b.WriteByte(' ')
			used++
		}
		b.WriteString(word)
		used += n
	}
	return b.String()
}

// check is the whole pipeline: load the files, price every customer both
// ways, compare, and judge the result against the rules.
func check(in load.Inputs, approvals []string) (report.Report, error) {
	s, err := load.Open(in)
	if err != nil {
		return report.Report{}, err
	}
	res, err := diff.Compare(s.Customers, s.Before, s.After, s.Segments)
	if err != nil {
		return report.Report{}, err
	}
	changes := diff.Configs(s.Before, s.After)
	return report.Report{
		Before:   in.Before,
		After:    in.After,
		Result:   res,
		Changes:  changes,
		Verdicts: rules.Check(s.Rules, res, rules.Context{Approvals: approvals, Changes: changes}),
	}, nil
}
