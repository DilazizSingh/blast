# Decisions

Choices made where the plan left room. Newest step last. Each entry says what
was chosen and why, so it can be revisited.

## Step 0: setup

- **The name is a variable.** `blast` appears in four places: the module path
  in `go.mod`, the `cmd/blast` directory, the `name` constant in
  `cmd/blast/cli.go`, and `BINARY` in the `Makefile`. To rename:
  `git mv cmd/blast cmd/<new>`, change those three values, then
  `grep -rl dilaziz/blast . | xargs sed -i 's#dilaziz/blast#<owner>/<new>#g'`.
- **Name check, as far as it could be run here.** No `blast` binary is on this
  machine's `PATH` and the Arch/CachyOS repos have no `blast` package. `brew`
  and `gh` are not installed, so Homebrew and GitHub were not searched. The
  known collision stands: NCBI BLAST+ (bioinformatics) ships as the Homebrew
  core formula `blast`, with binaries `blastn`, `blastp` and friends. A tap
  formula `<you>/tap/blast` would install but shadow or conflict with it.
- **Module path `github.com/dilaziz/blast`** is a placeholder built from the
  local user name. Replace the owner with the real GitHub account before the
  first push.
- **MIT licence**, copyright "The blast authors".
- **Standard `flag` package, not cobra.** Five commands do not need a
  framework, and it keeps the dependency list short. The plan allows either.
- **YAML via `go.yaml.in/yaml/v4`**, the YAML organisation's maintained
  successor to `gopkg.in/yaml.v3`, which is archived. v4 was chosen over the
  v3 line because its errors carry the exact line of a syntax problem; v3
  reports the start of the enclosing block, off by one.

## Step 1: model

- **Percent discounts are stored as basis points** (`20%` is `2000`), not the
  plan's `Percent int`. Stripe coupons allow `12.5%`, and one fixed-point
  parser (`model.ParseHundredths`) then serves dollars-to-cents, percents and
  the numbers in the rule language. For whole percents the arithmetic is
  identical to the plan's formula.
- **A plan's prices are a map keyed by billing period**, not two fields. A
  missing period means "not sold", which a zero in a struct field could not
  tell apart from a free plan.
- **Amounts are capped at $1 billion** so percent math cannot overflow.

## Step 2: loading

- **Unknown sections and fields are errors**, in pricing files and in
  `customers.json`. A misspelt `discount:` or `"refered"` would otherwise
  load as "nothing" and quietly misprice people.
- **A pricing file has three sections:** `plans`, `discounts` and an optional
  free-form `payment`. blast does not interpret `payment`; it is flattened to
  dotted paths so rules can ask whether it changed (see Step 5).
- **A customer's plan must exist, with a price for their billing period, in
  both files.** A change that deletes a plan people are still on cannot be
  priced, so it is reported as an input error rather than guessed at.
- **A customer's coupon only has to exist in one of the two files.** Adding
  or retiring a coupon is exactly the kind of change being simulated; a
  coupon neither file knows is a typo.
- **A customer may not list the same coupon twice.** That is bad data, not a
  second discount.
- **Two error shapes**, both from the plan: `file line N: message` when the
  line is known, `file:line:col message` when the character is too.
- **CSV customers are not in yet.** The plan marks it "add later"; JSON is
  what `init` and the Stripe import produce.

## Step 3: invoice engine

- **The expression language lives in `internal/rules/expr`**, a leaf package
  under `rules`. `sim` needs it for `when:` and `rules` needs `diff`, which
  needs `sim`; one package for both would be an import cycle. `rules` itself
  (Step 5) holds what a rule is and how it is checked.
- **The language core was built here, not in Step 5**, because pricing the
  ten example customers needs `when:` evaluated.
- **Expressions are type-checked when compiled.** Every variable has a known
  type, so `refered` or `billing > 1` is an error at load time with a column,
  and evaluation cannot fail halfway through a customer list.
- **Numbers in expressions are fixed-point hundredths.** `712.80` compares
  exactly against cents and `2` against a count, with one integer type and no
  floats. More than two decimal places is an error.
- **Text is double-quoted, with no escape sequences.** Plan and coupon names
  do not need them.
- **A discount applies if the customer holds the coupon or its `when:` is
  true**, exactly as the plan's Step 3 words it. A held coupon applies even
  where `when:` is false.
- **Stacking order is the pricing file's declaration order**, for coupons and
  automatic discounts alike. One rule, controlled in one place.
- **Rounding is half up after every discount**, per the plan's formula.
- **Fixed amounts are per invoice and stop at zero.**
- **Expressions are capped at 4,096 characters**, which bounds parser
  recursion without tracking depth.
- `Engine.Invoice(customer)` replaces the plan's `Invoice(c, cfg)` sketch so
  conditions compile once per config rather than once per customer.

## Step 4: diff

- **"Affected" means the invoice total changed.** A customer whose discounts
  were swapped for an equal amount is not counted.
- **A segment is a name plus a condition** in the expression language, so the
  four built-in ones from the plan (`annual + referred`, `monthly`,
  `legacy plans`, `has coupon`) are four lines of data. Segments may overlap.
- **Plan facts (`legacy`) come from the new config**, the one under test.
- **The config diff is a list of dotted paths** with old and new values
  (`discounts.REFERRAL20.percent`). Reordering discounts changes stacking
  without changing any setting, so it is reported as `discounts.order`.
- **Changed customers are listed largest change first.** With thousands of
  customers, the biggest movers are the ones worth reading.
- **No progress bar.** Pricing is two integer passes per customer and finishes
  before a bar could draw.
