package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dilaziz/blast/internal/load"
	"github.com/dilaziz/blast/internal/report"
	"github.com/dilaziz/blast/internal/stripe"
)

// stripeURL is where the import reads from. Tests point it at a local
// server; there is deliberately no flag or environment variable for it, so
// nothing can talk blast into sending a key somewhere else.
var stripeURL = stripe.APIURL

const starterRules = `# Rules a pricing change must not break. There are none yet, so "` + name + ` check"
# reports the numbers and exits 0. Two to start from:
#
#   - name: max-one-discount
#     never: customer.discount_count > 1
#
#   - name: no-increase-over-ten-dollars
#     never: invoice.change > 10
rules: []
`

func runImport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "stripe" {
		return fail(stderr, fmt.Errorf("import needs a source: %q", name+" import stripe"))
	}
	flags := flag.NewFlagSet(name+" import stripe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("out", ".", "directory to write the files into")
	currency := flags.String("currency", "usd", "import the subscriptions billed in this currency")
	live := flags.Bool("live", false, "allow a live-mode restricted key, which reads real customer data")
	force := flags.Bool("force", false, "overwrite customers.json and pricing.old.yml")
	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage of %s:\n", flags.Name())
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nThe key is read from STRIPE_API_KEY and must be restricted and read-only.")
	}
	if code, ok := parse(flags, args[1:]); !ok {
		return code
	}
	if flags.NArg() > 0 {
		return fail(stderr, fmt.Errorf("import stripe takes flags only, not %q", flags.Arg(0)))
	}

	client, err := stripe.NewClient(os.Getenv("STRIPE_API_KEY"), stripeURL, name+"/"+version, *live)
	if err != nil {
		return fail(stderr, err)
	}
	res, err := stripe.Import(client, *currency)
	if err == nil {
		err = nothingImported(res)
	}
	if err != nil {
		return fail(stderr, err)
	}
	kept, err := writeImport(*dir, res, *force)
	if err != nil {
		return fail(stderr, err)
	}
	summarise(terminal(stdout, false), res, client.Live(), *dir, kept)
	return exitOK
}

func nothingImported(res stripe.Result) error {
	switch {
	case len(res.Customers) > 0:
		return nil
	case res.Found == 0:
		return errors.New("the account has no subscriptions; test mode and live mode hold separate data")
	}
	return fmt.Errorf("none of the %s can be modelled; the most common reason: %s",
		report.Plural(res.Found, "subscription"), res.Skipped[0].Reason)
}

// writeImport writes the four files `check` reads. The two that mirror
// Stripe are replaced only with force. The two the user edits, the proposed
// pricing and the rules, are never replaced; kept names those left alone.
func writeImport(dir string, res stripe.Result, force bool) (kept map[string]bool, err error) {
	pricing, err := load.FormatConfig(res.Config,
		"Today's pricing, as imported from Stripe. Propose changes in pricing.new.yml, not here.")
	if err != nil {
		return nil, err
	}
	proposed, err := load.FormatConfig(res.Config, "The proposed pricing. Edit this file, then run \""+name+" check\".")
	if err != nil {
		return nil, err
	}
	files := []struct {
		name    string
		data    []byte
		mode    fs.FileMode
		mirrors bool // a copy of what Stripe holds, safe to regenerate
	}{
		{"customers.json", customersJSON(res), 0o600, true}, // who pays what is nobody else's business
		{"pricing.old.yml", pricing, 0o644, true},
		{"pricing.new.yml", proposed, 0o644, false},
		{"rules.yml", []byte(starterRules), 0o644, false},
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	kept = map[string]bool{}
	for _, f := range files {
		_, err := os.Stat(filepath.Join(dir, f.name))
		switch exists := err == nil; {
		case exists && !f.mirrors:
			kept[f.name] = true
		case exists && !force:
			return nil, fmt.Errorf("%s already exists (use --force to overwrite)", filepath.Join(dir, f.name))
		}
	}
	for _, f := range files {
		if kept[f.name] {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, f.mode); err != nil {
			return nil, err
		}
	}
	return kept, nil
}

// customersJSON writes one customer per line, as the example does, so a
// change to one customer is a one-line diff.
func customersJSON(res stripe.Result) []byte {
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, c := range res.Customers {
		line, _ := json.Marshal(c) // a struct of strings and numbers always encodes
		b.WriteString("  ")
		b.Write(line)
		if i < len(res.Customers)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("]\n")
	return b.Bytes()
}

func summarise(w io.Writer, res stripe.Result, live bool, dir string, kept map[string]bool) {
	mode := "test mode"
	if live {
		mode = "live mode"
	}
	note := func(file, fresh string) string {
		if kept[file] {
			return "kept as it was"
		}
		return fresh
	}
	fmt.Fprintf(w, " Imported from Stripe (%s) into %s\n", mode, dir)
	fmt.Fprintf(w, "   customers.json    %s\n", report.Plural(len(res.Customers), "customer"))
	fmt.Fprintf(w, "   pricing.old.yml   %s, %s\n",
		report.Plural(len(res.Config.Plans), "plan"), report.Plural(len(res.Config.Discounts), "coupon"))
	fmt.Fprintf(w, "   pricing.new.yml   %s\n", note("pricing.new.yml", "a copy of today's pricing, for you to edit"))
	fmt.Fprintf(w, "   rules.yml         %s\n", note("rules.yml", "no rules yet, with two examples to start from"))

	if len(res.Skipped) > 0 {
		left := 0
		for _, s := range res.Skipped {
			left += s.Count
		}
		fmt.Fprintf(w, "\n Left out %s of %d that %s cannot model:\n", report.Plural(left, "subscription"), res.Found, name)
		for _, s := range res.Skipped {
			fmt.Fprintf(w, "   %5d  %s\n", s.Count, s.Reason)
		}
	}
	fmt.Fprintf(w, "\n Next: edit pricing.new.yml, then run %q.\n", name+" check")
}
