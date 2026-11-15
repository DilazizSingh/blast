package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/ with the current output")

// The reports for the plan's example are pinned byte for byte, so no change
// to a renderer goes unnoticed. After an intended change:
//
//	go test ./cmd/blast -run TestGolden -update
func TestGolden(t *testing.T) {
	golden, err := filepath.Abs("../../testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir("../../examples/acme")

	runs := []struct {
		name string
		args []string
		code int
	}{
		{"acme.new", []string{"check"}, exitBroken},
		{"acme.fixed", []string{"check", "--after", "pricing.fixed.yml"}, exitOK},
	}
	for _, run := range runs {
		for _, format := range []string{"text", "md", "json"} {
			file := filepath.Join(golden, run.name+"."+format)
			t.Run(filepath.Base(file), func(t *testing.T) {
				stdout, stderr, code := blast(t, append(run.args, "--format", format)...)
				if code != run.code || stderr != "" {
					t.Fatalf("exit code %d, stderr %q; want %d and silence", code, stderr, run.code)
				}
				if *update {
					if err := os.WriteFile(file, []byte(stdout), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal([]byte(stdout), want) {
					t.Errorf("output differs from %s; if the change is intended, rerun with -update.\n--- got ---\n%s", file, stdout)
				}
			})
		}
	}
}
