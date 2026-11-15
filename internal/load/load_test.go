package load

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dilaziz/blast/internal/model"
)

const acme = "../../examples/acme/"

var acmeInputs = Inputs{
	Before:    acme + "pricing.old.yml",
	After:     acme + "pricing.new.yml",
	Customers: acme + "customers.json",
}

func TestOpenExample(t *testing.T) {
	s, err := Open(acmeInputs)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Before.Plans["pro"].Prices[model.Annual]; got != 99000 {
		t.Errorf("pro annual = %d cents, want 99000 (dollars must become cents)", got)
	}
	if !s.Before.Plans["legacy-pro"].Legacy || s.Before.Plans["pro"].Legacy {
		t.Error("only legacy-pro should be legacy")
	}
	if len(s.Before.Discounts) != 1 || len(s.After.Discounts) != 2 {
		t.Fatalf("discounts: before %d, after %d; want 1 and 2", len(s.Before.Discounts), len(s.After.Discounts))
	}
	want := model.Discount{Name: "REFERRAL20", BasisPoints: 2000, When: `billing == "annual" and referred`}
	if got := s.After.Discounts[1]; got != want {
		t.Errorf("second discount = %+v, want %+v", got, want)
	}
	if len(s.Customers) != 10 {
		t.Fatalf("loaded %d customers, want 10", len(s.Customers))
	}
	c4 := s.Customers[3]
	if c4.ID != "c4" || c4.Billing != model.Annual || !c4.Referred || !c4.HasCoupon("WELCOME10") {
		t.Errorf("c4 loaded as %+v", c4)
	}
}

func TestConfigErrors(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"empty", "", "p.yml: file is empty"},
		{"syntax", "plans:\n  pro: { monthly: 99\n", "p.yml line 3: did not find expected ',' or '}'"},
		{"not a mapping", "- a\n- b\n", "p.yml line 1: a pricing file must be a mapping of names to settings"},
		{"unknown section", "plans:\n  pro: { monthly: 99 }\ndiscount:\n  X: { percent: 5 }\n",
			`p.yml line 3: unknown section "discount" (expected plans, discounts or payment)`},
		{"no plans", "discounts:\n  X: { percent: 5 }\n", "p.yml: no plans defined"},
		{"duplicate plan", "plans:\n  pro: { monthly: 99 }\n  pro: { monthly: 89 }\n", `p.yml line 3: "pro" is defined twice`},
		{"unknown plan field", "plans:\n  pro:\n    monthly: 99\n    yearly: 990\n",
			`p.yml line 4: unknown plan field "yearly" (expected monthly, annual or legacy)`},
		{"no price", "plans:\n  pro: { legacy: true }\n", `p.yml line 2: plan "pro" has no monthly or annual price`},
		{"bad price", "plans:\n  pro:\n    monthly: cheap\n", `p.yml line 3: "cheap" is not a number`},
		{"sub-cent price", "plans:\n  pro:\n    monthly: 9.999\n", `p.yml line 3: "9.999" has more than two decimal places`},
		{"negative price", "plans:\n  pro:\n    monthly: -5\n", "p.yml line 3: an amount cannot be negative"},
		{"bad legacy", "plans:\n  pro:\n    monthly: 5\n    legacy: maybe\n", "p.yml line 4: expected true or false"},
		{"percent over 100", "plans:\n  pro: { monthly: 5 }\ndiscounts:\n  X:\n    percent: 120\n",
			"p.yml line 5: percent must be above 0 and at most 100"},
		{"percent and amount", "plans:\n  pro: { monthly: 5 }\ndiscounts:\n  X: { percent: 10, amount: 5 }\n",
			`p.yml line 4: discount "X" needs exactly one of percent or amount`},
		{"neither", "plans:\n  pro: { monthly: 5 }\ndiscounts:\n  X: { when: referred }\n",
			`p.yml line 4: discount "X" needs exactly one of percent or amount`},
		{"unknown discount field", "plans:\n  pro: { monthly: 5 }\ndiscounts:\n  X:\n    percent: 10\n    if: referred\n",
			`p.yml line 6: unknown discount field "if" (expected percent, amount or when)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig("p.yml", []byte(tc.yaml))
			if err == nil || err.Error() != tc.want {
				t.Errorf("got  %v\nwant %s", err, tc.want)
			}
		})
	}
}

func TestConfigExtras(t *testing.T) {
	cfg, err := parseConfig("p.yml", []byte(`
plans:
  free: { monthly: 0 }
  pro:  { monthly: 99.50 }
discounts:
  HALF:   { percent: 12.5 }
  TENOFF: { amount: 10 }
payment:
  capture: automatic
  retry: { days: [1, 3] }
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := cfg.Plans["free"].Prices[model.Monthly]; !ok || got != 0 {
		t.Errorf("free monthly = %d, %v; want a real price of 0", got, ok)
	}
	if _, ok := cfg.Plans["free"].Prices[model.Annual]; ok {
		t.Error("free has an annual price it never declared")
	}
	if got := cfg.Plans["pro"].Prices[model.Monthly]; got != 9950 {
		t.Errorf("pro monthly = %d cents, want 9950", got)
	}
	if got := cfg.Discounts[0].BasisPoints; got != 1250 {
		t.Errorf("12.5%% = %d basis points, want 1250", got)
	}
	if got := cfg.Discounts[1].AmountCents; got != 1000 {
		t.Errorf("amount 10 = %d cents, want 1000", got)
	}
	wantPayment := map[string]string{"payment.capture": "automatic", "payment.retry.days.0": "1", "payment.retry.days.1": "3"}
	for path, want := range wantPayment {
		if cfg.Payment[path] != want {
			t.Errorf("payment[%q] = %q, want %q", path, cfg.Payment[path], want)
		}
	}
}

func TestCustomerErrors(t *testing.T) {
	const ok = `{ "id": "c1", "plan": "pro", "billing": "annual", "coupons": [], "referred": false }`
	cases := []struct{ name, json, want string }{
		{"not an array", `{"id": "c1"}`, "c.json line 1: expected a JSON array of customers"},
		{"empty file", ``, "c.json line 1: expected a JSON array of customers"},
		{"syntax", "[\n  " + ok + ",\n  { \"id\": \"c2\" \"plan\": \"pro\" }\n]",
			"c.json line 3: invalid JSON: invalid character '\"' after object key:value pair"},
		{"truncated", "[\n  " + ok + ",\n", "c.json line 3: invalid JSON: unexpected end of JSON input"},
		{"unknown field", "[\n  " + ok + ",\n  { \"id\": \"c2\", \"refered\": true }\n]", `c.json line 3: unknown field "refered"`},
		{"wrong type", "[\n  { \"id\": \"c1\", \"referred\": \"yes\" }\n]", `c.json line 2: field "referred" cannot be a string`},
		{"not an object", "[\n  42\n]", "c.json line 2: each customer must be a JSON object, not a number"},
		{"no id", "[\n  { \"plan\": \"pro\", \"billing\": \"annual\" }\n]", `c.json line 2: customer has no "id"`},
		{"bad billing", "[\n  " + ok + ",\n\n  { \"id\": \"c2\", \"plan\": \"pro\", \"billing\": \"yearly\" }\n]",
			`c.json line 4: customer "c2": billing must be "monthly" or "annual", not "yearly"`},
		{"duplicate id", "[\n  " + ok + ",\n  " + ok + "\n]", `c.json line 3: duplicate customer id "c1" (first on line 2)`},
		{"duplicate coupon", "[\n  { \"id\": \"c1\", \"billing\": \"annual\", \"coupons\": [\"A\", \"A\"] }\n]",
			`c.json line 2: customer "c1" lists coupon "A" twice`},
		{"trailing content", "[\n  " + ok + "\n]\n[]", "c.json line 4: unexpected content after the customer list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseCustomers("c.json", []byte(tc.json))
			if err == nil || err.Error() != tc.want {
				t.Errorf("got  %v\nwant %s", err, tc.want)
			}
		})
	}
}

func TestCrossCheck(t *testing.T) {
	const pricing = "plans:\n  pro: { monthly: 99, annual: 990 }\n  solo: { monthly: 9 }\ndiscounts:\n  OLD10: { percent: 10 }\n"
	cases := []struct{ name, after, customer, want string }{
		{"unknown plan", pricing, `{ "id": "c1", "plan": "pro-plus", "billing": "annual" }`,
			`customers.json line 3: unknown plan "pro-plus" (not defined in pricing.old.yml)`},
		{"plan removed by the change", "plans:\n  solo: { monthly: 9 }\n", `{ "id": "c1", "plan": "pro", "billing": "annual" }`,
			`customers.json line 3: unknown plan "pro" (not defined in pricing.new.yml)`},
		{"period not sold", pricing, `{ "id": "c1", "plan": "solo", "billing": "annual" }`,
			`customers.json line 3: plan "solo" has no annual price in pricing.old.yml`},
		{"unknown coupon", pricing, `{ "id": "c1", "plan": "pro", "billing": "annual", "coupons": ["NOPE"] }`,
			`customers.json line 3: unknown coupon "NOPE" (not defined in either pricing file)`},
		{"coupon retired by the change", "plans:\n  pro: { monthly: 99, annual: 990 }\n  solo: { monthly: 9 }\n",
			`{ "id": "c1", "plan": "pro", "billing": "annual", "coupons": ["OLD10"] }`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			write(t, "pricing.old.yml", pricing)
			write(t, "pricing.new.yml", tc.after)
			write(t, "customers.json", "[\n  { \"id\": \"c0\", \"plan\": \"solo\", \"billing\": \"monthly\" },\n  "+tc.customer+"\n]\n")

			_, err := Open(Inputs{Before: "pricing.old.yml", After: "pricing.new.yml", Customers: "customers.json"})
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.want != "" && (err == nil || err.Error() != tc.want):
				t.Errorf("got  %v\nwant %s", err, tc.want)
			}
		})
	}
}

func TestMissingFileNamesTheFile(t *testing.T) {
	in := acmeInputs
	in.After = filepath.Join(t.TempDir(), "nope.yml")
	if _, err := Open(in); err == nil || !os.IsNotExist(err) {
		t.Errorf("Open with a missing file: %v", err)
	}
}

func write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
