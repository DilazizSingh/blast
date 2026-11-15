# Walkthrough: build it, test it, use it

Every command below was run while writing this. Work through it top to
bottom the first time; each part stands alone after that.

You need Go 1.27 or newer and git. Nothing else.

## 1. Build and run the tests

```sh
make build        # writes bin/blast
make test         # every test, about 5 seconds
```

`make test` must end with `ok` on every line. Put the binary on your path, or
call it as `./bin/blast` below:

```sh
export PATH="$PWD/bin:$PATH"
blast version
```

## 2. The example: red, then green

```sh
mkdir /tmp/try && cd /tmp/try
blast init          # writes pricing.old.yml, pricing.new.yml, customers.json, rules.yml
blast check; echo "exit code $?"
```

You should see a total of `$5,229.00 -> $4,584.60 (-$644.40, -12.3%)`, a blast
radius of `5 of 10 customers (50%)`, two broken rules, and `exit code 1`.

The proposed change gives a 20% referral discount to everyone on annual
billing who was referred. It also lands on a legacy plan and stacks on top of
an existing coupon. Fix it by narrowing the condition in `pricing.new.yml`:

```yaml
    when: billing == "annual" and referred and not legacy and coupon_count == 0
```

```sh
blast check; echo "exit code $?"
```

Now only c3 and c8 change, the total is `$4,973.00`, every rule passes, and
the exit code is 0. (`pricing.fixed.yml` holds the same fix if you would
rather run `blast check --after pricing.fixed.yml`.)

## 3. The dashboard, live

Undo the fix (`blast init --force`), then open two terminals in `/tmp/try`.

```sh
blast dash        # terminal 1
```

| Key | Does |
|---|---|
| `↑` `↓` (or `j` `k`) | move through segments and rules, or through customers |
| `enter` | drill in: segment or rule → its customers → why one pays what they pay |
| `esc` | back out |
| `d` | the pricing settings that differ between the two files |
| `r` | simulate again now |
| `q` | quit |

In terminal 2, make the fix from step 2 in your editor and save. The frame in
terminal 1 turns from red to green without a keypress. Save a typo instead
and the last good result stays on screen with the error under it.

## 4. Your own data

**Pricing** is two YAML files with the same shape: today's and the proposal.

```yaml
plans:
  starter: { monthly: 29, annual: 290 }
  pro:     { monthly: 99, annual: 990 }
  old-pro: { monthly: 79, annual: 790, legacy: true }

discounts:               # applied top to bottom, each off the running total
  WELCOME10: { percent: 10 }                  # for customers holding the coupon
  LOYALTY:   { amount: 5, when: legacy }      # or automatic, by condition
```

**Customers** are JSON or CSV; blast picks by file extension.

```csv
id,plan,billing,coupons,referred,quantity
acme,pro,annual,WELCOME10,yes,25
solo,starter,monthly,,no,
```

`coupons` may hold several, separated by `;`. `referred` and `quantity`
(seats; one when blank) are optional. Run with
`blast check --customers customers.csv`.

**Rules** say what a change must never do:

```yaml
rules:
  - name: max-one-discount
    never: customer.discount_count > 1

  - name: no-big-increase
    never: invoice.change > 50          # dollars per billing cycle

  - name: finance-approval
    require: approval("finance")        # satisfied by: --approve finance
    when: diff.touches("discounts")
```

A condition can read `plan`, `billing`, `referred`, `legacy`, `coupon_count`,
`quantity`, `customer.discount_count`, `invoice.old`, `invoice.new` and
`invoice.change`, joined with `and`, `or`, `not` and comparisons.
[pricing-model.md](pricing-model.md) has the exact arithmetic.

A mistake in any file is reported with its file and line, and exits 2:

```
blast: pricing.new.yml line 14: unknown plan "pro-plus"
```

## 5. Day to day: one pricing file, compared with git

Keep a single `pricing.yml` in your repository and compare your working copy
with `main`:

```sh
blast check --base main --after pricing.yml
```

Save typing with a `blast.yml` next to it. After that, `blast check` and
`blast dash` need no flags:

```yaml
after: pricing/pricing.yml
base: main
customers: pricing/customers.json
rules: pricing/rules.yml
```

Other outputs: `--format md` (a pull-request comment) and `--format json`
(integers only, for scripts). Exit codes are 0 all rules pass, 1 a rule broke,
2 bad input.

## 6. Real customers from Stripe

Follow [stripe.md](stripe.md). In short: create a restricted read-only key in
test mode, then

```sh
read -rs STRIPE_API_KEY && export STRIPE_API_KEY
blast import stripe --out pricing
```

Read the "Left out" list it prints: those subscriptions are not in your
numbers.

## 7. On every pull request

Copy [`examples/github-action/blast.yml`](../examples/github-action/blast.yml)
to `.github/workflows/blast.yml` in the repository that holds your pricing and
adjust the four paths at its top. Each pull request touching `pricing/` then
gets the report as a comment, and a broken rule turns the check red.

## 8. Optional: a plain-words summary

With [Ollama](https://ollama.com) installed and a model pulled
(`ollama pull gemma4`):

```sh
blast check --explain
```

The model runs on your machine and is given totals only, never customers. A
summary that states a number blast did not compute is discarded. Without
Ollama the flag prints one line saying so and the report is unchanged.

## 9. Check it yourself

```sh
make audit     # tests, known-vulnerability scan, static analysis, security linter
make fuzz      # 30 seconds of random input against the rule parser
go test ./cmd/blast -run TestGolden -update   # after an intended change to a report
```

[security.md](security.md) lists what was attacked, what was found, and what
is still unverified.

## 10. Before you publish

Nothing has been pushed, tagged or released. When you are ready:

```sh
# 1. Make the module and the commits yours (the owner is a placeholder).
go mod edit -module github.com/YOU/blast
grep -rl 'github.com/dilaziz/blast' --include='*.go' --include='*.yml' --include='*.yaml' --include='*.md' . \
  | xargs sed -i 's#github.com/dilaziz/blast#github.com/YOU/blast#g'
make test
git commit -am "Set the module path"
git rebase -r --root --exec 'git commit --amend --no-edit --reset-author'

# 2. Push. CI runs on Linux, macOS and Windows.
git remote add origin git@github.com:YOU/blast.git
git push -u origin main

# 3. Release. The tag triggers .github/workflows/release.yml.
git tag v0.1.0
git push origin v0.1.0
```

To rehearse a release without publishing, install GoReleaser and run
`goreleaser release --snapshot --clean`; the binaries land in `dist/`.

Check the name first: `blast` is also NCBI's sequence-search tool, which
ships a `blastn`/`blastp` family of commands. Renaming starts at the `name`
constant in `cmd/blast/cli.go`; `docs/decisions.md` lists the other places.

## Troubleshooting

| You see | Cause |
|---|---|
| `no such file (run "blast init" …)` | You are not in the directory holding the files; pass `--before/--after/--customers/--rules` or add a `blast.yml`. |
| `YAML anchors and aliases are not supported` | Write the value out; blast refuses `&name` / `*name` on purpose. |
| `must be plain, visible text` | A name contains a control or invisible character, often pasted from a spreadsheet. |
| `dash needs a terminal` | You piped or redirected it; use `blast check` in scripts. |
| `cannot read … at revision …` | The file does not exist at that git revision, or the path is not relative to where you ran blast. |
| No colour | Output is piped, or `NO_COLOR` is set. |
