package load

import (
	"reflect"
	"testing"

	"github.com/dilaziz/blast/internal/model"
)

func TestCSVCustomers(t *testing.T) {
	// A spreadsheet export: byte-order mark, CRLF line endings, columns in
	// their own order, padded cells, and the usual spellings of yes and no.
	csv := "\xef\xbb\xbfBilling, Plan ,id,referred,coupons,quantity\r\n" +
		"annual,pro,c1,TRUE,WELCOME10,\r\n" +
		"monthly, starter ,c2,no,,12\r\n" +
		"annual,pro,c3,1,\"WELCOME10, VIP5\",1\r\n" +
		"annual,pro,c4,,WELCOME10; VIP5 | EARLY,\r\n"
	got, lines, err := parseCustomers("customers.csv", []byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Customer{
		{ID: "c1", Plan: "pro", Billing: model.Annual, Referred: true, Coupons: []string{"WELCOME10"}},
		{ID: "c2", Plan: "starter", Billing: model.Monthly, Quantity: 12},
		{ID: "c3", Plan: "pro", Billing: model.Annual, Referred: true, Coupons: []string{"WELCOME10", "VIP5"}, Quantity: 1},
		{ID: "c4", Plan: "pro", Billing: model.Annual, Coupons: []string{"WELCOME10", "VIP5", "EARLY"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if wantLines := []int{2, 3, 4, 5}; !reflect.DeepEqual(lines, wantLines) {
		t.Errorf("lines = %v, want %v", lines, wantLines)
	}
}

func TestCSVCustomerErrors(t *testing.T) {
	cases := []struct{ name, csv, want string }{
		{"empty file", "", "c.csv line 1: expected a header row: id,plan,billing,coupons,referred,quantity"},
		{"unknown column", "id,plan,billing,email\n", `c.csv line 1: unknown column "email" (expected id, plan, billing, coupons, referred, quantity)`},
		{"misspelt column", "id,plan,billing,refered\n", `c.csv line 1: unknown column "refered" (expected id, plan, billing, coupons, referred, quantity)`},
		{"missing column", "id,billing\n", `c.csv line 1: missing column "plan"`},
		{"repeated column", "id,plan,billing,ID\n", `c.csv line 1: column "id" appears twice`},
		{"too few cells", "id,plan,billing\nc1,pro,annual\nc2,pro\n", "c.csv line 3: wrong number of fields"},
		{"stray quote", "id,plan,billing\nc1,pro,annual\nc2,pr\"o,annual\n", "c.csv line 3: bare \" in non-quoted-field"},
		{"referred", "id,plan,billing,referred\nc1,pro,annual,maybe\n", `c.csv line 2: referred must be true or false, not "maybe"`},
		{"fractional quantity", "id,plan,billing,quantity\nc1,pro,annual,1.5\n", `c.csv line 2: quantity "1.5" is not a whole number`},
		{"zero quantity", "id,plan,billing,quantity\nc1,pro,annual,0\n",
			"c.csv line 2: quantity must be between 1 and 1,000,000,000 (leave it out for a single seat)"},
		{"billing", "id,plan,billing\nc1,pro,annual\n\nc2,pro,Yearly\n", `c.csv line 4: customer "c2": billing must be "monthly" or "annual", not "Yearly"`},
		{"no id", "id,plan,billing\n,pro,annual\n", `c.csv line 2: customer has no "id"`},
		{"duplicate id", "id,plan,billing\nc1,pro,annual\nc1,pro,monthly\n", `c.csv line 3: duplicate customer id "c1" (first on line 2)`},
		{"duplicate coupon", "id,plan,billing,coupons\nc1,pro,annual,A;A\n", `c.csv line 2: customer "c1" lists coupon "A" twice`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseCustomers("c.csv", []byte(tc.csv))
			if err == nil || err.Error() != tc.want {
				t.Errorf("got  %v\nwant %s", err, tc.want)
			}
		})
	}
}

func TestJSONQuantity(t *testing.T) {
	got, _, err := parseCustomers("c.json", []byte("\xef\xbb\xbf"+`[
  { "id": "c1", "plan": "pro", "billing": "annual", "quantity": 12 },
  { "id": "c2", "plan": "pro", "billing": "annual" },
  { "id": "c3", "plan": "pro", "billing": "annual", "quantity": null }
]`))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{12, 1, 1} {
		if got[i].Units() != want {
			t.Errorf("%s is billed for %d units, want %d", got[i].ID, got[i].Units(), want)
		}
	}

	bad := []struct{ name, quantity, want string }{
		{"zero", "0", "c.json line 2: quantity must be between 1 and 1,000,000,000 (leave it out for a single seat)"},
		{"negative", "-3", "c.json line 2: quantity must be between 1 and 1,000,000,000 (leave it out for a single seat)"},
		{"too many", "1000000001", "c.json line 2: quantity must be between 1 and 1,000,000,000 (leave it out for a single seat)"},
		{"text", `"twelve"`, `c.json line 2: field "quantity" cannot be a string`},
	}
	for _, tc := range bad {
		_, _, err := parseCustomers("c.json", []byte("[\n  { \"id\": \"c1\", \"billing\": \"annual\", \"quantity\": "+tc.quantity+" }\n]"))
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s:\n got  %v\n want %s", tc.name, err, tc.want)
		}
	}
}

// An invoice is capped so that percent arithmetic cannot overflow.
func TestInvoiceTooLargeToPrice(t *testing.T) {
	t.Chdir(t.TempDir())
	write(t, "old.yml", "plans:\n  pro: { monthly: 99, annual: 990 }\n")
	write(t, "new.yml", "plans:\n  pro: { monthly: 99, annual: 9900 }\n")
	write(t, "rules.yml", "rules: []\n")
	// Fine at today's $990 ($990 million), too large at the proposed $9,900.
	write(t, "customers.csv", "id,plan,billing,quantity\nc0,pro,annual,5\nc1,pro,annual,1000000\n")

	_, err := Open(Inputs{Before: "old.yml", After: "new.yml", Customers: "customers.csv", Rules: "rules.yml"})
	const want = `customers.csv line 3: $9,900.00 × 1,000,000 on plan "pro" in new.yml is over the $1,000,000,000.00 limit for one invoice`
	if err == nil || err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}
}
