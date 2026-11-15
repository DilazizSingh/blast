package stripe

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/sim"
)

// Everything here runs against a local stand-in for Stripe. No test, and no
// part of the build, uses a real key.

const testKey = "rk_test_madeUpForTests"

func listJSON(more bool, objects ...string) string {
	return fmt.Sprintf(`{"object":"list","has_more":%t,"data":[%s]}`, more, strings.Join(objects, ","))
}

func priceJSON(product, currency string, cents int, interval string, every int) string {
	return fmt.Sprintf(`{"id":"price_x","product":%q,"currency":%q,"unit_amount":%d,"billing_scheme":"per_unit",`+
		`"recurring":{"interval":%q,"interval_count":%d,"usage_type":"licensed"}}`, product, currency, cents, interval, every)
}

// subJSON is a subscription with one item. customer is JSON: an id in
// quotes, or an expanded object.
func subJSON(id, customer, status string, quantity int, price string, discounts ...string) string {
	return fmt.Sprintf(`{"id":%q,"status":%q,"customer":%s,"discounts":[%s],`+
		`"items":{"has_more":false,"data":[{"quantity":%d,"price":%s,"discounts":[]}]}}`,
		id, status, customer, strings.Join(discounts, ","), quantity, price)
}

var (
	starterMonthly = priceJSON("prod_1", "usd", 2900, "month", 1)
	starterAnnual  = priceJSON("prod_1", "usd", 29000, "year", 1)
	proMonthly     = priceJSON("prod_2", "usd", 9900, "month", 1)
	proMonthlyOld  = priceJSON("prod_2", "usd", 7900, "month", 1)
	proAnnual      = priceJSON("prod_2", "usd", 99000, "year", 1)

	// The two places API versions put a discount's coupon.
	welcome10 = `{"id":"di_1","coupon":{"id":"WELCOME10","percent_off":10,"created":100}}`
	half      = `{"id":"di_2","source":{"coupon":"HALF"}}`
)

var acme = map[string][]string{
	"/v1/subscriptions": {
		listJSON(true,
			subJSON("sub_01", `"cus_a"`, "active", 5, proMonthly, welcome10),
			subJSON("sub_02", `"cus_b"`, "trialing", 1, proAnnual),
			subJSON("sub_03", `"cus_c"`, "active", 1, starterMonthly, half),
		),
		listJSON(false,
			subJSON("sub_04", `"cus_c"`, "active", 2, starterAnnual),
			subJSON("sub_05", `"cus_d"`, "past_due", 1, proMonthlyOld),
			subJSON("sub_06", `"cus_e"`, "incomplete", 1, proMonthly),
			`{"id":"sub_07","status":"active","customer":"cus_f","items":{"data":[{"quantity":1,"price":`+proMonthly+`},{"quantity":1,"price":`+starterMonthly+`}]}}`,
			subJSON("sub_08", `"cus_g"`, "active", 1, priceJSON("prod_2", "usd", 27000, "month", 3)),
			subJSON("sub_09", `"cus_h"`, "active", 1, priceJSON("prod_2", "eur", 8900, "month", 1)),
			`{"id":"sub_10","status":"active","customer":"cus_i","items":{"data":[{"quantity":null,"price":`+
				`{"product":"prod_2","currency":"usd","unit_amount":null,"billing_scheme":"tiered","recurring":{"interval":"month","interval_count":1,"usage_type":"metered"}}}]}}`,
			subJSON("sub_11", `"cus_j"`, "active", 1, starterMonthly, `{"id":"di_3","coupon":{"id":"TENOFF"}}`),
			// The customer is expanded, with a name and an email that must not
			// be kept, and a discount of their own.
			subJSON("sub_12", `{"id":"cus_k","name":"Ada Lovelace","email":"ada@example.com",`+
				`"discount":{"id":"di_5","coupon":{"id":"VIP"}}}`, "active", 1, starterMonthly),
			subJSON("sub_13", `"cus_l"`, "active", 1, starterMonthly, `"di_9"`),
			subJSON("sub_14", `"cus_m"`, "active", 1, starterMonthly, `{"id":"di_6","coupon":{"id":"FINE"}}`),
			subJSON("sub_15", `"cus_n"`, "active", 1, priceJSON("prod_2", "usd", 2500, "week", 1)),
			subJSON("sub_16", `"cus_o"`, "active", 1, priceJSON("prod_3", "usd", 4900, "month", 1)),
			// A coupon since deleted: it exists only inside this discount.
			subJSON("sub_17", `"cus_p"`, "active", 1, starterMonthly, `{"id":"di_7","coupon":{"id":"GONE","percent_off":5,"created":10}}`),
			subJSON("sub_18", `"cus_q"`, "active", 1, starterMonthly, `{"id":"di_8","coupon":{"id":"EURO5"}}`),
		),
	},
	"/v1/coupons": {listJSON(false,
		`{"id":"WELCOME10","percent_off":10,"created":100}`,
		`{"id":"HALF","percent_off":12.5,"created":50}`,
		`{"id":"TENOFF","amount_off":1000,"currency":"usd","created":200}`,
		`{"id":"VIP","percent_off":20,"created":300}`,
		`{"id":"EURO5","amount_off":500,"currency":"eur","created":400}`,
		`{"id":"FINE","percent_off":33.333,"created":500}`,
	)},
	"/v1/products": {listJSON(false,
		`{"id":"prod_1","name":"Starter"}`, `{"id":"prod_2","name":"Pro"}`, `{"id":"prod_3","name":"Starter!"}`,
	)},
}

// fakeStripe serves an account's pages in order and fails the test on any
// request Stripe's read-only API would not get from blast.
func fakeStripe(t *testing.T, pages map[string][]string) *Client {
	t.Helper()
	cursors := map[string]string{"": "sub_03"} // page 2 of subscriptions follows sub_03
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method != http.MethodGet:
			t.Errorf("%s %s: only GET may ever be sent", r.Method, r.URL.Path)
		case r.Header.Get("Authorization") != "Bearer "+testKey:
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		case r.Header.Get("Stripe-Version") != apiVersion:
			t.Errorf("Stripe-Version = %q", r.Header.Get("Stripe-Version"))
		case q.Get("limit") != "100":
			t.Errorf("limit = %q", q.Get("limit"))
		}
		if r.URL.Path == "/v1/subscriptions" {
			if got, want := q["expand[]"], []string{"data.customer", "data.discounts", "data.items.data.discounts"}; !reflect.DeepEqual(got, want) {
				t.Errorf("expand[] = %v, want %v", got, want)
			}
		}
		served := pages[r.URL.Path]
		n := 0
		if after := q.Get("starting_after"); after != "" {
			if after != cursors[""] {
				t.Errorf("starting_after = %q, want %q", after, cursors[""])
			}
			n = 1
		}
		if n >= len(served) {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, served[n])
	}))
	t.Cleanup(srv.Close)
	return client(t, srv.URL)
}

func client(t *testing.T, url string) *Client {
	t.Helper()
	c, err := NewClient(testKey, url, "blast/test", false)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(time.Duration) {}
	return c
}

func TestImportModelsTheAccount(t *testing.T) {
	res, err := Import(fakeStripe(t, acme), "USD")
	if err != nil {
		t.Fatal(err)
	}

	prices := func(monthly, annual int64) model.Plan {
		p := model.Plan{Prices: map[model.Billing]int64{}}
		if monthly > 0 {
			p.Prices[model.Monthly] = monthly
		}
		if annual > 0 {
			p.Prices[model.Annual] = annual
		}
		return p
	}
	wantPlans := map[string]model.Plan{
		"starter":   prices(2900, 29000),
		"starter-2": prices(4900, 0), // another product whose name reduces to the same
		// Pro is sold at two monthly prices, so each of its prices stands alone.
		"pro-79-monthly": prices(7900, 0),
		"pro-99-monthly": prices(9900, 0),
		"pro-990-annual": prices(0, 99000),
	}
	if !reflect.DeepEqual(res.Config.Plans, wantPlans) {
		t.Errorf("plans:\n got  %v\n want %v", res.Config.Plans, wantPlans)
	}

	wantDiscounts := []model.Discount{ // oldest coupon first
		{Name: "GONE", BasisPoints: 500},
		{Name: "HALF", BasisPoints: 1250},
		{Name: "WELCOME10", BasisPoints: 1000},
		{Name: "TENOFF", AmountCents: 1000},
		{Name: "VIP", BasisPoints: 2000},
	}
	if !reflect.DeepEqual(res.Config.Discounts, wantDiscounts) {
		t.Errorf("discounts:\n got  %v\n want %v", res.Config.Discounts, wantDiscounts)
	}

	who := func(id, plan string, billing model.Billing, quantity int64, coupons ...string) model.Customer {
		return model.Customer{ID: id, Plan: plan, Billing: billing, Quantity: quantity, Coupons: append([]string{}, coupons...)}
	}
	wantCustomers := []model.Customer{
		who("cus_a", "pro-99-monthly", model.Monthly, 5, "WELCOME10"),
		who("cus_b", "pro-990-annual", model.Annual, 1),
		who("cus_c:sub_03", "starter", model.Monthly, 1, "HALF"), // two subscriptions, told apart
		who("cus_c:sub_04", "starter", model.Annual, 2),
		who("cus_d", "pro-79-monthly", model.Monthly, 1),
		who("cus_j", "starter", model.Monthly, 1, "TENOFF"),
		who("cus_k", "starter", model.Monthly, 1, "VIP"), // the customer's own discount
		who("cus_o", "starter-2", model.Monthly, 1),
		who("cus_p", "starter", model.Monthly, 1, "GONE"),
	}
	if !reflect.DeepEqual(res.Customers, wantCustomers) {
		t.Errorf("customers:\n got  %+v\n want %+v", res.Customers, wantCustomers)
	}

	wantSkipped := []Skipped{
		{"a discount whose coupon could not be read", 1},
		{"billed every 3 months", 1},
		{"billed every week", 1},
		{"billed in EUR (import those with --currency eur)", 1},
		{"coupon in another currency", 1},
		{"coupon with a percentage finer than hundredths", 1},
		{"metered, tiered or one-off price", 1},
		{"more than one item on the subscription", 1},
		{"status is incomplete", 1},
	}
	if res.Found != 18 || !reflect.DeepEqual(res.Skipped, wantSkipped) {
		t.Errorf("found %d, skipped:\n got  %v\n want %v", res.Found, res.Skipped, wantSkipped)
	}

	// What was imported prices to the cent: five seats at $99 less 10%, and
	// $29 less 12.5%, which no float holds exactly.
	engine, err := sim.New(res.Config)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range map[int]int64{0: 44550, 2: 2538} {
		inv, err := engine.Invoice(res.Customers[i])
		if err != nil || inv.TotalCents != want {
			t.Errorf("%s is invoiced %d cents (%v), want %d", res.Customers[i].ID, inv.TotalCents, err, want)
		}
	}
}

func TestCurrencyMustBeOneBlastCanCount(t *testing.T) {
	for currency, want := range map[string]string{
		"dollars": `"dollars" is not a three-letter currency code`,
		"JPY":     "JPY is not counted in hundredths, which is all blast can price",
	} {
		if _, err := Import(client(t, "http://127.0.0.1:1"), currency); err == nil || err.Error() != want {
			t.Errorf("%s: got %v, want %s", currency, err, want)
		}
	}
}

func TestKeyRules(t *testing.T) {
	cases := []struct {
		key  string
		live bool
		want string // a fragment of the refusal; empty when the key is accepted
	}{
		{"rk_test_abc", false, ""},
		{"sk_test_abc", false, ""},
		{"rk_live_abc", true, ""},
		{"rk_live_abc", false, "add --live"},
		{"sk_live_abc", false, "live secret key, which can move money"},
		{"sk_live_abc", true, "live secret key, which can move money"},
		{"", false, "STRIPE_API_KEY is not set"},
		{"pk_test_abc", false, "does not look like a Stripe key"},
		{"hunter2", true, "does not look like a Stripe key"},
	}
	for _, tc := range cases {
		_, err := NewClient(tc.key, APIURL, "blast/test", tc.live)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s (live=%t) was refused: %v", tc.key, tc.live, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s (live=%t): got %v, want a refusal mentioning %q", tc.key, tc.live, err, tc.want)
		case err != nil && tc.key != "" && strings.Contains(err.Error(), tc.key):
			t.Errorf("the refusal repeats the key: %v", err)
		}
	}
}

func TestRefusalsAreReportedInStripesWords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","message":"The provided key 'rk_test_***Tests' does not have the required permissions for this endpoint."}}`)
	}))
	defer srv.Close()
	_, err := Import(client(t, srv.URL), "usd")
	const want = "stripe: /v1/subscriptions: The provided key 'rk_test_***Tests' does not have the required permissions for this endpoint. (HTTP 403)"
	if err == nil || err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}
}

func TestBusyStripeIsTriedAgainAndARefusalIsNot(t *testing.T) {
	for status, wantCalls := range map[int]int{http.StatusTooManyRequests: 3, http.StatusBadGateway: 3, http.StatusUnauthorized: 1} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(status)
		}))
		c := client(t, srv.URL)
		var waits []time.Duration
		c.sleep = func(d time.Duration) { waits = append(waits, d) }
		err := c.get("/v1/coupons", nil, &struct{}{})
		srv.Close()
		if want := fmt.Sprintf("stripe: /v1/coupons: HTTP %d", status); err == nil || err.Error() != want {
			t.Errorf("HTTP %d: got %v, want %s", status, err, want)
		}
		if calls != wantCalls || len(waits) != wantCalls-1 {
			t.Errorf("HTTP %d: %d requests and %d waits, want %d and %d", status, calls, len(waits), wantCalls, wantCalls-1)
		}
	}

	// And it recovers when Stripe does.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls++; calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, listJSON(false, `{"id":"A","percent_off":10}`))
	}))
	defer srv.Close()
	got, err := list(client(t, srv.URL), "/v1/coupons", map[string][]string{}, func(c coupon) string { return c.ID })
	if err != nil || len(got) != 1 {
		t.Errorf("after two busy answers: %v, %v", got, err)
	}
}

// The key belongs to Stripe. A redirect must not carry it anywhere else.
func TestRedirectsAreNotFollowed(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a redirect was followed, carrying Authorization %q", r.Header.Get("Authorization"))
	}))
	defer elsewhere.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()
	err := client(t, srv.URL).get("/v1/coupons", nil, &struct{}{})
	if err == nil || err.Error() != "stripe: /v1/coupons: HTTP 302" {
		t.Errorf("got %v, want the redirect reported as an error", err)
	}
}

func TestRunawayResponsesAreCutOff(t *testing.T) {
	t.Run("a list that never ends", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, listJSON(true, `{"id":"SAME"}`))
		}))
		defer srv.Close()
		_, err := list(client(t, srv.URL), "/v1/coupons", map[string][]string{}, func(c coupon) string { return c.ID })
		if want := `stripe: /v1/coupons: the list does not advance past "SAME"`; err == nil || err.Error() != want {
			t.Errorf("got %v, want %s", err, want)
		}
	})
	t.Run("a body with no end", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			chunk := []byte(strings.Repeat(" ", 1<<20))
			for range maxBody>>20 + 1 {
				w.Write(chunk)
			}
		}))
		defer srv.Close()
		err := client(t, srv.URL).get("/v1/coupons", nil, &struct{}{})
		if want := "stripe: /v1/coupons answered with more than 32 MB; refusing to read it"; err == nil || err.Error() != want {
			t.Errorf("got %v, want %s", err, want)
		}
	})
	t.Run("an answer that is not JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "<html>captive portal</html>")
		}))
		defer srv.Close()
		err := client(t, srv.URL).get("/v1/coupons", nil, &struct{}{})
		if err == nil || !strings.HasPrefix(err.Error(), "stripe: /v1/coupons: unexpected answer: ") {
			t.Errorf("got %v", err)
		}
	})
}
