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
- **YAML via `go.yaml.in/yaml/v3`**, the YAML organisation's maintained
  continuation of `gopkg.in/yaml.v3`, which is archived. Same API.

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
