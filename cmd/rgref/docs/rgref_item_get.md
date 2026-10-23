# rgref item get

Resolve an item by name or id

## Usage

```
rgref item get <name>
```

## Examples

```
rgref item get alpha
rgref item get 1
rgref item get alpha --jq .id
rgref item get alpha --template '{{.id}}'
```

## Output

Shows text you can read in your terminal.

Use --json to get JSON for scripts and other programs.

Use --jq to filter the JSON output with a jq expression.

Use --template to format the output with a Go template.

## Related commands

- rgref item list
