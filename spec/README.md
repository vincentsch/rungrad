# The rungrad agent-ready CLI spec

Version: `rungrad-spec/1`

This specification is for command-line tools used by people, scripts, and CI. It
covers CLI behavior that is often left undefined: JSON output, previews before
destructive actions, stable exit codes, names instead of opaque IDs, and help
with examples.

The spec stands on its own. Any CLI in any language can follow it. The rungrad
Go framework is one implementation, and `rungrad score` checks any executable
against the ruleset in [`ruleset.yaml`](ruleset.yaml).

## Sections

1. [Output contract](output-contract.md)
2. [Exit-code model](exit-codes.md)
3. [Dry run](dry-run.md)
4. [Determinism](determinism.md)
5. [Name resolution](name-resolution.md)
6. [Self-describing help](self-describing-help.md)
7. [Self-update](self-update.md)
8. [Auth and config](auth-and-config.md)

## How conformance is scored

Each section lists testable assertions. Every assertion maps to a rule in
`ruleset.yaml` with a stable `id`, a `severity` (`required` or `recommended`),
and a `probe` the conformance runner knows how to execute against a target
executable. A probe returns pass, fail, or not-applicable. The scorer aggregates
results per section and overall, weighting required rules above recommended ones,
and reports the spec version a score was computed against. Rules that a target
cannot be driven to exercise are not-applicable and never count against it.
