# Determinism

## Criterion

Given the same input and state, a tool produces the same output: stable field
names, stable ordering, and no incidental noise such as timestamps or random
identifiers in default output.

## Rationale

Determinism is what makes output diffable and cacheable. An agent that compares
two runs, or a CI job that snapshots output, depends on the same result encoding
the same way every time.

## Testable assertions

- `determinism.repeatable` (required): running the same read command twice with
  `--json` produces byte-identical output.
- `determinism.stable-sort` (recommended): a list command returns rows in the
  same order across runs.
