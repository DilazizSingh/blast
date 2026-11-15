# Importing from Stripe

`blast import stripe` reads the subscriptions in a Stripe account and writes
the four files `blast check` needs. After that, blast works offline.

It is the only command that uses the network. It talks to `api.stripe.com`
and nowhere else, sends `GET` requests only, and keeps nothing from the
answers beyond ids, prices, quantities and coupons. Names, emails, addresses
and payment methods are never written to disk.

## 1. Make a restricted, read-only key

Do this in **test mode** first. Test mode is free and holds fake data.

1. Open the Stripe Dashboard and switch to test mode (or a sandbox).
2. Go to **Developers → API keys → Create restricted key**.
3. Set every permission to **None**, then set these five to **Read**:
   Customers, Subscriptions, Prices, Products, Coupons.
4. Create the key. It starts with `rk_test_`.

blast refuses a live secret key (`sk_live_…`) outright. A secret key can move
money and blast only needs to read.

## 2. Run the import

Give blast the key through the environment, never on the command line, so it
stays out of your shell history and the process list:

```sh
read -rs STRIPE_API_KEY && export STRIPE_API_KEY   # paste the key, press Enter
blast import stripe --out pricing
unset STRIPE_API_KEY
```

```
 Imported from Stripe (test mode) into pricing
   customers.json    412 customers
   pricing.old.yml   6 plans, 3 coupons
   pricing.new.yml   a copy of today's pricing, for you to edit
   rules.yml         no rules yet, with two examples to start from

 Left out 17 subscriptions of 429 that blast cannot model:
       9  more than one item on the subscription
       5  status is incomplete
       3  billed every 3 months

 Next: edit pricing.new.yml, then run "blast check".
```

| Flag | Meaning |
|---|---|
| `--out DIR` | where to write the files (default: here) |
| `--currency CODE` | which currency's subscriptions to import (default `usd`) |
| `--force` | overwrite `customers.json` and `pricing.old.yml` |
| `--live` | accept a live-mode restricted key |

Importing again is safe. `customers.json` and `pricing.old.yml` mirror Stripe
and are replaced only with `--force`. `pricing.new.yml` and `rules.yml` are
yours and are never overwritten.

## 3. Live data

When the test-mode import looks right, create the same restricted key in live
mode (`rk_live_…`) and add `--live`:

```sh
blast import stripe --live --out pricing
```

`customers.json` is written readable by you alone (mode 0600). It holds
Stripe customer ids and what each pays. Decide deliberately whether it
belongs in your repository; for many teams a private repository is fine, for
others it should be ignored and re-imported in CI.

Revoke the key in the Dashboard when you no longer need it.

## How Stripe maps onto blast

| Stripe | blast |
|---|---|
| Subscription with one item | one customer |
| Customer id (`cus_…`) | the customer's `id`; `cus_…:sub_…` when a customer has several subscriptions |
| Product | a plan, named after the product: "Pro (Teams)" becomes `pro-teams` |
| Price billed every month / every year | the plan's `monthly` / `annual` price |
| Item quantity | `quantity` |
| Coupon | a discount with the coupon's id, as `percent` or `amount` |
| Discounts on the subscription or its item | the customer's `coupons` |
| Discount on the customer | their `coupons`, when the subscription has none of its own |

When one product is sold at more than one price for the same period (an old
price kept for early customers, say), nothing in Stripe says which monthly
price belongs with which annual one. Each such price becomes its own plan,
named for its amount: `pro-79-monthly`, `pro-99-monthly`.

Subscriptions that are `active`, `trialing` or `past_due` are imported.

## What is left out, and why

Each of these is counted and reported, never silently dropped:

- **More than one item.** blast models one plan per customer.
- **Metered or tiered prices.** The invoice depends on usage blast cannot see.
- **Billing periods other than a month or a year.**
- **Other currencies.** Import them separately with `--currency`.
- **Coupons blast cannot represent**: a percentage finer than hundredths, or
  an amount in another currency.

Three things are imported but simplified. Know them before trusting a number:

- **Stacking order.** Stripe applies a subscription's discounts in the order
  they were added. blast applies them in the order `pricing.old.yml` lists
  them, which the import sets to oldest coupon first. For percentages the two
  agree to within a cent; where a fixed amount stacks with a percentage they
  can differ. Reorder the `discounts:` section if your account needs it.
- **Coupon duration.** A coupon attached today is treated as still attached
  on the next invoice, even if it is about to run out.
- **Coupons limited to certain products** are treated as applying.

Tax, proration, trials and credits are not modelled; see
[pricing-model.md](pricing-model.md).

## The API version

blast pins `Stripe-Version: 2026-10-28.endive`, the version its response
handling was written against, so an account's own default does not change
what it reads. Stripe has moved the coupon inside a discount between
versions; blast reads it from either place.

The import was built and tested against recorded response shapes on a local
stand-in server. Run it in test mode before trusting it with live data, and
please report any subscription it reads wrongly.
