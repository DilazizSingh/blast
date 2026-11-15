package main

import (
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dilaziz/blast/internal/load"
)

// projectFile is where a repository can record its file locations.
const projectFile = name + ".yml"

// selection is what a command was asked to compare.
type selection struct {
	load.Inputs
	base    string // git revision to read today's pricing from
	approve string
}

// selectInputs registers the flags shared by check and dash. Their defaults
// come from the project file when there is one, and otherwise are the names
// `init` writes.
func selectInputs(fs *flag.FlagSet) (*selection, error) {
	project, err := load.ReadProject(projectFile)
	if err != nil {
		return nil, err
	}
	before := project.Before
	if before == "" && project.Base == "" {
		before = "pricing.old.yml"
	}
	sel := &selection{}
	fs.StringVar(&sel.Before, "before", before, "pricing as it is today")
	fs.StringVar(&sel.base, "base", project.Base, "git revision to take today's pricing from, instead of --before")
	fs.StringVar(&sel.After, "after", cmp.Or(project.After, "pricing.new.yml"), "pricing as proposed")
	fs.StringVar(&sel.Customers, "customers", cmp.Or(project.Customers, "customers.json"), "the customers to price (.json or .csv)")
	fs.StringVar(&sel.Rules, "rules", cmp.Or(project.Rules, "rules.yml"), "the rules the change must not break")
	fs.StringVar(&sel.approve, "approve", "", `comma-separated sign-offs, read by approval("...") in rules`)
	return sel, nil
}

// resolve settles --before against --base once the flags are parsed. With a
// base revision, "before" is the --after file as that revision has it.
func (s *selection) resolve(fs *flag.FlagSet) error {
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	switch {
	case given["before"] && given["base"]:
		return errors.New("use --before or --base, not both")
	case given["before"]:
		s.base = "" // a file named on the command line beats the project's base revision
	}
	if s.base == "" {
		return nil
	}
	base, after := s.base, s.After
	s.Before = base + ":" + after
	s.Read = func(path string) ([]byte, error) {
		if path == s.Before {
			return gitShow(base, after)
		}
		return os.ReadFile(path) // #nosec G304 -- reading the files the user names is the tool's job
	}
	return nil
}

// files are the inputs that live on disk, for the dashboard to watch.
func (s *selection) files() []string {
	files := []string{s.After, s.Customers, s.Rules}
	if s.base == "" {
		files = append(files, s.Before)
	}
	return files
}

func (s *selection) approvals() []string {
	return strings.FieldsFunc(s.approve, func(r rune) bool { return r == ',' || r == ' ' })
}

// gitShow returns a file as a git revision has it.
func gitShow(rev, path string) ([]byte, error) {
	// A revision that looks like an option would be read as one by git.
	if rev == "" || strings.HasPrefix(rev, "-") {
		return nil, fmt.Errorf("%q is not a git revision", rev)
	}
	// #nosec G204 -- no shell is involved, and rev cannot be read as an option (checked above)
	cmd := exec.Command("git", "show", "--end-of-options", rev+":./"+filepath.ToSlash(path))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		reason, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return nil, fmt.Errorf("cannot read %s at revision %s: %s", path, rev, cmp.Or(reason, err.Error()))
	}
	return out, nil
}
