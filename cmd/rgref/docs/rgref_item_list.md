# rgref item list

List items

## Usage

```
rgref item list
```

## Examples

```
rgref item list
rgref item list --json
rgref item list --plain
rgref item list --jq '.[].name'
rgref item list --template '{{range .}}{{.id}} {{.name}}{{"\n"}}{{end}}'
```

## Output

Shows text you can read in your terminal.

Use --json to get JSON for scripts and other programs.

Use --plain to get text without color or formatting.

Use --jq to filter the JSON output with a jq expression.

Use --template to format the output with a Go template.

## Request details

Add `--include-meta` to the JSON output to get the result as `{data, meta}`, where `meta` holds details about the request.

## Related commands

- rgref item get
- rgref item create
