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

This command changes things. Add `--dry-run` to see what it would change; nothing changes then.

## Confirmation

This command deletes or overwrites something, so it asks before it acts. Preview it with `--dry-run` first. It doesn't ask when there is no terminal or when you pass `--json` or `--no-prompt`; then it needs its confirmation flag.

## Related commands

- rgref item list
