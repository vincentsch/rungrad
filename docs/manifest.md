# The rungrad manifest

By default, a tool built with `rungrad.New` exposes a hidden machine endpoint:

```bash
mytool __rungrad_manifest
```

It prints deterministic JSON describing the built Cobra command tree and the
rungrad metadata on each command. The command is hidden from ordinary `--help`,
but it is a stable protocol for agents, tools, and test harnesses that need
structured command metadata.

The manifest command does not require `--json`; JSON is its only output form. It
does not load credentials, initialize the config store, or run adopter command
handlers. It is generated from the same command tree and annotations used by
`--help` and `docsgen`, so help, docs, and the manifest share one source of
truth.

`__rungrad_manifest` is reserved when rungrad registers the default endpoint:
the rungrad-owned mode and the host-rendered default-endpoint mode. Adopters use
`AppConfig.Surface.Manifest` to disable the endpoint or rename it to a
different hidden command. In renamed mode, the renamed endpoint name is reserved
instead and the default `__rungrad_manifest` name is not registered.

## Versioning

The manifest schema version is `rungrad-manifest/1`. It is separate from the
scored CLI spec version, `rungrad-spec/1`.

The top-level `spec_version` field is informational: it records which rungrad
spec version the emitting framework was built against. Consumers use
`schema_version` to decide whether they understand the manifest's JSON shape.

## Manifest fields

| Field | Type | Meaning |
| --- | --- | --- |
| `schema_version` | string | Manifest wire schema version. For this schema, `rungrad-manifest/1`. |
| `spec_version` | string | Informational rungrad spec version, such as `rungrad-spec/1`. |
| `tool_name` | string | Program name from `AppConfig.Name`. |
| `tool_version` | string | Tool version from `AppConfig.Version`, even when Cobra `--version` is host-owned or disabled. |
| `global_flags` | array of flag | Visible root persistent flags, sorted by flag name. |
| `commands` | array of command | Visible non-synthetic command tree, root first, then depth-first with siblings sorted by command name. Hidden commands, Cobra-generated `help`, and framework-generated `completion` commands are excluded. A host-owned visible `completion` command is included. |

Arrays are always JSON arrays. Empty arrays are emitted as `[]`, never `null`.

## Command fields

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | array of string | Executable-relative command path. The root command is `[]`; `mytool item list` is `["item", "list"]`. |
| `use` | string | Cobra `Use` line for the command. |
| `short` | string | One-line command description. |
| `examples` | array of string | Examples from the command metadata or root `Example`. |
| `related` | array of string | Related command paths from rungrad metadata, not parsed from help prose. |
| `output_modes` | array of string | Declared machine/human output modes, such as `human` and `json`. |
| `requires_auth` | bool | Whether rungrad metadata marks the command as requiring a credential. |
| `mutates` | bool | Whether rungrad metadata marks the command as changing state. |
| `supports_dry_run` | bool | Whether the rungrad-emitted manifest says the mutating command honors `--dry-run`. |
| `destructive` | bool | Whether rungrad metadata marks the command as destructive. |
| `requires_confirmation` | bool | Whether the command requires destructive confirmation before acting. |
| `supports_meta` | bool | Whether the command supports request metadata and accepts `--include-meta`. |
| `local_flags` | array of flag | Visible local flags, sorted by name, excluding global flags plus synthetic `help`; synthetic `version` is excluded only when Cobra `--version` is enabled. |
| `extensions` | object | Product-owned namespaced metadata, keyed by `example.com/product`-style namespace. Omitted when no extensions are declared. Generic consumers tolerate unknown namespaces; values cannot reuse core command field names. |

The root command has `path: []`. Its `related` field is `[]` because root related
commands are ordinary help prose, not command metadata. Its `local_flags` field
is usually `[]` because global flags are reported once in `global_flags`.

## Command extensions

Commands may carry product-owned extension metadata under `extensions`. Each
namespace must be lowercase and shaped like `example.com/product`: exactly one
slash, a DNS-like owner on the left, and a product token on the right. Namespaces
starting with `rungrad/` or `rungrad.` are reserved for the framework.

Each namespace value must be a JSON object. Nulls are invalid at every level,
including namespace values and fields. Extension field values may be strings,
booleans, finite numbers, arrays, or objects with string keys; functions,
non-finite numbers, cycles, non-string map keys, and values with custom JSON or
text marshaling are invalid. A field inside a namespace cannot reuse a core
command field name such as `path`, `output_modes`, `supports_dry_run`,
`supports_meta`, or `local_flags`.
Duplicate object keys inside `extensions` are invalid.

Generic consumers ignore unknown valid namespaces. Products own the meaning of
their own fields and validate product invariants in tests. JSON
ordering is deterministic because manifest emission uses stable JSON with sorted
map keys. A malformed extension makes the whole manifest invalid for discovery.

## Flag fields

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Long flag name without `--`. |
| `shorthand` | string | Short flag name without `-`, or an empty string. |
| `usage` | string | Flag help text. |
| `default` | string | Cobra default value. |
| `type` | string | Cobra/pflag value type, such as `bool` or `string`. |
| `required` | bool | Whether Cobra marks the flag required. |

## Example

```bash
$ rgref __rungrad_manifest
{
  "schema_version": "rungrad-manifest/1",
  "spec_version": "rungrad-spec/1",
  "tool_name": "rgref",
  "tool_version": "v0.1.0",
  "global_flags": [
    {
      "name": "config",
      "shorthand": "",
      "usage": "Path to the config file",
      "default": "",
      "type": "string",
      "required": false
    }
  ],
  "commands": [
    {
      "path": [],
      "use": "rgref",
      "short": "rungrad reference CLI",
      "examples": [
        "rgref item list"
      ],
      "related": [],
      "output_modes": [],
      "requires_auth": false,
      "mutates": false,
      "supports_dry_run": false,
      "destructive": false,
      "requires_confirmation": false,
      "supports_meta": false,
      "local_flags": []
    }
  ]
}
```

The example is shortened; rungrad tools include every visible global flag
and every visible non-synthetic command.
