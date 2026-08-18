# Auth and config

## Criterion

A tool loads credentials with a documented precedence, never prints a secret, and
lets the config location be set explicitly.

## Rationale

Automation supplies credentials through the environment and needs a predictable
precedence. Secrets must never leak into logs, output, or previews, because those
are captured and shared. An explicit config path lets isolated environments and
tests avoid touching a shared default.

## Precedence

A credential is resolved as: an environment variable, then the stored credential
for the active profile, then an error. Configuration files are located under the
user config directory by default and overridden with `--config`.

## Testable assertions

- `auth.secret-not-printed` (required): a command that handles a credential never
  prints the secret value to standard output or standard error.
- `auth.config-flag` (recommended): a read command honors `--config` pointing at
  an alternate location.
