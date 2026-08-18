# rgref command reference

rungrad reference CLI

## Commands

- [rgref item](rgref_item.md) - Work with items
- [rgref item create](rgref_item_create.md) - Create an item
- [rgref item delete](rgref_item_delete.md) - Delete an item
- [rgref item get](rgref_item_get.md) - Resolve an item by name or id
- [rgref item list](rgref_item_list.md) - List items
- [rgref update](rgref_update.md) - Check for and install the latest version
- [rgref whoami](rgref_whoami.md) - Show the authenticated identity

## Global flags

- `--api-url` Base URL for the reference API service
- `--auth-file` Path to the credentials file
- `--config` Path to the config file
- `--dry-run` Preview changes without performing them
- `--include-meta` Wrap machine output as {data, meta} (commands that expose request metadata)
- `--jq` Transform stable JSON output with a jq expression (commands with machine output)
- `--json` Output stable JSON instead of the human view
- `--no-ansi` Disable all ANSI/control sequences in human output
- `--no-color` Disable color in human output
- `--no-pager` Never use a pager for long human output
- `--no-prompt` Never block on an interactive prompt
- `--plain` Print unstyled, copy-safe text (commands with human output)
- `--profile` Profile to use for config and credentials
- `--quiet` Suppress non-essential output
- `--template` Render stable JSON output through a Go text/template (commands with machine output)
