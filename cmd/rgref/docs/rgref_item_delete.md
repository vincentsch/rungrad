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

## Output

Shows text you can read in your terminal.

Use --json to get JSON for scripts and other programs.

## Preview changes

This command makes changes. Use `--dry-run` to see what would change without changing anything.

## Destructive

This command performs a destructive action and asks for confirmation before acting. Preview it first with `--dry-run`; outside a dry run it proceeds only after explicit confirmation, and in non-interactive mode (`--json`, `--no-prompt`, or no terminal) it requires a confirmation flag instead of blocking.

## Related commands

- rgref item list
