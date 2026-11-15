package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/dilaziz/blast/internal/stripe"
)

// fakeStripe points the import at a local stand-in for one small account:
// two customers on Pro, one of them with three seats and a coupon.
func fakeStripe(t *testing.T) {
	t.Helper()
	const pro = `{"product":"prod_1","currency":"usd","unit_amount":%d,"billing_scheme":"per_unit","recurring":{"interval":%q,"interval_count":1,"usage_type":"licensed"}}`
	sub := func(id, customer string, quantity int, price string, discounts string) string {
		return fmt.Sprintf(`{"id":%q,"status":"active","customer":%q,"discounts":[%s],"items":{"data":[{"quantity":%d,"price":%s}]}}`,
			id, customer, discounts, quantity, price)
	}
	answers := map[string]string{
		"/v1/subscriptions": sub("sub_1", "cus_1", 3, fmt.Sprintf(pro, 9900, "month"), `{"id":"di_1","coupon":{"id":"WELCOME10"}}`) + "," +
			sub("sub_2", "cus_2", 1, fmt.Sprintf(pro, 99000, "year"), ""),
		"/v1/coupons":  `{"id":"WELCOME10","percent_off":10,"created":1}`,
		"/v1/products": `{"id":"prod_1","name":"Pro"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("%s %s: only GET may ever be sent", r.Method, r.URL.Path)
		}
		fmt.Fprintf(w, `{"has_more":false,"data":[%s]}`, answers[r.URL.Path])
	}))
	stripeURL = srv.URL
	t.Cleanup(func() { srv.Close(); stripeURL = stripe.APIURL })
	t.Setenv("STRIPE_API_KEY", "rk_test_madeUpForTests")
}

func TestImportStripeThenCheck(t *testing.T) {
	fakeStripe(t)
	t.Chdir(t.TempDir())

	stdout, stderr, code := blast(t, "import", "stripe")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	wantLines(t, stdout,
		"Imported from Stripe (test mode) into .",
		"customers.json    2 customers",
		"pricing.old.yml   1 plan, 1 coupon",
		"pricing.new.yml   a copy of today's pricing, for you to edit",
		`Next: edit pricing.new.yml, then run "blast check".`,
	)
	wantFile(t, "customers.json", `[
  {"id":"cus_1","plan":"pro","billing":"monthly","coupons":["WELCOME10"],"referred":false,"quantity":3},
  {"id":"cus_2","plan":"pro","billing":"annual","coupons":[],"referred":false,"quantity":1}
]
`)
	wantFile(t, "pricing.old.yml", `# Today's pricing, as imported from Stripe. Propose changes in pricing.new.yml, not here.
plans:
  pro: {monthly: 99, annual: 990}
discounts:
  WELCOME10: {percent: 10}
`)
	if info, err := os.Stat("customers.json"); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("customers.json: %v, mode %v; want it readable by its owner alone", err, info.Mode().Perm())
	}

	// Straight after an import nothing has changed and there are no rules.
	stdout, _, code = blast(t, "check")
	if code != exitOK {
		t.Fatalf("check after import: exit code %d\n%s", code, stdout)
	}
	wantLines(t, stdout, "2 customers simulated", "0 of 2 customers (0%)", "no rules defined")

	// Propose a price rise: three seats at $109 less 10%, up from $99.
	proposed, err := os.ReadFile("pricing.new.yml")
	if err != nil {
		t.Fatal(err)
	}
	proposed = []byte(strings.Replace(string(proposed), "monthly: 99", "monthly: 109", 1))
	if err := os.WriteFile("pricing.new.yml", proposed, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, _ = blast(t, "check")
	wantLines(t, stdout, "cus_1      pro monthly ×3   $267.30   $294.30", "1 of 2 customers (50%)", "$1,257.30 -> $1,284.30")

	// Importing again never destroys work: not without --force, and with it
	// the proposal and the rules are still left alone.
	_, stderr, code = blast(t, "import", "stripe")
	if code != exitBadInput || !strings.Contains(stderr, "customers.json already exists (use --force to overwrite)") {
		t.Errorf("second import: exit code %d, stderr %q", code, stderr)
	}
	stdout, _, code = blast(t, "import", "stripe", "--force")
	if code != exitOK {
		t.Errorf("import --force: exit code %d", code)
	}
	wantLines(t, stdout, "pricing.new.yml   kept as it was", "rules.yml         kept as it was")
	wantFile(t, "pricing.new.yml", string(proposed))
}

func TestImportRefusesKeysItShouldNotHave(t *testing.T) {
	fakeStripe(t)
	t.Chdir(t.TempDir())
	cases := []struct{ key, want string }{
		{"", "blast: STRIPE_API_KEY is not set"},
		{"sk_live_notARealKeyButShapedLikeOne", "blast: STRIPE_API_KEY is a live secret key, which can move money"},
		{"rk_live_notARealKeyButShapedLikeOne", "blast: STRIPE_API_KEY is a live-mode key; add --live"},
	}
	for _, tc := range cases {
		t.Setenv("STRIPE_API_KEY", tc.key)
		stdout, stderr, code := blast(t, "import", "stripe")
		if code != exitBadInput || stdout != "" || !strings.Contains(stderr, tc.want) {
			t.Errorf("key %q: exit code %d, stdout %q, stderr %q", tc.key, code, stdout, stderr)
		}
		if tc.key != "" && strings.Contains(stderr, tc.key) {
			t.Errorf("the key was printed: %q", stderr)
		}
		if entries, _ := os.ReadDir("."); len(entries) != 0 {
			t.Errorf("key %q: files were written", tc.key)
		}
	}

	_, stderr, code := blast(t, "import")
	if code != exitBadInput || !strings.Contains(stderr, `import needs a source: "blast import stripe"`) {
		t.Errorf("import with no source: exit code %d, stderr %q", code, stderr)
	}
}

func wantFile(t *testing.T, name, want string) {
	t.Helper()
	got, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
	}
}
