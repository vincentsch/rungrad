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

## Output modes

human, json, plain, jq, template

## Metadata

This command can attach request metadata. In advanced-output apps, add `--include-meta` to a supported machine output mode to wrap the result as `{data, meta}`.

## Related commands

- rgref item get
- rgref item create
