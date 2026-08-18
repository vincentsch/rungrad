# rgref item delete

Delete an item

## Usage

```
rgref item delete <name>
```

## Examples

```
rgref item delete alpha --dry-run
rgref item delete alpha --confirm
```

## Flags

- `--confirm` Confirm the destructive action without a prompt

## Output modes

human, json

## Changes state

This command changes state and honors `--dry-run`.

## Destructive

This command performs a destructive action and asks for confirmation before acting. Preview it first with `--dry-run`; outside a dry run it proceeds only after explicit confirmation, and in non-interactive mode (`--json`, `--no-prompt`, or no terminal) it requires a confirmation flag instead of blocking.

## Related commands

- rgref item list
