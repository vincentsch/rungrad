# Self-describing help

## Criterion

Every command's `--help` includes examples and related commands, so the surface
can be learned from the tool itself.

## Rationale

An agent discovers the command surface by reading help, not external docs.
Examples show the shape of an invocation, and related-command links let the
agent navigate without guessing names.

## Testable assertions

- `help.examples` (required): a command's `--help` output contains an examples
  section.
- `help.related` (recommended): a command's `--help` output points to related
  commands.
