// Package model holds the plain data blast reasons about: pricing configs and
// the customers priced against them. All money is whole cents in an int64.
package model

import "slices"

// Billing is how often a customer is invoiced.
type Billing string

const (
	Monthly Billing = "monthly"
	Annual  Billing = "annual"
)

// Valid reports whether b is a billing period blast can price.
func (b Billing) Valid() bool { return b == Monthly || b == Annual }

// Plan is one sellable plan.
type Plan struct {
	// Prices is cents per invoice for each billing period the plan is sold
	// on. A period that is absent is not offered, which is different from
	// a free plan priced at zero.
	Prices map[Billing]int64
	Legacy bool // grandfathered: existing customers were promised these terms
}

// Discount takes money off an invoice. Exactly one of BasisPoints and
// AmountCents is set.
type Discount struct {
	Name        string
	BasisPoints int64  // percent off in hundredths of a percent: 20% is 2000
	AmountCents int64  // fixed amount off each invoice
	When        string // optional condition; where it holds, no coupon is needed
}

// Config is one version of a pricing setup.
type Config struct {
	Plans map[string]Plan
	// Discounts are kept in the order the file declares them, because that
	// is the order they are applied in.
	Discounts []Discount
	// Payment is the file's payment section flattened to dotted paths. blast
	// does not interpret it; it exists so rules can ask whether it changed.
	Payment map[string]string
}

// HasDiscount reports whether the config declares a discount by that name.
func (c Config) HasDiscount(name string) bool {
	return slices.ContainsFunc(c.Discounts, func(d Discount) bool { return d.Name == name })
}

// Customer is one customer's billing situation.
type Customer struct {
	ID       string   `json:"id"`
	Plan     string   `json:"plan"`
	Billing  Billing  `json:"billing"`
	Coupons  []string `json:"coupons"`
	Referred bool     `json:"referred"`
	// Quantity is how many seats or units the plan's price is charged for.
	// Zero means it was not stated, which is one.
	Quantity int64 `json:"quantity,omitempty"`
}

// Units is the quantity the customer is billed for, never less than one.
func (c Customer) Units() int64 { return max(c.Quantity, 1) }

// HasCoupon reports whether the customer holds the named coupon.
func (c Customer) HasCoupon(name string) bool { return slices.Contains(c.Coupons, name) }
