package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dilaziz/blast/examples"
)

// exampleFiles are the files init writes, in the order they are worth reading.
var exampleFiles = []struct{ name, about string }{
	{"pricing.old.yml", "pricing as it is today"},
	{"pricing.new.yml", "the proposed change: 20% off for referred annual customers"},
	{"pricing.fixed.yml", "the same change, narrowed so it breaks no rule"},
	{"customers.json", "ten sample customers"},
	{"rules.yml", "three rules the change must not break"},
}

func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(name+" init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "overwrite files that already exist")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if fs.NArg() > 1 {
		return fail(stderr, errors.New("init takes at most one directory"))
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	if err := writeExample(dir, *force); err != nil {
		return fail(stderr, err)
	}

	fmt.Fprintf(stdout, "Wrote an example pricing change to %s:\n\n", dir)
	for _, f := range exampleFiles {
		fmt.Fprintf(stdout, "  %-18s %s\n", f.name, f.about)
	}
	fmt.Fprint(stdout, "\nNext:\n\n")
	if dir != "." {
		fmt.Fprintf(stdout, "  cd %s\n", dir)
	}
	fmt.Fprintf(stdout, "  %-40s see who the change hits\n", name+" check")
	fmt.Fprintf(stdout, "  %-40s see the fix pass\n", name+" check --after pricing.fixed.yml")
	return exitOK
}

// writeExample copies the embedded example into dir. Unless force is set it
// writes nothing at all if any of the files is already there.
func writeExample(dir string, force bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- an ordinary project directory
		return err
	}
	if !force {
		for _, f := range exampleFiles {
			path := filepath.Join(dir, f.name)
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists (use --force to overwrite)", path)
			}
		}
	}
	for _, f := range exampleFiles {
		data, err := examples.Acme.ReadFile("acme/" + f.name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), data, 0o644); err != nil { // #nosec G306 -- example files, meant to be read and committed
			return err
		}
	}
	return nil
}
