# Output contract

## Criterion

A tool produces text by default and a JSON view on request. `--json` selects the
JSON view, which uses the same data as the text view.

## Rationale

A script cannot parse aligned columns or prose without brittle rules. It needs
JSON with stable field names. Building both views from one model keeps them
aligned: what a person sees and what a script reads describe the same result.

## Testable assertions

- `output.json-parseable` (required): running a read command with `--json`
  writes valid JSON to standard output and nothing that breaks parsing.
- `output.dual-form` (recommended): the same read command produces output both
  with and without `--json`, and the `--json` form parses.
