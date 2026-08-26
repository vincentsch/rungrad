# Agent distribution metadata

rungrad can project a typed `rungrad-manifest/1` document into an
`agentmeta.Document`:

```go
m, err := app.ManifestDocumentChecked()
if err != nil {
    return err
}
doc, err := agentmeta.FromManifest(m)
if err != nil {
    return err
}
```

The projection is pure. It validates the manifest, checks the rungrad spec
version, preserves command order, and derives command templates from the
manifest fields. It does not execute commands, call Cobra handlers, read config
or credential files, inspect environment variables, contact networks, or import
an MCP SDK.

## Terms

A repository-scoped skill is a `SKILL.md` file under
`.agents/skills/<name>/`. Product repositories own that file and decide which
manifest fields are allowed to appear in it.

An agent metadata document is the `agentmeta.Document` returned by
`agentmeta.FromManifest`. It is an inventory for commands, visible flags,
examples, output modes, mutation metadata, request-metadata support, and
namespaced manifest extensions.

A local stdio MCP adapter is product-owned local process code. It can read the
agent metadata document as inventory, but it owns runtime command execution,
argument handling, auth, product API calls, permission checks, and MCP protocol
behavior.

A hosted MCP provider is product or backend code that exposes a hosted
integration. It owns OAuth, account binding, authorization, product API clients,
hosting, and provider submission.

A machine usage template is non-executing command guidance. It starts with the
tool name and manifest command path, appends positional tokens parsed from
`use`, and adds canonical visible machine flags only when they can be identified
from the manifest.

A host-owned surface is an `AppConfig.Surface` setup that renames, hides, or
disables framework global flags. When `--json`, `--no-prompt`, or `--dry-run`
are renamed, hidden, disabled, differently typed, or have a non-`false` default,
`agentmeta` treats their canonical semantics as absent.

## Scaffolded repository skills

The product scaffold can generate a conservative repository-scoped skill:

```bash
rungrad new acmectl --product-profile --skill
```

That flag adds:

- `.agents/skills/acmectl/SKILL.md`
- `.agents/README.md`

The skill directory name matches the `SKILL.md` frontmatter `name`. Agents that
scan repository skills can discover the file from the repository root or from a
child working directory inside the repository.

The generated skill is built from a validated scaffold-time manifest projected
through `agentmeta.FromManifest`. It renders only an allowlist: the validated tool
name, generated command templates, read/mutation/destructive capability guidance,
the scaffold's read-only `update --check` pattern, and fixed reviewed prose. It
does not render product descriptions, service URLs, config paths, auth paths,
arbitrary examples, extension values, direct API instructions, or provider
claims.

Product maintainers should review the generated text and keep it aligned with
their command surface. rungrad intentionally does not generate `.codex-plugin/`,
`.mcp.json`, Claude plugin files, MCP server code, hosted-tool descriptors, or
marketplace metadata.

## What the projection includes

Each command keeps the manifest path, `use`, short text, examples, related
commands, output modes, auth/mutation/destructive metadata, metadata support,
local flags, required local flags, and namespaced extensions. Extensions are
preserved under their original namespaces. Generic consumers should tolerate
unknown namespaces.

`CommandTemplate` is the tool name plus command path with no positional
operands. `UsageTemplate` adds the remaining `use` fields after validating that
the first `use` field matches the tool name for root or the final path segment
for non-root commands.

`MachineUsageTemplate` is emitted only when the command declares `json` output,
the canonical visible `--json` and `--no-prompt` boolean globals are present,
and the command is not mutating or destructive.

`DryRunUsageTemplate` is emitted only when the command declares dry-run support
and the canonical visible `--dry-run` boolean global is present. It adds
`--json --no-prompt` only when JSON output and both canonical machine globals
are visible.

Required local flags stay in `RequiredLocalFlags`; templates do not pretend that
callers have supplied product-specific values. Destructive commands never get a
confirmed execution template. When a command requires confirmation, the document
contains a note telling consumers to require explicit user confirmation and
inspect product documentation for the confirmation mechanism.

## Boundary

The manifest does not expose typed positional inputs, typed output schemas, auth
policy, authorization policy, or complete execution semantics. Because of that,
`agentmeta` supports inventory, repository-scoped skill material, and manual
adapter curation. It does not support mechanical typed-MCP generation.

The manifest may contain product-supplied strings. rungrad cannot prove that
those strings are secret-free. Any generated skill, adapter config, or provider
artifact should use an allowlist before rendering manifest text.

## Not in rungrad

- MCP runtime
- MCP SDK dependency
- OAuth flow
- Hosted service
- Product API client
- Permissions layer
- Provider marketplace packaging
- Provider submission workflow
