package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/charmbracelet/colorprofile"

	"github.com/dilaziz/blast/internal/report"
)

// name is the command's name wherever it is printed. Renaming the tool starts here.
const name = "blast"

// Exit codes are part of the interface: git hooks and CI branch on them. A
// Go panic also exits with 2, so a crash reads as "bad input or crash" too.
const (
	exitOK       = 0 // every rule passes
	exitBroken   = 1 // a rule broke
	exitBadInput = 2 // bad input or crash
)

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stdout)
		return exitOK
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "init":
		return runInit(rest, stdout, stderr)
	case "check":
		return runCheck(rest, stdout, stderr)
	case "dash":
		return runDash(rest, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "%s %s\n", name, version)
		return exitOK
	case "help", "--help", "-h":
		usage(stdout)
		return exitOK
	default:
		return fail(stderr, fmt.Errorf("unknown command %q (run %q for the list)", cmd, name+" help"))
	}
}

func usage(w io.Writer) {
	out := terminal(w, false)
	fmt.Fprintf(out, `
%s
 %s

 Usage:
   %[3]s init  [--force] [dir]   write an example: pricing, customers and rules
   %[3]s check [flags]           simulate a pricing change and check your rules
   %[3]s dash  [flags]           the same, full screen, rerun live as you edit
   %[3]s version

 Run "%[3]s check --help" for the flags; dash takes the same file flags.

 Exit codes: 0 every rule passes, 1 a rule broke, 2 bad input.
`, report.Banner(), report.Pitch, name)
}

// terminal wraps w so colour reaches a terminal and nothing else: codes are
// stripped when w is a pipe or a file, and NO_COLOR is honoured.
func terminal(w io.Writer, noColor bool) io.Writer {
	out := colorprofile.NewWriter(w, os.Environ())
	if noColor {
		out.Profile = colorprofile.NoTTY
	}
	return out
}

// parse runs fs over args. When ok is false the command is over and code is
// its exit status: asking for --help is not an error, a bad flag is.
func parse(fs *flag.FlagSet, args []string) (code int, ok bool) {
	switch err := fs.Parse(args); {
	case err == nil:
		return exitOK, true
	case errors.Is(err, flag.ErrHelp):
		return exitOK, false
	default:
		return exitBadInput, false
	}
}

// fail reports an error and returns the bad-input exit code. Errors quote
// from input files, so anything that is not visible text is replaced first:
// a crafted file must not be able to repaint the terminal through a message.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "%s: %s\n", name, visible(err.Error()))
	return exitBadInput
}

func visible(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || unicode.IsPrint(r) {
			return r
		}
		return '?'
	}, s)
}
