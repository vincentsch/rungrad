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

## Output modes

human, json, jq, template

## Related commands

- rgref item list
