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
	var in load.Inputs
	fs.StringVar(&in.Before, "before", "pricing.old.yml", "pricing as it is today")
	fs.StringVar(&in.After, "after", "pricing.new.yml", "pricing as proposed")
	fs.StringVar(&in.Customers, "customers", "customers.json", "the customers to price")
	fs.StringVar(&in.Rules, "rules", "rules.yml", "the rules the change must not break")
	approve := fs.String("approve", "", `comma-separated sign-offs, read by approval("...") in rules`)
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

	rep, err := check(in, strings.FieldsFunc(*approve, func(r rune) bool { return r == ',' || r == ' ' }))
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
