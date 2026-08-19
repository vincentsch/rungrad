# Auth and config

## Criterion

A tool loads credentials with a documented precedence, never prints a secret, and
lets the config location be set explicitly.

## Rationale

Scripts and CI pass credentials through environment variables. A tool documents
which credential source wins when more than one is present. Secrets must not
appear in logs, output, or previews because they get saved and shared. An
explicit config path lets tests use a temporary config instead of touching a
user's files.

## Precedence

A tool resolves a credential in this order: environment variable, then the stored
credential for the active profile, then an error. Configuration files are stored
under the user config directory by default; `--config` overrides that path.

## Testable assertions

- `auth.secret-not-printed` (required): a command that handles a credential never
  prints the secret value to standard output or standard error.
- `auth.config-flag` (recommended): a read command honors `--config` pointing at
  an alternate location.
