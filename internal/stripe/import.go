package stripe

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/dilaziz/blast/internal/model"
)

// The objects below are Stripe's, cut down to the fields an import reads.
// Nothing else in a response, a customer's name or email included, is kept.

// ref is a field holding an object's id or, when expanded, the object.
type ref[T any] struct {
	ID  string
	Obj *T
}

func (r *ref[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		return json.Unmarshal(data, &r.ID)
	}
	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	r.ID, r.Obj = object.ID, new(T)
	return json.Unmarshal(data, r.Obj)
}

type subscription struct {
	ID       string        `json:"id"`
	Status   string        `json:"status"`
	Customer ref[customer] `json:"customer"`
	// Discounts is the current field; Discount is what older API versions
	// call the same thing. Either may be present.
	Discounts []ref[discount] `json:"discounts"`
	Discount  ref[discount]   `json:"discount"`
	Items     struct {
		Data    []item `json:"data"`
		HasMore bool   `json:"has_more"`
	} `json:"items"`
}

type customer struct {
	Discount ref[discount] `json:"discount"`
}

type item struct {
	Price     price           `json:"price"`
	Quantity  *int64          `json:"quantity"` // null on a metered price
	Discounts []ref[discount] `json:"discounts"`
}

type price struct {
	Product       ref[struct{}] `json:"product"`
	Currency      string        `json:"currency"`
	UnitAmount    *int64        `json:"unit_amount"` // null on a tiered price
	BillingScheme string        `json:"billing_scheme"`
	Recurring     *struct {
		Interval      string `json:"interval"`
		IntervalCount int64  `json:"interval_count"`
		UsageType     string `json:"usage_type"`
	} `json:"recurring"`
}

// discount is a coupon applied to something. API versions differ on where
// the coupon sits, so both places are read.
type discount struct {
	Coupon ref[coupon] `json:"coupon"`
	Source struct {
		Coupon ref[coupon] `json:"coupon"`
	} `json:"source"`
}

type coupon struct {
	ID string `json:"id"`
	// PercentOff stays text: 12.5 must reach blast as 1250 basis points
	// without passing through a float.
	PercentOff json.Number `json:"percent_off"`
	AmountOff  *int64      `json:"amount_off"`
	Currency   string      `json:"currency"`
	Created    int64       `json:"created"`
}

type product struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Result is an account as blast models it.
type Result struct {
	Config    model.Config
	Customers []model.Customer
	Found     int       // subscriptions in the account, canceled ones aside
	Skipped   []Skipped // those left out, most common reason first
}

// Skipped counts the subscriptions left out for one reason.
type Skipped struct {
	Reason string
	Count  int
}

// billable are the subscription states that will be invoiced.
var billable = []string{"active", "trialing", "past_due"}

var billings = map[string]model.Billing{"month": model.Monthly, "year": model.Annual}

// Stripe counts these currencies in whole units or in thousandths; blast
// counts in hundredths and would be wrong about them by that factor.
var unsupported = strings.Fields("bif clp djf gnf jpy kmf krw mga pyg rwf ugx vnd vuv xaf xof xpf bhd jod kwd omr tnd")

// Import reads the account's subscriptions billed in currency and models
// them: one customer per subscription, one plan per product.
func Import(c *Client, currency string) (Result, error) {
	currency = strings.ToLower(currency)
	switch {
	case !currencyCode.MatchString(currency):
		return Result{}, fmt.Errorf("%q is not a three-letter currency code", currency)
	case slices.Contains(unsupported, currency):
		return Result{}, fmt.Errorf("%s is not counted in hundredths, which is all blast can price", strings.ToUpper(currency))
	}

	subs, err := list(c, "/v1/subscriptions",
		url.Values{"expand[]": {"data.customer", "data.discounts", "data.items.data.discounts"}},
		func(s subscription) string { return s.ID })
	if err != nil {
		return Result{}, err
	}
	coupons, err := list(c, "/v1/coupons", url.Values{}, func(c coupon) string { return c.ID })
	if err != nil {
		return Result{}, err
	}
	products, err := list(c, "/v1/products", url.Values{}, func(p product) string { return p.ID })
	if err != nil {
		return Result{}, err
	}

	a := account{currency: currency, coupons: map[string]coupon{}, names: map[string]string{}}
	for _, c := range coupons {
		a.coupons[c.ID] = c
	}
	for _, p := range products {
		a.names[p.ID] = p.Name
	}
	return a.model(subs), nil
}

type account struct {
	currency string
	coupons  map[string]coupon // by id
	names    map[string]string // product names, by id
}

// seat is one subscription blast can price.
type seat struct {
	customer, subscription, product string
	billing                         model.Billing
	cents, quantity                 int64
	coupons                         []coupon
}

// planKey is what makes two subscriptions the same plan at the same price.
type planKey struct {
	product string
	billing model.Billing
	cents   int64
}

func (s seat) key() planKey { return planKey{s.product, s.billing, s.cents} }

func (a account) model(subs []subscription) Result {
	res := Result{Found: len(subs), Config: model.Config{Plans: map[string]model.Plan{}}}
	var (
		seats   []seat
		skipped = map[string]int{}
		held    = map[string]int{} // subscriptions per customer
		used    = map[string]coupon{}
	)
	for _, sub := range subs {
		s, why := a.read(sub)
		if why != "" {
			skipped[why]++
			continue
		}
		seats = append(seats, s)
		held[s.customer]++
		for _, c := range s.coupons {
			used[c.ID] = c
		}
	}

	plans := a.plans(seats, res.Config.Plans)
	for _, s := range seats {
		c := model.Customer{ID: s.customer, Plan: plans[s.key()], Billing: s.billing, Coupons: []string{}, Quantity: s.quantity}
		if held[s.customer] > 1 {
			c.ID += ":" + s.subscription
		}
		for _, coupon := range s.coupons {
			if !c.HasCoupon(coupon.ID) {
				c.Coupons = append(c.Coupons, coupon.ID)
			}
		}
		res.Customers = append(res.Customers, c)
	}
	slices.SortFunc(res.Customers, func(x, y model.Customer) int { return cmp.Compare(x.ID, y.ID) })

	// Stripe has no stacking order of its own to copy; oldest first is at
	// least stable from one import to the next.
	oldestFirst := func(x, y coupon) int { return cmp.Or(cmp.Compare(x.Created, y.Created), cmp.Compare(x.ID, y.ID)) }
	for _, c := range slices.SortedFunc(maps.Values(used), oldestFirst) {
		d, _ := a.discount(c)
		res.Config.Discounts = append(res.Config.Discounts, d)
	}
	for why, n := range skipped {
		res.Skipped = append(res.Skipped, Skipped{why, n})
	}
	slices.SortFunc(res.Skipped, func(x, y Skipped) int {
		return cmp.Or(cmp.Compare(y.Count, x.Count), cmp.Compare(x.Reason, y.Reason))
	})
	return res
}

// read turns a subscription into a seat, or says why blast cannot price it.
func (a account) read(sub subscription) (s seat, why string) {
	switch {
	case !slices.Contains(billable, sub.Status):
		return s, "status is " + sub.Status
	case len(sub.Items.Data) != 1 || sub.Items.HasMore:
		return s, "more than one item on the subscription"
	}
	it := sub.Items.Data[0]
	p := it.Price
	switch {
	case p.Currency != a.currency:
		return s, fmt.Sprintf("billed in %s (import those with --currency %s)", strings.ToUpper(p.Currency), p.Currency)
	case p.Recurring == nil || p.Recurring.UsageType != "licensed" || p.BillingScheme != "per_unit" || p.UnitAmount == nil || it.Quantity == nil:
		return s, "metered, tiered or one-off price"
	case p.Recurring.IntervalCount != 1:
		return s, fmt.Sprintf("billed every %d %ss", p.Recurring.IntervalCount, p.Recurring.Interval)
	case billings[p.Recurring.Interval] == "":
		return s, "billed every " + p.Recurring.Interval
	case *it.Quantity < 1 || *it.Quantity > model.MaxQuantity || *p.UnitAmount < 0 || *p.UnitAmount > model.MaxCents / *it.Quantity:
		return s, "quantity or amount outside what blast prices"
	}

	// A subscription's own discounts take the place of its customer's.
	applied := slices.Concat(it.Discounts, sub.Discounts, []ref[discount]{sub.Discount})
	if c := sub.Customer.Obj; c != nil && !slices.ContainsFunc(applied, func(d ref[discount]) bool { return d.ID != "" }) {
		applied = []ref[discount]{c.Discount}
	}
	s = seat{
		customer: sub.Customer.ID, subscription: sub.ID, product: p.Product.ID,
		billing: billings[p.Recurring.Interval], cents: *p.UnitAmount, quantity: *it.Quantity,
	}
	for _, d := range applied {
		if d.ID == "" {
			continue
		}
		c, ok := a.coupon(d)
		if !ok {
			return s, "a discount whose coupon could not be read"
		}
		if _, why := a.discount(c); why != "" {
			return s, why
		}
		s.coupons = append(s.coupons, c)
	}
	return s, ""
}

// coupon finds the coupon behind a discount. The account's list is tried
// first; a deleted coupon survives only inside the discounts made from it.
func (a account) coupon(d ref[discount]) (coupon, bool) {
	if d.Obj == nil {
		return coupon{}, false
	}
	r := d.Obj.Coupon
	if r.ID == "" {
		r = d.Obj.Source.Coupon
	}
	if c, ok := a.coupons[r.ID]; ok {
		return c, true
	}
	if r.Obj != nil {
		return *r.Obj, true
	}
	return coupon{}, false
}

// discount is a coupon in blast's terms, or the reason it has none.
func (a account) discount(c coupon) (d model.Discount, why string) {
	switch {
	case c.PercentOff != "":
		bp, err := model.ParseHundredths(c.PercentOff.String())
		if err != nil || bp <= 0 || bp > 100_00 {
			return d, "coupon with a percentage finer than hundredths"
		}
		return model.Discount{Name: c.ID, BasisPoints: bp}, ""
	case c.AmountOff == nil || *c.AmountOff <= 0 || *c.AmountOff > model.MaxCents:
		return d, "coupon with neither a percentage nor an amount"
	case c.Currency != a.currency:
		return d, "coupon in another currency"
	}
	return model.Discount{Name: c.ID, AmountCents: *c.AmountOff}, ""
}

// plans names a plan for every product and price in use, fills in into, and
// returns the name for each. A product becomes one plan with a monthly and an
// annual price. When a product is sold at more than one price for the same
// period, nothing says which monthly price belongs with which annual one, so
// each of its prices becomes a plan of its own, such as "pro-79-monthly".
func (a account) plans(seats []seat, into map[string]model.Plan) map[planKey]string {
	keys := map[planKey]bool{}
	prices := map[string]map[model.Billing]int{} // distinct prices per product and period
	for _, s := range seats {
		if !keys[s.key()] {
			keys[s.key()] = true
			if prices[s.product] == nil {
				prices[s.product] = map[model.Billing]int{}
			}
			prices[s.product][s.billing]++
		}
	}
	// By product, monthly before annual, cheapest first: the order decides
	// which of two clashing names gets the number.
	ordered := slices.SortedFunc(maps.Keys(keys), func(x, y planKey) int {
		return cmp.Or(cmp.Compare(x.product, y.product), cmp.Compare(y.billing, x.billing), cmp.Compare(x.cents, y.cents))
	})

	names := map[planKey]string{}
	byProduct := map[string]string{}
	for _, k := range ordered {
		base := slug(cmp.Or(a.names[k.product], k.product))
		if n := prices[k.product]; n[model.Monthly] > 1 || n[model.Annual] > 1 {
			names[k] = unique(fmt.Sprintf("%s-%s-%s", base, model.Decimal(k.cents), k.billing), into)
			into[names[k]] = model.Plan{Prices: map[model.Billing]int64{k.billing: k.cents}}
			continue
		}
		name, seen := byProduct[k.product]
		if !seen {
			name = unique(base, into)
			byProduct[k.product] = name
			into[name] = model.Plan{Prices: map[model.Billing]int64{}}
		}
		into[name].Prices[k.billing] = k.cents
		names[k] = name
	}
	return names
}

var (
	currencyCode = regexp.MustCompile(`^[a-z]{3}$`)
	notSlug      = regexp.MustCompile(`[^a-z0-9]+`)
)

// slug makes a plan name from a product's: "Pro (Teams)" becomes "pro-teams".
func slug(name string) string {
	return cmp.Or(strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-"), "plan")
}

// unique returns name, or name with the first number that makes it unused.
func unique(name string, taken map[string]model.Plan) string {
	candidate := name
	for n := 2; ; n++ {
		if _, used := taken[candidate]; !used {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", name, n)
	}
}
