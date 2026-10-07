# The rungrad agent-ready CLI spec

Version: `rungrad-spec/1`

This specification is for command-line tools used by people, scripts, and CI. It
covers CLI behavior that is often left undefined: JSON output, previews before
destructive actions, stable exit codes, names instead of opaque IDs, and help
with examples.

The spec stands on its own. Any CLI in any language can follow it. The rungrad
Go framework is one implementation, and `rungrad score` checks any executable
against the ruleset in [`ruleset.yaml`](ruleset.yaml).

## Why this spec exists

A script needs to know whether a command failed. It needs data it can parse and
must not wait for someone to answer a prompt. An AI agent calling the same
command needs those things too.

A CLI framework can parse flags without defining any of that behavior. This
spec gives CLI authors a shared set of rules for output, errors and changes.
It also gives users something to check before depending on a tool in a script.

## How to use it

When building a CLI, use the sections below to decide how commands should
behave. For example, a delete command should show a preview under `--dry-run`
and refuse to prompt when called with `--no-prompt`. The rules apply whether
you use rungrad, another framework, or a language other than Go.

For an existing CLI, install the scorer and name a command it can read:

```bash
go install github.com/vincentsch/rungrad/cmd/rungrad@v0.3.2
rungrad score ./mytool --read "project list"
```

Replace `project list` with a read-only command from your tool. Add fixtures
for its other behaviors as you implement them. You can run the same checks in
CI with `--strict` to fail when a required rule fails. Unconfigured checks are
not-applicable, so a high score alone does not mean the whole spec was checked.
See [conformance](../docs/conformance.md) for fixture flags and scoring limits.

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
