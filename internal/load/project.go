package load

import (
	"errors"
	"io/fs"
	"os"
)

// Project is the optional project file: where a repository keeps its
// pricing, customers and rules, so that everyday commands need no flags.
type Project struct {
	Before    string // today's pricing as a file...
	Base      string // ...or as a git revision of After
	After     string
	Customers string
	Rules     string
}

// ReadProject reads a project file. There being no such file is not an
// error; it yields the zero Project.
func ReadProject(path string) (Project, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Project{}, nil
	}
	if err != nil {
		return Project{}, err
	}
	root, err := parseYAML(path, data)
	if err != nil {
		return Project{}, err
	}
	fields, err := entries(path, root, "a project file")
	if err != nil {
		return Project{}, err
	}

	var p Project
	settings := map[string]*string{
		"before": &p.Before, "base": &p.Base, "after": &p.After, "customers": &p.Customers, "rules": &p.Rules,
	}
	for _, f := range fields {
		setting, ok := settings[f.key.Value]
		if !ok {
			return Project{}, errAt(path, f.key, "unknown setting %q (expected before, base, after, customers or rules)", f.key.Value)
		}
		if *setting, err = text(path, f.val, "a value"); err != nil {
			return Project{}, err
		}
	}
	if p.Before != "" && p.Base != "" {
		return Project{}, &Error{File: path, Msg: `set "before" or "base", not both`}
	}
	return p, nil
}
