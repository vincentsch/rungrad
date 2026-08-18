# Self-describing help

## Criterion

Every command's `--help` carries concrete examples and points to related
commands, so the surface can be learned from the tool itself.

## Rationale

An agent discovers what a tool can do by reading its help, not external docs.
Examples show the real shape of an invocation, and related-command links let the
agent navigate the surface without guessing names.

## Testable assertions

- `help.examples` (required): a command's `--help` output contains an examples
  section.
- `help.related` (recommended): a command's `--help` output points to related
  commands.
