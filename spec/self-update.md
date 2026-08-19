# Self-update

## Criterion

A tool reports whether a newer version is available without changing anything,
and that check has a JSON form.

## Rationale

Agents and CI need a read-only answer to "is this current?" separate from
installing a release. A JSON form lets automation gate on the answer.

## Testable assertions

- `update.check-readonly` (required): `update --check` reports status and exits 0
  without modifying the binary.
- `update.check-json` (recommended): `update --check --json` emits valid JSON.
