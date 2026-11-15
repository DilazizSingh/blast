### ❌ Pricing change breaks 2 of 3 rules

`pricing.old.yml` → `pricing.new.yml` · 10 customers simulated

| | |
|---|---|
| **Blast radius** | 5 of 10 customers (50%) |
| **Revenue per cycle** | \$5,229.00 → \$4,584.60 (-\$644.40, -12.3%) |

#### Rules

| | Rule | Result |
|:-:|---|---|
| ❌ | `max-one-discount` | 2 customers: `c4`, `c7` |
| ❌ | `no-legacy-repricing` | 1 customer: `c6` |
| ✅ | `finance-approval` | not triggered |

#### Segments

| Segment | Affected | Revenue change |
|---|--:|--:|
| `annual + referred` | 5 | -\$644.40 |
| `monthly` | 0 | \$0.00 |
| `legacy plans` | 1 | -\$158.00 |
| `has coupon` | 2 | -\$230.40 |

#### Customers, largest change first

| Customer | Plan | Old | New | Change | Rules broken |
|---|---|--:|--:|--:|---|
| `c3` | `pro annual` | \$990.00 | \$792.00 | -\$198.00 |  |
| `c4` | `pro annual` | \$891.00 | \$712.80 | -\$178.20 | `max-one-discount` |
| `c6` | `legacy-pro annual` | \$790.00 | \$632.00 | -\$158.00 | `no-legacy-repricing` |
| `c8` | `starter annual` | \$290.00 | \$232.00 | -\$58.00 |  |
| `c7` | `starter annual` | \$261.00 | \$208.80 | -\$52.20 | `max-one-discount` |

<details><summary>Pricing settings that changed (2)</summary>

| Setting | Before | After |
|---|---|---|
| `discounts.REFERRAL20.percent` |  | `20%` |
| `discounts.REFERRAL20.when` |  | `billing == "annual" and referred` |

</details>

<sub>Every number here was computed from the pricing files and the customer list. None is estimated.</sub>
