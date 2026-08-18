# Output contract

## Criterion

A tool produces a human-readable view by default and a stable machine view on
request. The machine view is selected with `--json` and is the same data the
human view is built from.

## Rationale

An automated caller cannot reliably scrape aligned columns or prose. It needs a
parseable structure with stable field names. Building both views from one model
keeps them from drifting, so what a human sees and what a script parses describe
the same result.

## Testable assertions

- `output.json-parseable` (required): running a read command with `--json`
  writes valid JSON to standard output and nothing that breaks parsing.
- `output.dual-form` (recommended): the same read command produces output both
  with and without `--json`, and the `--json` form parses.
