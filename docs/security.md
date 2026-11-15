# Security notes

## What blast trusts

blast reads files and, on request, two network sources. None of them is
trusted:

| Input | Who may control it | Where it ends up |
|---|---|---|
| pricing, rules, customers, `blast.yml` | anyone who can open a pull request | terminal, PR comment, JSON |
| Stripe API responses | Stripe; product and coupon names are typed by people | the files blast writes |
| text from a local model (`--explain`) | the model | terminal |

The outputs matter because a report is a decision aid. An attacker's goal is
a report that looks green when it is red, or a tool that falls over in CI.

## Findings

Each was demonstrated with a working exploit against the build of the time,
fixed at its cause, and the exploit kept as a regression test.

| # | Finding | Severity | Fix | Test |
|---|---|---|---|---|
| 1 | **Terminal escape injection.** A customer id containing cursor-movement codes erased the real verdict and drew "all rules pass". | High | Names and conditions must be visible text; refused at load with file and line. Error text is sanitised again before printing. | `TestInputThatCouldRepaintATerminalIsRefused`, `TestCraftedInputCannotReachTheTerminal` |
| 2 | **YAML alias bomb.** A 978-byte pricing file with 30 nested aliases exhausted memory. | High (denial of service in CI) | Anchors and aliases are refused. | `TestAliasBombIsRefused` |
| 3 | **Second YAML document ignored.** Pricing after a `---` was dropped without a word. | Medium (misleading result) | Exactly one document per file. | `TestSecondDocumentIsRefused` |

Designed against from the start, each with a test:

- **Markdown injection in PR comments.** Every value from an input file is
  inline code: no HTML, link, `@mention` or escape from its table cell.
- **Git option injection** through `--base`: a revision starting with `-` is
  refused, and git is run without a shell.
- **Stripe key handling**: environment only, never printed, sent to one
  fixed host, redirects not followed, `sk_live_` refused, `GET` only.
- **Runaway responses**: size, time and page limits on every network read.
- **Generated text**: model output is stripped of control characters, and
  withheld if it states a number blast did not compute.
- **Integer overflow**: prices and quantities are bounded so no product can
  wrap.

## Scanners

Run them all with `make audit`. Results when this was written:

| Tool | Result |
|---|---|
| `govulncheck` v1.9.0 | no known vulnerabilities |
| `staticcheck` v0.8.2 | clean, after five style fixes |
| `gosec` v2.30.0 | clean. Eight reports reviewed: two ignored errors fixed; six are the tool doing its job (reading the files you name, running `git show`, creating ordinary project files) and are annotated `#nosec` with the reason at each line |
| rule-parser fuzzing | no crash; rerun with `make fuzz` |

## Not verified

Say so rather than imply otherwise:

- **Strix was not run.** It needs Docker and a paid model key, or uploads
  the code to its cloud. Its method (white box, exploit, fix, retest) was
  followed by hand. To run it yourself:
  `strix -n -t ./ --scan-mode standard --max-budget 15`
- **The Stripe import has never touched the real API.** No real key was
  used, by design. It is tested against a local stand-in built from Stripe's
  documentation. Use test mode first.
- **`--explain` has not been run against a real Ollama**, only a stand-in.
- **The GitHub workflows have not run on GitHub.** They pass `actionlint`.
- **Windows and macOS** are covered by CI configuration, not by a run.

## Known limits

- The `--explain` number check knows which numbers were computed, not what
  each means, and cannot see numbers written as words. Hence the label.
- A pull request can edit `rules.yml` itself. Protect it with CODEOWNERS.
- `blast.yml` and the flags can name any readable file. blast only reads
  them, and prints at most a parse error.

## Reporting a problem

Open a private security advisory on the repository rather than a public
issue.
