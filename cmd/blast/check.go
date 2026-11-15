package main

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/dilaziz/blast/internal/diff"
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
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return fail(stderr, fmt.Errorf("check takes flags only, not %q", fs.Arg(0)))
	}
	render, ok := formats[*format]
	if !ok {
		return fail(stderr, fmt.Errorf("unknown format %q (available: %s)", *format, available))
	}

	if err := sel.resolve(fs); err != nil {
		return fail(stderr, err)
	}
	rep, err := check(sel.Inputs, sel.approvals())
	if err != nil {
		return fail(stderr, err)
	}
	if err := render(terminal(stdout, *noColor), rep); err != nil {
		return fail(stderr, err)
	}
	return rep.ExitCode()
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
