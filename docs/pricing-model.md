# The pricing model

How blast turns a customer and a pricing file into an invoice. Every number
blast prints comes from the rules on this page, implemented in
[`internal/sim`](../internal/sim/sim.go) and pinned by its tests.

## Money

Money is whole cents in an `int64`. Nothing is ever a float: prices are read
from the file as text (`99.50` becomes `9950`), and all arithmetic is integer
arithmetic. Amounts are capped at $1 billion so no intermediate product can
overflow.

## What an invoice is

One invoice is what one customer pays for one billing cycle: a month for
monthly customers, a year for annual ones. Totals in a report therefore add
monthly and annual invoices together, as "revenue per cycle".

    invoice = list price of the customer's plan for their billing period
              minus each discount that applies, one after another

## Which discounts apply

A discount applies to a customer when **either** is true:

1. the customer holds it as a coupon (it is listed in their `coupons`), or
2. the discount has a `when:` condition and the condition is true for them.

So a discount with no `when:` is a classic coupon, and a discount with a
`when:` is automatic for everyone it describes. A coupon a customer holds
applies even if the discount's `when:` is false for them: `when:` widens who
gets a discount, it does not take a coupon away.

A discount applies **at most once** per invoice, even to a customer who both
holds the coupon and meets the condition. A coupon the pricing file no longer
declares is ignored, which is how retiring a coupon is simulated.

## Stacking order

Discounts stack **sequentially, in the order the pricing file declares
them**. Each one is taken off the running total left by the one before, so
10% then 20% leaves `0.9 × 0.8 = 72%` of the price, not 70%.

The order of a customer's `coupons` list does not matter. The file's order is
the one place stacking is controlled, and reordering two discounts in the file
is a pricing change like any other. It matters as soon as fixed amounts are
involved:

| Declared order | $99.00 becomes |
|---|---|
| 50% off, then $10 off | $49.50, then **$39.50** |
| $10 off, then 50% off | $89.00, then **$44.50** |

## Percent discounts and rounding

A percent is stored in basis points (`20%` is `2000`, `12.5%` is `1250`), so
fractional percents are exact. The price that remains after a percent
discount is rounded **half up to a whole cent**:

    remaining = (amount × (10000 − basis points) + 5000) / 10000     (integer division)

Rounding happens after every discount, not once at the end, because each step
is a real amount a customer could be shown.

Worked example, customer c4 (pro annual, `WELCOME10`, referred):

| Step | Arithmetic | Running total |
|---|---|---:|
| List price | | $990.00 |
| `WELCOME10`, 10% | (99000 × 9000 + 5000) / 10000 | $891.00 |
| `REFERRAL20`, 20% | (89100 × 8000 + 5000) / 10000 | $712.80 |

Half-cent cases round up: 50% off 3 cents leaves 2 cents, not 1.

## Fixed-amount discounts

`amount: 10` takes $10.00 off the invoice, whatever the billing period. A
fixed amount never takes an invoice below zero: $10 off a $9 invoice is $0.00.

## Conditions

`when:` is written in blast's expression language. It can read these facts
about a customer:

| Name | Type | Meaning |
|---|---|---|
| `id` | text | the customer's id |
| `plan` | text | the name of their plan |
| `billing` | text | `"monthly"` or `"annual"` |
| `referred` | true/false | they were referred by someone |
| `legacy` | true/false | their plan is marked `legacy: true` |
| `coupon_count` | number | how many coupons they hold |

Each also answers to a qualified name (`customer.billing`, `plan.legacy`,
`plan.name`, ...), which reads better in rules. Operators are `and`, `or`,
`not`, `==`, `!=`, `<`, `<=`, `>`, `>=` and parentheses. A misspelt name or a
type mismatch is an error when the file loads, not a surprise at customer 5,000.

## What is not modelled

Trials, proration, tax, usage-based pricing and multiple currencies. A green
result means "no rule broke in this model", not "this change is safe".
