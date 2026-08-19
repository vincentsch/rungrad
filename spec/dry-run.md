# Dry run

## Criterion

A command that changes state accepts `--dry-run` and, under it, reports exactly
what it would do without performing the change.

## Rationale

An agent or operator needs to preview a destructive or expensive action before
committing to it. A dry run shows the planned change and lets automation gate on
the preview. For destructive actions, the tool performs the change only after
explicit confirmation, and refuses instead of blocking when confirmation is not
available non-interactively.

## Testable assertions

- `dryrun.accepted` (required): a mutating command accepts `--dry-run` and exits
  0.
- `dryrun.no-side-effects` (required): under `--dry-run` the command emits a
  preview and states that no change was made, rather than reporting a completed
  mutation.
- `dryrun.destructive-preview` (required): a destructive command under
  `--dry-run` previews the action without requiring confirmation and without
  performing it.
- `dryrun.destructive-confirm-required` (required): outside `--dry-run`, a
  destructive command performs the action only after explicit confirmation, and in
  non-interactive mode (`--json`, `--no-prompt`, or no terminal) it refuses and
  exits 1 when confirmation is absent.
