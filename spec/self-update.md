# Self-update

## Criterion

A tool can report whether a newer version is available without changing anything,
and that check has a machine-readable form.

## Rationale

Agents and CI should be able to ask "is this current?" as a safe, read-only
question, separate from the act of installing. A JSON form lets automation gate
on the answer.

## Testable assertions

- `update.check-readonly` (required): `update --check` reports status and exits 0
  without modifying the binary.
- `update.check-json` (recommended): `update --check --json` emits valid JSON.
