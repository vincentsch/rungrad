# Exit-code model

## Criterion

A tool returns a stable set of exit codes that classify the outcome, so callers
branch on the result without parsing text.

## Rationale

Scripts and agents decide what to do next based on whether a command succeeded,
was used incorrectly, hit an auth problem, or asked for something that does not
exist. Collapsing every failure into exit 1 forces text scraping and makes
automation brittle.

## Codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | usage error: unknown command or flag, ambiguous name, failed validation |
| 2 | upstream or runtime error the user did not cause |
| 3 | missing or invalid credentials |
| 4 | authenticated but not permitted |
| 5 | the requested resource does not exist |
| 6 | throttled by an upstream service |

## Testable assertions

- `exit.success-zero` (required): a successful read command exits 0.
- `exit.unknown-usage` (required): an unknown subcommand exits 1.
- `exit.missing-credential-auth` (required): a command that needs a credential,
  run with none available, exits 3.
- `exit.not-found` (recommended): asking for a resource that does not exist exits
  5.
- `exit.api-error` (recommended): a command that hits an upstream or runtime error
  the user did not cause exits 2.
- `exit.forbidden` (recommended): a command refused because the caller is
  authenticated but not permitted exits 4.
- `exit.rate-limited` (recommended): a command throttled by an upstream service
  exits 6.
