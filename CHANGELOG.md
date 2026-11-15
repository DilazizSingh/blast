# Changelog

## Unreleased

First version.

- `blast init`, `blast check`: simulate a pricing change against every
  customer and check it against your rules. Exit codes 0, 1 and 2.
- Reports as text, Markdown (`--format md`) and JSON (`--format json`).
- `blast dash`: full-screen dashboard with drill-down and live rerun.
- Customers from JSON or CSV, with an optional per-seat `quantity`.
- `--base <git revision>` and an optional `blast.yml` for defaults.
- `blast import stripe`: read-only import of subscriptions.
- `--explain`: optional summary from an Ollama on the same machine.
- Hardened input handling; see `docs/security.md`.
