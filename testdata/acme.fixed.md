### ✅ Pricing change passes all 3 rules

`pricing.old.yml` → `pricing.fixed.yml` · 10 customers simulated

| | |
|---|---|
| **Blast radius** | 2 of 10 customers (20%) |
| **Revenue per cycle** | \$5,229.00 → \$4,973.00 (-\$256.00, -4.9%) |

#### Rules

| | Rule | Result |
|:-:|---|---|
| ✅ | `max-one-discount` | ok |
| ✅ | `no-legacy-repricing` | ok |
| ✅ | `finance-approval` | not triggered |

#### Segments

| Segment | Affected | Revenue change |
|---|--:|--:|
| `annual + referred` | 2 | -\$256.00 |
| `monthly` | 0 | \$0.00 |
| `legacy plans` | 0 | \$0.00 |
| `has coupon` | 0 | \$0.00 |

#### Customers, largest change first

| Customer | Plan | Old | New | Change | Rules broken |
|---|---|--:|--:|--:|---|
| `c3` | `pro annual` | \$990.00 | \$792.00 | -\$198.00 |  |
| `c8` | `starter annual` | \$290.00 | \$232.00 | -\$58.00 |  |

<details><summary>Pricing settings that changed (2)</summary>

| Setting | Before | After |
|---|---|---|
| `discounts.REFERRAL20.percent` |  | `20%` |
| `discounts.REFERRAL20.when` |  | `billing == "annual" and referred and not legacy and coupon_count == 0` |

</details>

<sub>Every number here was computed from the pricing files and the customer list. None is estimated.</sub>
