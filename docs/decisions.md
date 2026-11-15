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

## Step 5: rules

- **A rule is `never:` or `require:`, with an optional `when:`.** `never`
  breaks where its condition is true, `require` where it is false, and where
  `when:` is false the rule does not apply.
- **Rules are per customer or about the whole change, decided by what they
  read.** A rule that reads any customer fact is judged once per customer and
  reports who broke it. A rule built only from `approval()` and
  `diff.touches()` is judged once. No keyword is needed to tell them apart.
- **"Not triggered"** means the rule's `when:` held nowhere.
- **`customer.discount_count` and `plan.legacy` describe the new pricing**,
  the side under test. `invoice.old`, `invoice.new` and `invoice.change` are
  dollar amounts.
- **`diff.touches("x")`** is true when the two pricing files differ at the
  dotted path `x` or anywhere under it: `"payment"`, `"plans.pro"`,
  `"discounts.REFERRAL20.percent"`. The `payment` section exists for this:
  blast does not price it, it only notices that it changed.
- **`approval("finance")`** is true when `finance` was passed to
  `blast check --approve`. blast records that someone said so; collecting
  the sign-off (a PR label, a review) belongs to whatever calls it.
- **A rules file may also define `segments:`** (name plus `where:`), replacing
  the four built-in ones. Without it the example-shaped defaults would be the
  only grouping available to every other company.
- **Fuzzing:** `make fuzz`. A 25-second run made 6.9 million attempts
  without a crash.

## Step 6: CLI

- **`blast check` with no flags reads `pricing.old.yml`, `pricing.new.yml`,
  `customers.json` and `rules.yml`** from the current directory, which are the
  names `init` writes. That is what makes `blast init && blast check` work.
- **`init` also writes `pricing.fixed.yml`**, the section 5.7 fix, so the red
  to green demo is two commands. A test proves it is byte-for-byte the
  proposed pricing with that one edit applied.
- **`init` refuses to overwrite** any existing file unless given `--force`,
  and writes nothing when it refuses.
- **Approvals arrive as `--approve finance,legal`.**
- **Colour is decided at the edge.** Renderers always style their output; the
  CLI writes through a colour-profile writer that strips codes for pipes and
  files and honours `NO_COLOR`. `--no-color` forces the same on a terminal.
- **`--format` lists only formats that exist.** It accepts `text` today;
  `json` and `md` join the same table in Step 7.
- **The text report adds a table of changed customers** to the plan's
  section 5.6 layout, largest change first and capped at ten, because "who is
  affected" is the question the tool exists to answer.
- **A crash exits with 2 for free:** that is the Go runtime's exit status for
  a panic, matching "2 = bad input or crash".
- **The wordmark in `internal/report/palette.go` is drawn letter by letter**,
  so it is a fifth place to touch when renaming the tool.

## Real data and hardened inputs (after Step 6)

Asked for after the first milestone: work on real data, and close
vulnerabilities.

- **Customers have an optional `quantity`** (seats). The invoice is the plan
  price times the quantity; a fixed-amount discount still comes off once per
  invoice. Per-seat pricing is too common in real billing data to skip. A
  stated quantity must be at least 1: zero is refused rather than read as one.
- **Customers can be a CSV file** (`--customers customers.csv`), chosen by
  the file extension. Columns are `id, plan, billing` plus optional
  `coupons, referred, quantity`, in any order. Unknown columns are refused,
  as unknown JSON fields are. A byte-order mark is tolerated in CSV and JSON.
- **Names and conditions from input files must be visible text.** They are
  printed on terminals and in pull-request comments, where an escape
  sequence can redraw the screen. The proof-of-concept drew "all rules pass"
  over a failing report. Control and formatting characters are now refused
  at load, with the file and line.
- **YAML anchors and aliases are refused.** A 978-byte pricing file with 30
  nested aliases ran blast out of memory.
- **A file must hold exactly one YAML document.** A second one used to be
  ignored without a word, along with any pricing in it.
- **Conditions are one line.** A folded scalar (`>`) is still the way to
  wrap a long one.
- **Error messages are made visible before they are printed**, as a second
  line of defence behind the load-time checks.
- **Strix was not run.** The installed skills drive the `strix` CLI, which
  needs Docker and a paid LLM key, or uploads the code to Strix's cloud.
  Neither is installed and both go against "free tools, nothing leaves the
  machine". Its method was followed by hand instead: each finding has a
  working exploit, a root-cause fix, and the exploit kept as a test. See
  `docs/security.md`.

## Step 7: reports

- **Three renderers, one `Report`.** `--format text|md|json`.
- **Golden files live in `testdata/`** and pin the real CLI output for the
  example, red and green, in all three formats. Regenerate after an intended
  change with `go test ./cmd/blast -run TestGolden -update`.
- **JSON is versioned (`"version": 1`) and holds integers only.** Money is
  cents; ratios are left for the consumer to compute.
- **JSON lists every notable customer; text and Markdown list ten.** Notable
  means the invoice changed or the customer breaks a rule.
- **A customer who breaks a rule without a price change is still listed.**
  Someone who already held two discounts before the change is the rule's
  business even though their invoice did not move.
- **Markdown treats every name from an input file as hostile.** The comment
  is posted with a bot's authority while the names may come from the pull
  request's author, so they are always inline code: no HTML, no `@mention`,
  no link, no way out of the table cell.
- **Dollar signs are escaped in Markdown**, since some renderers read `$…$`
  as a formula.
- **The Markdown report is capped** (ten customers, forty changed settings)
  to stay inside GitHub's comment size limit.

## Everyday use: `--base` and `blast.yml`

- **`--base <git revision>`** takes today's pricing from git: "before" is
  the `--after` file as that revision has it. A team keeps one pricing file
  and reviews the uncommitted or branch change against `main`, with no
  hand-kept "old" copy. blast runs `git show`; a revision starting with `-`
  is refused so it cannot be read as an option.
- **`blast.yml`** (optional, in the working directory) records `before` or
  `base`, `after`, `customers` and `rules`, so `blast check` needs no flags.
  Flags still win. It holds file locations only, never pricing.
- **`--before` on the command line beats `base` in `blast.yml`**; giving
  both as flags is an error.

## Step 8: dashboard

- **`internal/tui` on Bubble Tea v2**, with Lip Gloss for colour. No
  Bubbles and no chart library: two lists and a table did not need them.
- **One cursor runs through segments and rules** on the left, rather than a
  second key to switch panels. `enter` drills in, `esc` backs out.
- **"all customers" heads the segment list**, so the whole change can be
  browsed, not only its slices.
- **Lists show every member, largest change first**, and scroll. Column
  widths come from the whole list so they hold still while scrolling.
- **In a narrow window the plan column goes first, then the old price.**
  Which rule a customer breaks matters more than either.
- **Live rerun watches each file's directory**, because editors replace
  files rather than write into them. Changes are debounced by 100 ms.
- **A failed rerun keeps the last good result on screen** under its error.
  A half-saved file is normal while editing.
- **The frame is red, green or grey by verdict**, and the verdict is
  highlighted for a moment when it flips.
- **`dash` refuses to start without a terminal** and points to `check`.
- **With `--base`, the "before" side is not watched**: it is a git
  revision, not a file. Press `r` after changing branches.
