package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/dilaziz/blast/internal/report"
	"github.com/dilaziz/blast/internal/tui"
)

func runDash(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(name+" dash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sel, err := selectInputs(fs)
	if err != nil {
		return fail(stderr, err)
	}
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return fail(stderr, fmt.Errorf("dash takes flags only, not %q", fs.Arg(0)))
	}
	if err := sel.resolve(fs); err != nil {
		return fail(stderr, err)
	}
	if out, ok := stdout.(*os.File); !ok || !term.IsTerminal(out.Fd()) {
		return fail(stderr, fmt.Errorf("dash needs a terminal; in scripts and CI use %q", name+" check"))
	}

	rerun := func() (report.Report, error) { return check(sel.Inputs, sel.approvals()) }
	first, err := rerun()
	if err != nil {
		return fail(stderr, err)
	}
	if err := tui.Run(tui.Options{Name: name, First: first, Rerun: rerun, Watch: sel.files()}); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}
