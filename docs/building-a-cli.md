# Building a CLI with rungrad

Start with `rungrad new mytool` (see [Getting started](getting-started.md)). This
guide explains the generated code and the APIs used to turn the starter into
your own tool. Your command does the work. rungrad handles text and JSON output,
dry-run previews, confirmation before destructive actions, exit codes, help,
generated docs, the hidden manifest, and scoring.

rungrad handles output modes, terminal and pager controls, redaction, feature
modules, catalog validation, docs/help checks, manifest output,
config/auth/service hooks, browser-opening helpers, test helpers, and scoring.
Your CLI owns API calls, command behavior, service URLs, login flow, workspace
rules, and extra secret values unless it registers them with rungrad.

To port an existing Cobra CLI, follow the ordered path in
[Migrating from Cobra](migrating-from-cobra.md). This guide documents the APIs
used there.

## The App

A tool constructs an `App`, registers commands, and calls `Run`.

```go
app := rungrad.New(rungrad.AppConfig{
    Name:    "mytool",
    Short:   "mytool CLI",
    Long:    "Longer description shown on the root --help.",
    Version: "v0.1.0",
    EnvVar:  "MYTOOL_TOKEN", // credential environment variable
})
app.AddCommand(/* commands */)
os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr))
```

In the default rungrad-owned surface mode (`Surface.GlobalFlags` unset or
`SurfaceRungradOwned`), `New` registers the five persistent global flags
(`--json`, `--dry-run`, `--no-prompt`, `--quiet`, `--config`), enables Cobra
`--version` when `AppConfig.Version` is set, leaves Cobra's generated
`completion` command enabled, registers the hidden `__rungrad_manifest`
endpoint, and installs the validate-then-auth pre-run hook. Set
`AppConfig{AdvancedOutput: true}` to also register `--plain`, `--jq`,
`--template`, `--include-meta`, `--no-color`, `--no-ansi`, and `--no-pager` and
install the output-mode guard. Set `AppConfig.Resolution` to add profile,
auth-file, and service-endpoint globals. The reference CLI opts into advanced
output and resolution in
[`cmd/rgref/main.go`](../cmd/rgref/main.go).

Existing product CLIs customize those public framework surfaces through
`AppConfig.Surface`; see [Framework surface customization](#framework-surface-customization).
`Run` executes the tool, prints any error in the active output mode, and returns
the classified process exit code. `RunIO` is the same with an explicit input
reader, which the test harness uses to drive prompts.

## Commands

A `Command` is a thin builder over Cobra that records the metadata rungrad needs
for help, docs, and conformance:

```go
&rungrad.Command{
    Use:         "list",
    Short:       "List widgets",
    Examples:    []string{"mytool widget list", "mytool widget list --json"},
    Related:     []string{"mytool widget create"},
    OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
    Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
        return f.WriteResult(data, func(w io.Writer) { /* human view */ })
    },
}
```

Fields:

- `Examples` and `Related` populate the examples and related-commands sections of
  `--help`, which the spec requires for self-describing help.
- `OutputModes` documents the supported forms for docs and conformance.
- `Mutates: true` marks a state-changing command that should honor `--dry-run`.
- `Destructive: true` marks a destructive command. It implies `Mutates` and is
  expected to gate the action behind `f.ConfirmDestructive` (see Dry run).
- `RequiresAuth: true` makes the validate-then-auth pre-run hook load a credential
  before the command runs, failing with the auth exit code when none is available.
- `Extensions` attaches product-owned command metadata to the manifest under
  namespaced keys such as `example.com/product`.
- `GroupID` sorts the command under a named help group registered with
  `app.AddGroup`.
- `Configure func(*cobra.Command)` registers local flags.
- `Args` is a Cobra positional-args validator.

Use `cmd.AddCommand(sub...)` to build a parent command with subcommands.

## Feature modules and command catalog

A large CLI can split commands across compiled-in feature modules. A module is an
ordinary Go value registered explicitly at startup. It implements the
`rungrad.FeatureModule` interface:

```go
type FeatureModule interface {
    Groups() []Group
    Commands() []*Command
    Catalog() []CommandSpec
}
```

A module looks like this:

```go
type WidgetModule struct{}

func (WidgetModule) Groups() []rungrad.Group {
    return []rungrad.Group{{ID: "widgets", Title: "Widgets:"}}
}

func (WidgetModule) Commands() []*rungrad.Command {
    return []*rungrad.Command{{
        Use:     "widget",
        Short:   "Work with widgets",
        GroupID: "widgets",
    }}
}

func (WidgetModule) Catalog() []rungrad.CommandSpec {
    return []rungrad.CommandSpec{{
        Path:    "widget",
        Summary: "Work with widgets",
        GroupID: "widgets",
    }}
}

app.AddModule(WidgetModule{})
```

`App.AddModule` registers module groups through the same path as `App.AddGroup`,
registers the module's top-level commands through `App.AddCommand`, and stores a
deep copy of the module catalog. Re-registering the same help group
`ID` with the same title is ignored so modules can share groups. Reusing a group
`ID` with a different title panics. `help` is always a reserved command name.
`completion` is reserved unless `AppConfig.Surface.Completion` is host-owned.
`__rungrad_manifest` is reserved only when rungrad registers the default
manifest endpoint; a renamed endpoint reserves its configured name instead.
Reserved names are rejected anywhere in an added command tree because they are
internal or intentionally absent from the ordinary command surface.

`CommandSpec` is deliberately independent of `Command`: it restates the
docs-facing command contract, including `Path`, `Summary`, `GroupID`,
`OutputModes`, `Examples`, `Related`, `RequiresAuth`, `Mutates`, `Destructive`,
`SupportsMeta`, and `Extensions`. `CommandSpec.Extensions` mirrors
`Command.Extensions`; `ValidateCatalog` canonical-encodes both sides and reports
drift when product metadata no longer matches the built command. Use
`testutil.AssertConsistent` from a unit test to catch catalog, help, docs, and
manifest drift:

```go
func TestCommandCatalog(t *testing.T) {
    testutil.AssertConsistent(t, newApp)
}
```

The consistency helper compares the catalog to the built Cobra tree through the
same metadata projection used by the manifest and docs generator. It also checks
that manifest-declared examples, related commands, output modes, authentication,
mutation/destructive markers, and metadata support are present in help and
generated docs. When the catalog includes extensions, the same path checks
extension drift. That keeps the catalog, `__rungrad_manifest`, help, and the
generated command reference on one command surface.

For a compiled-module reference, see [`cmd/rgref/main.go`](../cmd/rgref/main.go):
`itemsModule` declares the `core` help group and item commands,
`identityModule` registers `whoami`, and `updateSpecModule` supplies the catalog
row for the framework-built `update` command. The seven-row reference catalog is
gated by `TestReferenceCatalogCoversVisibleCommands` in
[`cmd/rgref/catalog_test.go`](../cmd/rgref/catalog_test.go) and by
`TestHelpDocsManifestConsistent` in
[`cmd/rgref/docs_test.go`](../cmd/rgref/docs_test.go).

`ValidateCatalog` is a whole-surface gate: every visible command needs a
`CommandSpec`. Specs are contributed through `AddModule`; if you also register a
command directly with `AddCommand`, cover it with a spec-only module or skip
catalog validation. `CommandSpec.Examples` uses the same line-oriented shape as
the manifest, so provide one example per slice element rather than one string
containing newlines.

Runtime plugins are out of scope. Feature modules are compiled into the binary
and registered explicitly; rungrad never discovers or loads modules at runtime.

## Framework surface customization

`AppConfig.Surface` controls which public surfaces rungrad owns, which the host
owns, and which are disabled. The zero value preserves the default rungrad-owned
behavior.

`SurfaceMode` has three values:

- `SurfaceRungradOwned` (`"rungrad"`) keeps the default framework surface.
- `SurfaceHostOwned` (`"host"`) suppresses rungrad's public surface where
  applicable so the product can provide its own contract.
- `SurfaceDisabled` (`"disabled"`) omits that surface.

Global flags are all-or-nothing through `Surface.GlobalFlags`. In
rungrad-owned mode, rungrad registers its flags. In disabled mode, it
registers none of them; product-local flags named `--json`, `--jq`, or
`--template` do not trigger rungrad machine-mode error rendering. In host-owned
mode, provide a `GlobalFlagBindings` entry for every rungrad-recognized flag
that is active for the app. The binding names may be renamed or hidden, and they
still drive rungrad output modes, config/profile/auth-file resolution, service
overrides, terminal/pager controls, prompts, and machine-mode error rendering.
Bindings for `json`, `jq`, and `template` cannot define shorthands because the
early raw-argument machine detector tracks long names only. Hidden host bindings
work at runtime but are omitted from the manifest and generated docs.

Advanced embedders call `app.BindGlobalFlags(fs, bindings)` to register the
same host-owned binding logic on a chosen `*pflag.FlagSet`. `New` calls this
automatically when `Surface.GlobalFlags.Mode` is `SurfaceHostOwned`; malformed
binding sets return an error from `BindGlobalFlags` and panic from `New`.

Version ownership is controlled by `Surface.Version`. Rungrad-owned mode enables
Cobra `--version` from `AppConfig.Version`; host-owned and disabled modes leave
Cobra's version flag suppressed. The manifest `tool_version` still comes from
`AppConfig.Version`.

Completion ownership is controlled by `Surface.Completion`. Rungrad-owned mode
keeps Cobra's generated completion command and filters it from docs, manifests,
and catalog validation. Host-owned mode disables Cobra's generated command and
allows a visible product `completion` command, which is included in docs,
manifests, and catalog validation like any other command. Disabled mode disables
Cobra completion and keeps the name reserved.

The manifest endpoint uses `Surface.Manifest`:

| Mode | Valid configuration | Behavior |
| --- | --- | --- |
| `ManifestEndpointRungradOwned` | `Name == ""` and `Render == nil` | Registers hidden `__rungrad_manifest` and uses rungrad's JSON renderer. |
| `ManifestEndpointDisabled` | `Name == ""` and `Render == nil` | Registers no endpoint and reserves no manifest endpoint name. |
| `ManifestEndpointRenamed` | Non-empty single-token `Name`; `Render == nil`; `Name` must not be `__rungrad_manifest`, `help`, an active reserved command name, or an existing top-level command. | Registers a hidden endpoint at `Name` and uses rungrad's JSON renderer. |
| `ManifestEndpointHostRendered` | `Name == ""` and `Render != nil` | Registers hidden `__rungrad_manifest` and calls `Render(ManifestEndpointContext) error`. |

For host-rendered endpoints, the context includes the current Cobra command, the
typed manifest, and a staged stdout writer. If the renderer returns an error,
staged bytes are discarded and rungrad reports the error; there is no fallback
to the default manifest bytes.

Call `app.ManifestDocument()` for the typed `manifest.Manifest`
document directly from a framework-built tree. If you are inspecting or mutating
raw Cobra annotations manually, call `app.ManifestDocumentChecked()` so malformed
`rungrad.extensions` annotation JSON is returned as an error instead of treated
as a programmer panic. The default hidden endpoint and host-rendered endpoints
use the checked builder before rendering.

The conformance scorer discovers only the default `__rungrad_manifest` endpoint.
Under `--manifest auto` or `--manifest required`, scoring uses rungrad-owned and
host-rendered default endpoints when discovery succeeds. Disabled or renamed
endpoints are valid customized framework surfaces, but scoring treats them as
black-box targets in `--manifest auto` or when the caller explicitly uses
`--manifest off`; `--manifest required` fails when the default endpoint is
absent.

## Output: one model, two forms

Never format twice. Build one result value and hand it to the Factory:

```go
func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
    return f.WriteResult(widgets, func(w io.Writer) {
        output.RenderTable(w, output.Table{
            Columns: []string{"ID", "Name"},
            Rows:    rows,
            Empty:   "No widgets.",
        })
    })
}
```

`WriteResult(model, human)` emits `output.StableJSON(model)` under `--json`,
otherwise it calls the human renderer. Because both come from one call, the text
and machine forms stay aligned. The `output` package also provides `Node` (for
key/value detail views) and `MutationSummary` (for create/update/delete results),
plus `RenderNodes`, `RenderTable`, and `RenderMutation`.

For copyable `WriteResult` examples, compare `rgref item get` and `rgref whoami`
in [`cmd/rgref/main.go`](../cmd/rgref/main.go). Their generated command
references are
[`cmd/rgref/docs/rgref_item_get.md`](../cmd/rgref/docs/rgref_item_get.md) and
[`cmd/rgref/docs/rgref_whoami.md`](../cmd/rgref/docs/rgref_whoami.md); their
exact help transcripts are
[`cmd/rgref/testdata/help/rgref_item_get.txt`](../cmd/rgref/testdata/help/rgref_item_get.txt)
and
[`cmd/rgref/testdata/help/rgref_whoami.txt`](../cmd/rgref/testdata/help/rgref_whoami.txt).
`TestItemGetUnique` and `TestWhoamiNeverPrintsRawToken` in
[`cmd/rgref/main_test.go`](../cmd/rgref/main_test.go) cover the shipped
behavior.

For terminal-aware human output, `output` also provides
`RenderMutationMode(w, summary, mode)` and `DryRunPreview.RenderMode(w, mode)`.
These color only the mutation action word and the `DRY RUN` label respectively,
and only when the `output.TerminalMode` has both `ANSI` and `Color` set. The plain
`RenderMutation` and `Render` helpers are the same renderers with a zero
`TerminalMode`. `Factory.TerminalMode()` enables styling only for human output to
a stdout terminal. JSON, jq, template, explicit plain output, piped output,
and ordinary test output stay escape-free. `WritePreview` already renders
previews through `RenderMode(f.Stdout, f.TerminalMode())`, while
mutation-summary color is opt-in by rendering with `RenderMutationMode`.

In advanced-output apps, users refine terminal behavior without changing
command code. `--no-color` disables color while leaving non-color ANSI available.
`--no-ansi` disables all terminal control bytes in human output and disables the
pager. `--no-pager` disables pager use only. Human output to non-terminal stdout
is sanitized the same way as `--no-ansi`, so copied or redirected output does not
contain raw control bytes even if a data field contains them.

`rgref item list` is the reference human-output paging fixture. The command code
lives in [`cmd/rgref/main.go`](../cmd/rgref/main.go), global terminal and pager
flags are listed in [`cmd/rgref/docs/index.md`](../cmd/rgref/docs/index.md), and
the exact root and command help bytes are pinned in
[`cmd/rgref/testdata/help/rgref.txt`](../cmd/rgref/testdata/help/rgref.txt) and
[`cmd/rgref/testdata/help/rgref_item_list.txt`](../cmd/rgref/testdata/help/rgref_item_list.txt).
`TestItemListPagerPolicy` in
[`cmd/rgref/advanced_output_test.go`](../cmd/rgref/advanced_output_test.go)
proves when human output pages and when machine/plain output does not.

`StableJSON` is deterministic: two-space indentation, sorted map keys, and a
trailing newline, byte-identical across runs. Keep timestamps and random ids out of
default output so the determinism rules hold. `output.Node` entries marked `Prose`
are omitted from JSON, including when they are nested inside ordinary structs,
maps, slices, pointers, interfaces, or a node's own `Value` passed to
`StableJSON`. If a model contains a reference cycle (a value that points back to
itself), `StableJSON` returns a `cycle detected in result model` error instead of
overflowing the stack, and the command exits with the API/runtime code (2).

## Advanced output: plain, jq, and template

Advanced output is opt-in at the app level:

```go
app := rungrad.New(rungrad.AppConfig{
    Name:           "mytool",
    Short:          "mytool CLI",
    AdvancedOutput: true,
})
```

Declare support per command with `OutputModes`:

```go
OutputModes: []string{
    rungrad.OutputModeHuman,
    rungrad.OutputModeJSON,
    rungrad.OutputModePlain,
    rungrad.OutputModeJQ,
    rungrad.OutputModeTemplate,
},
```

`WriteResult` keeps its compact signature and, in an advanced app, also serves
`--jq` and `--template` from the same stable JSON model used by `--json`. Use
`WriteOutput` when a command declares `plain`, because `--plain` requires an
explicit copy-safe renderer:

```go
return f.WriteOutput(rungrad.Output{
    Model: widgets,
    Human: func(w io.Writer) { output.RenderTable(w, table) },
    Plain: func(w io.Writer) { renderWidgetNames(w, widgets) },
})
```

The full reference fixture is `rgref item list`: it supports `--plain`,
`--jq '.[].name'`, and
`--template '{{range .}}{{.id}} {{.name}}{{"\n"}}{{end}}'` through
`WriteOutput`. `rgref item get alpha --jq .id` and
`rgref item get alpha --template '{{.id}}'` demonstrate `WriteResult` transform
support. See the command source in
[`cmd/rgref/main.go`](../cmd/rgref/main.go), the generated pages
[`cmd/rgref/docs/rgref_item_list.md`](../cmd/rgref/docs/rgref_item_list.md) and
[`cmd/rgref/docs/rgref_item_get.md`](../cmd/rgref/docs/rgref_item_get.md), the
help goldens
[`cmd/rgref/testdata/help/rgref_item_list.txt`](../cmd/rgref/testdata/help/rgref_item_list.txt)
and
[`cmd/rgref/testdata/help/rgref_item_get.txt`](../cmd/rgref/testdata/help/rgref_item_get.txt),
and `TestItemListPlain`, `TestItemListTransformsPreserveModel`, and
`TestItemGetTransforms` in
[`cmd/rgref/advanced_output_test.go`](../cmd/rgref/advanced_output_test.go).

The guard rejects unsupported or conflicting modes before the command handler
runs: `--jq` and `--template` are mutually exclusive; `--plain` cannot combine
with `--json`, `--jq`, or `--template`; `--plain` is refused unless the command
declares `OutputModePlain`; and transforms are refused unless the command
declares `OutputModeJQ` or `OutputModeTemplate`.

`--jq` and `--template` transform the exact stable JSON bytes that `--json`
would emit. An invalid jq expression or template is a usage error (exit 1). A
transform that fails while executing on the result data is a runtime/API error
(exit 2). On any failure, stdout is empty.

Transform modes are machine-output modes. While `--jq` or `--template` is
active, framework prompts are disabled, `Infof` hints are suppressed, and
`Factory.TerminalMode()` stays plain. JSON, jq, template, and plain output are
escape-free and never paged.

Transform output is deterministic for identical inputs: stable JSON goes in,
`gojq` emits compact JSON with sorted object keys, templates normalize to one
final newline, and results are fully buffered before stdout is written. The
output boundary sanitizes template output too, so a template that prints a raw
control byte from a string field still produces copy-safe stdout.

Advanced human output is also buffered so long listings can be paged. The pager
is selected from `<TOOL>_PAGER`, then `PAGER`, then `less -FRX` on non-Windows
systems. Values are split on whitespace and are not shell-evaluated. If a pager
cannot start, rungrad falls back to writing the already-rendered output directly
to stdout.

The `rgref item list` docs and help transcript named above show the user-facing
advanced-output modes for the same command that `TestItemListPagerPolicy` uses
as the paging fixture. The reference index
[`cmd/rgref/docs/index.md`](../cmd/rgref/docs/index.md) and root help golden
[`cmd/rgref/testdata/help/rgref.txt`](../cmd/rgref/testdata/help/rgref.txt)
pin the global terminal and pager flags.

## Metadata envelope (--include-meta)

To expose request metadata in machine output, declare the capability per command:

```go
&rungrad.Command{
    Use:          "show <id>",
    OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
    SupportsMeta: true,
    Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
        widget, meta, err := client.GetWidget(args[0])
        if err != nil {
            return err
        }
        return f.WriteResultWithMeta(widget, meta, func(w io.Writer) {
            renderWidget(w, widget)
        })
    },
}
```

Build `output.Meta` from the response information your API reports. Keep secrets
out of metadata because it is emitted verbatim. Use `RequestID` for the primary
request and `RequestIDs` when retries, redirects, or upstream services return
more than one request id:

```go
func metaFromResponse(resp *http.Response, page PageInfo, attempts int, waits []int64) output.Meta {
    requestID := resp.Header.Get("X-Request-Id")
    return output.Meta{
        RequestID:  requestID,
        RequestIDs: []string{requestID},
        Pagination: &output.Pagination{
            NextCursor: page.NextCursor,
            HasMore:    &page.HasMore,
        },
        RateLimit: &output.RateLimit{
            Limit:     parseInt64Header(resp.Header, "X-RateLimit-Limit"),
            Remaining: parseInt64Header(resp.Header, "X-RateLimit-Remaining"),
            Reset:     parseInt64Header(resp.Header, "X-RateLimit-Reset"),
            Raw: map[string]string{
                "X-RateLimit-Limit":     resp.Header.Get("X-RateLimit-Limit"),
                "X-RateLimit-Remaining": resp.Header.Get("X-RateLimit-Remaining"),
                "X-RateLimit-Reset":     resp.Header.Get("X-RateLimit-Reset"),
            },
        },
        Retry:       &output.Retry{Attempts: attempts, WaitsMS: waits},
        Idempotency: &output.Idempotency{Key: resp.Header.Get("Idempotency-Key")},
        Extra:       map[string]any{"region": resp.Header.Get("X-Region")},
    }
}
```

Under `--include-meta`, rungrad wraps the machine value as `{data, meta}`.
`--json` and any supported `--jq` or `--template` transforms see that envelope,
so `--jq '.meta'` and `--jq '.data'` both work for commands that declare jq
support. Without `--include-meta`, output is the bare result model. If no
metadata is attached, `--include-meta` still yields a deterministic `{"data":
..., "meta": {}}` shape.

When a command also declares `plain`, use `WriteOutput` and set `Output.Meta`.
Human output and `--plain` are never wrapped; a command may still render captured
metadata in its human view.

The reference command is `rgref item list`: it sets `SupportsMeta`, attaches
`Output.Meta`, and makes the envelope visible to
`rgref item list --include-meta --json`,
`rgref item list --include-meta --jq '.meta.request_id'`, and
`rgref item list --include-meta --template '{{.meta.pagination.total_items}}'`.
See [`cmd/rgref/main.go`](../cmd/rgref/main.go), the generated page
[`cmd/rgref/docs/rgref_item_list.md`](../cmd/rgref/docs/rgref_item_list.md), the
help golden
[`cmd/rgref/testdata/help/rgref_item_list.txt`](../cmd/rgref/testdata/help/rgref_item_list.txt),
and `TestItemListIncludeMetaEnvelope` plus
`TestItemListIncludeMetaTransformsSeeEnvelope` in
[`cmd/rgref/metadata_test.go`](../cmd/rgref/metadata_test.go).

The guard rejects `--include-meta` before the handler runs when the command does
not declare `SupportsMeta`, when no machine-output mode (`--json`, `--jq`, or
`--template`) is active, when combined with `--plain`, or when combined with
`--dry-run`.

## Dry run

A mutating command builds a preview and returns it under `--dry-run`:

```go
if f.DryRun() {
    return f.WritePreview(output.DryRunPreview{
        Method: "POST", Path: "/widgets",
        Body: []output.Field{
            {Name: "name", Value: args[0]},
            {Name: "token", Value: secret, Secret: true}, // masked everywhere
        },
    })
}
```

Fields marked `Secret` are masked in both the human and JSON forms, so a preview
never leaks a credential. On a terminal, `WritePreview` highlights the `DRY RUN`
label; under `--json` or when piped the preview is plain. Without `--dry-run`,
perform the write and return a `MutationSummary` through `WriteResult`.

For destructive actions, mark the command `Destructive: true`, register a local
`--confirm` flag through `Configure`, and gate the action behind
`f.ConfirmDestructive`:

```go
&rungrad.Command{
    Use:         "delete <name>",
    Destructive: true, // implies Mutates: the command honors --dry-run
    Configure: func(cmd *cobra.Command) {
        cmd.Flags().Bool("confirm", false, "Confirm the destructive action without a prompt")
    },
    Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
        preview := output.DryRunPreview{Method: "DELETE", Path: "/widgets/" + args[0]}
        if f.DryRun() {
            return f.WritePreview(preview)
        }
        confirmed, _ := cmd.Flags().GetBool("confirm")
        if err := f.ConfirmDestructive(rungrad.ConfirmOptions{
            Action: "delete widget", Target: args[0], Confirmed: confirmed,
        }); err != nil {
            return err
        }
        // perform the delete, then report a MutationSummary through WriteResult
    },
}
```

`ConfirmDestructive` is safe in every mode: under `--dry-run` it returns without
prompting; with `--confirm` it proceeds; on a terminal it prompts on stderr and
proceeds only on `y`/`yes`; and under machine output (`--json`, `--jq`, or
`--template`), `--no-prompt`, or no terminal it refuses with the usage exit code
(1) instead of blocking, so an agent is never stuck. The refusal body is the
standard JSON error on stderr under `--json`.

## Name resolution

Accept human names where your API wants ids:

```go
id, err := f.Resolve(args[0], lookup, resolve.Options{
    ResourceType: "widget",
    AllowPrompt:  true,
    IsID:         resolve.IsNumericID,
})
```

`lookup` is a `func(name string) ([]resolve.Match, error)`. `f.Resolve` wires the
interactive decision from the global flags: it disambiguates with a numbered prompt
on a terminal, and under `--no-prompt` or machine output (`--json`, `--jq`, or
`--template`) it returns an `AmbiguousError` listing the candidates instead of
blocking. A `NotFoundError` maps to the not-found exit code; an `AmbiguousError`
maps to the usage code.

## Config and credentials

The Factory carries a `config.Store` resolved from `--config` and the tool name.
The validate-then-auth pre-run hook loads the credential for commands marked
`RequiresAuth` into `f.Token`. Credentials are read with env-then-file
precedence, stored in a separate `0600` file, and masked for display with
`config.Mask`. Never print `f.Token` raw; print `config.Mask(f.Token)`.

### Profiles, paths, and service endpoints

To use product-style profile, auth-file, or endpoint resolution, set
`AppConfig.Resolution`:

```go
app := rungrad.New(rungrad.AppConfig{
    Name:   "mytool",
    EnvVar: "MYTOOL_TOKEN",
    Resolution: &rungrad.ResolutionConfig{
        Profile:  true, // registers --profile and enables MYTOOL_PROFILE
        AuthFile: true, // registers --auth-file and enables MYTOOL_AUTH_FILE
        Services: []rungrad.Service{{
            Name:      "api",
            Flag:      "api-url",
            EnvVar:    "MYTOOL_API_URL",
            ConfigKey: "base_url",
            Default:   "https://api.example.com",
            Usage:     "API base URL",
        }},
    },
})
```

Resolution is additive. Apps without `Resolution` keep the compact flag surface.
When enabled, `--config` also gets an env tier from `<TOOL>_CONFIG`.
`<TOOL>` is derived from the app name with non-alphanumeric bytes collapsed to
underscores and uppercased, the same rule used for `<TOOL>_PAGER`.

The full profile, config path, auth-file path, and service-endpoint precedence
details live in [Config and auth](config-and-auth.md). Read resolved values in
command code with:

```go
profile := f.Profile()
configPath := f.ConfigPath()
authPath := f.AuthFilePath()
api, ok := f.Service("api")
```

Default config-backed service values come from `Profile.BaseURL` for
`ConfigKey: "base_url"`, then profile/global `Services` maps for arbitrary
endpoints, then `Defaults` as a compatibility fallback. URL derivation, such as
deriving one endpoint from another, belongs in your adopter code.

For a concrete reference, `rgref` configures service name `api`, flag
`--api-url`, env `RGREF_API_URL`, config key `api_url`, and built-in default
`https://api.rgref.invalid` in
[`cmd/rgref/main.go`](../cmd/rgref/main.go). Its `item list` command reads
`f.Service("api")` and exposes the resolved value in metadata. The user-facing
flag is pinned in [`cmd/rgref/docs/index.md`](../cmd/rgref/docs/index.md) and
[`cmd/rgref/testdata/help/rgref.txt`](../cmd/rgref/testdata/help/rgref.txt);
`TestItemListServiceURLPrecedence` in
[`cmd/rgref/metadata_test.go`](../cmd/rgref/metadata_test.go) covers the
reference command, while `TestResolutionServiceAndProfilePrecedence` and
`TestFlaglessServiceResolvesEnvConfigAndBuiltin` in
[`auth_resolution_test.go`](../auth_resolution_test.go) cover the framework
resolver.

### Config loading

The default loader reads rungrad's `config.yaml` into `config.Config`. For
product-owned config formats, set `ResolutionConfig.LoadConfig` to read the file
and normalize it into `config.Config` before generic profile and service
precedence runs. Missing config files are treated as an empty
`Config{Version: 1}`.

### Custom credential resolution

Set `AppConfig.Auth` to a `CredentialResolver` when the default
env-then-stored-credential behavior is not enough. The resolver receives an
`AuthContext` with the resolved profile, config path, auth-file path, env var,
configured `config.Store`, injected env lookup, resolved services, and
`RegisterSecret`.

Return a `rungrad.Credential` with the primary token, source label, optional
display label, and adopter-defined `Extra` payload. The framework sets
`f.Token`, exposes the full value through `f.Credential()`, and auto-registers
only `Credential.Token` for redaction. Any secret material placed in
`Credential.Extra` or discovered elsewhere must be registered through
`AuthContext.RegisterSecret`.

Returning `config.ErrMissingCredential` exits 3. Returning `config.Error` exits
1. Returning a `rungrad.Error` or any error with `ExitCode() int` uses that code;
other errors exit 2. The default resolver reports malformed or unreadable local
credential files as structured config errors, so they exit 1.

### Browser login

Use `f.OpenBrowser(ctx, url)` to open the user's browser. It calls the injected
`Factory.BrowserOpener` when set, otherwise `browser.Open`. Tests inject the
same hook through `testutil.Options.BrowserOpener`.

The `browser.LoginFlow` helper owns the generic open-then-poll loop: it opens
`AuthURL` once, then calls your poll function on a fixed interval until done,
poll error, or context cancellation. The device protocol, verification
endpoint, credential storage, and API validation remain product code.

The generic browser pieces are [`browser.Open`](../browser/browser.go) and
[`browser.LoginFlow`](../browser/browser.go), with `Factory.OpenBrowser` in
[`auth.go`](../auth.go) and `testutil.Options.BrowserOpener` in
[`testutil/testutil.go`](../testutil/testutil.go). `TestLoginFlowRunSuccess`,
`TestLoginFlowPollError`, `TestLoginFlowOpenError`,
`TestLoginFlowSleepCancellation`, and
`TestLoginFlowCanceledContextStopsBeforeOpenOrPoll` in
[`browser/browser_test.go`](../browser/browser_test.go) cover the open/poll
loop and cancellation behavior. `TestBrowserOpenerInjection` in
[`auth_resolution_test.go`](../auth_resolution_test.go) covers the factory hook.
`TestCustomCredentialResolverAndRedaction` in the same file proves that a custom
`CredentialResolver` can register an extra secret through
`AuthContext.RegisterSecret` and have it redacted at output boundaries, which is
the behavior a browser or device login relies on after it captures a token.

### Structured config errors

`config.Error` reports malformed config/path resolution, invalid profile names,
invalid services, and malformed or unreadable local credential files discovered
by the default resolver. These are usage/configuration faults and map to exit 1
through the same `ExitCode()` interface used by adopter errors.

## Secret redaction

The credential loaded for a `RequiresAuth` command is registered for output
redaction automatically. Register any other runtime secret as soon as command,
auth, or config code discovers it:

```go
f.RegisterSecret(token)
```

Use this for environment tokens, browser-login results, generated keys, and API
error bodies that echo a credential. A registered value is replaced with
`[REDACTED]` at every framework-owned output boundary:
`WriteResult`/`WriteOutput`/`WriteResultWithMeta`/`WritePreview` in human,
`--json`, `--plain`, `--jq`, `--template`, and `--include-meta` forms; JSON and
text errors; `Infof`; the resolve disambiguation prompt; the destructive
confirmation prompt; and pager input. Redaction runs before the pager is invoked.
Resolve prompt redaction covers the default prompter installed by
`Factory.Resolve`; if you pass a custom `resolve.Prompter`, that prompter owns
its own writes and must avoid printing secrets or apply equivalent redaction.

Empty, whitespace-only, and values shorter than five bytes are ignored to avoid
false positives. Register opaque string secrets. JSON output remains valid
because only string contents are rewritten; a registered value emitted as a JSON
number, boolean, or null is not rewritten in machine output. Redaction is
deterministic, so repeatable-output guarantees still hold. Numeric-looking
secrets are still redacted in human, plain, template, and text-error output when
they are printed as text; the non-string scalar limitation applies only to JSON
machine values.

Dry-run field masking (`output.Field{Secret: true}`) and `config.Mask` are
independent and still recommended. Redaction is the final safety pass over all
outbound framework text.

The shipped reference behavior is `rgref whoami`: it prints `config.Mask(f.Token)`
instead of the raw token in [`cmd/rgref/main.go`](../cmd/rgref/main.go). Its
generated page and help golden are
[`cmd/rgref/docs/rgref_whoami.md`](../cmd/rgref/docs/rgref_whoami.md) and
[`cmd/rgref/testdata/help/rgref_whoami.txt`](../cmd/rgref/testdata/help/rgref_whoami.txt);
`TestWhoamiNeverPrintsRawToken` in
[`cmd/rgref/main_test.go`](../cmd/rgref/main_test.go) proves shipped output does
not carry the raw credential. The automatic raw-token safety net is proved
separately by the test-only leak app in
[`cmd/rgref/redaction_test.go`](../cmd/rgref/redaction_test.go), especially
`TestAutoRegisteredAuthTokenRedactedAcrossBoundaries`. That raw-token command is
not shipped behavior; it exists only to prove framework redaction. Broader
framework boundary coverage lives in [`redaction_test.go`](../redaction_test.go).

Use `f.Infof` for non-essential progress or hints on stderr. It is suppressed
under `--quiet` and machine output (`--json`, `--jq`, or `--template`), so a
successful machine-output run carries no human-only guidance on stderr. Do not
put primary command results behind `Infof`; route those through
`WriteResult` or `WritePreview`. Destructive prompts and JSON error bodies do not
go through `Infof`, so `--quiet` cannot hide a required prompt or error.

## Self-update

Add a standard `update` command backed by your repository's releases:

```go
app.AddCommand(update.Command(update.CommandConfig{
    CurrentVersion: version,
    ToolName:       "mytool",
    Fetcher:        update.GitHubFetcher{Owner: "you", Repo: "mytool"},
    Apply:          update.ReplaceExecutable,
}))
```

`update --check` reports status and exits 0 without touching the binary, with
`--json` emitting a machine result. For an offline or air-gapped tool, inject a
fetcher that returns a static release.

`ToolName` sets the program name shown in the standard update command's examples
in `--help`, generated docs, and the manifest. Leave it empty only for tests or
generic examples, where it falls back to `mytool`.

`rgref update` is the standard command built with `update.Command`, offline
`fixedFetcher`, and `ToolName: "rgref"` in
[`cmd/rgref/main.go`](../cmd/rgref/main.go). Its generated page and exact help
transcript are [`cmd/rgref/docs/rgref_update.md`](../cmd/rgref/docs/rgref_update.md)
and
[`cmd/rgref/testdata/help/rgref_update.txt`](../cmd/rgref/testdata/help/rgref_update.txt).
Manifest metadata is pinned in
[`cmd/rgref/manifest_test.go`](../cmd/rgref/manifest_test.go), including
`TestManifestUpdateExamples`, and generated docs/help consistency is pinned in
[`cmd/rgref/docs_test.go`](../cmd/rgref/docs_test.go).

`update.ReplaceExecutable` downloads the selected HTTPS release asset with a
bounded client and replaces the running executable on platforms that support that
operation. It does not verify checksums or signatures; wrap `Apply` with your own
verification if your distribution process requires it.

## Exit codes

The root classifies a returned error into a stable code: 0 success, 1 usage, 2
upstream/runtime, 3 auth, 4 forbidden, 5 not-found, 6 rate-limited. Return
`rungrad.NewError(code, msg)` to set a code explicitly, or return the framework's
typed errors (`resolve.NotFoundError`, `config.ErrMissingCredential`, and so on)
and let the root map them.

Flag misuse is classified for you. If you mark a flag required
(`cmd.MarkFlagRequired`) or declare a flag group (`MarkFlagsRequiredTogether`,
`MarkFlagsMutuallyExclusive`, `MarkFlagsOneRequired`), a violating invocation exits
1 (usage). The pre-run validates flags before loading any credential, so a missing
required flag or an invalid flag group on a `RequiresAuth` command exits 1 (usage),
not 3 (auth).

### Host-owned error rendering and exit codes

Use rungrad's default error text, JSON envelope, and exit-code mapping for new
CLIs. When porting an established Cobra CLI with a public error contract, set
`AppConfig.ErrorPolicy` so the host preserves its stderr shape and
product-specific exit categories without filtering rungrad output.

`ErrorPolicy.Classify` receives an `ErrorContext` with `DefaultExitCode`, the
underlying `Err`, the resolved command path when Cobra found one, raw args, the
global-flag snapshot, and narrow resolved config/auth fields. Return a positive
code to override the process exit code; return zero or a negative value to keep
rungrad's default classification.

`ErrorPolicy.Render` writes only to `ErrorContext.Stderr`. Return nil to fully
own stderr; rungrad will not append its default `Error:` line or JSON object.
Return a non-nil error to discard the host's staged bytes and fall back to the
default renderer. The fallback preserves the original exit code and adds a
redacted `renderer_error` detail in machine output.

`ErrorContext` is deliberately read-only: `Flags` is a value snapshot, the
context exposes no `*Factory`, and credential display is masked. Use
`RedactString`, `RedactText`, and `RedactJSON` for any host-rendered output;
use `RedactJSON` for parseable machine envelopes. rungrad does not apply an
extra redaction pass to bytes from a successful host renderer, because only the
host knows whether those bytes are text or JSON. `MachineOutput` uses parsed
global flags after flag parsing succeeds, and falls back to raw args only for
bare command-resolution and flag-parse failures, so `--json`, `--jq`, or
`--template` can still render those early errors as machine output.

```go
type productError struct {
    Code    int
    Message string
}

func (e productError) Error() string { return e.Message }

app := rungrad.New(rungrad.AppConfig{
    Name: "mytool",
    ErrorPolicy: &rungrad.ErrorPolicy{
        Classify: func(ctx rungrad.ErrorContext) int {
            var pe productError
            if errors.As(ctx.Err, &pe) {
                return pe.Code
            }
            return ctx.DefaultExitCode
        },
        Render: func(ctx rungrad.ErrorContext) error {
            if ctx.MachineOutput {
                body, err := output.StableJSON(map[string]any{
                    "message": ctx.Err.Error(),
                    "code":    ctx.ExitCode,
                })
                if err != nil {
                    return err
                }
                _, err = ctx.Stderr.Write(ctx.RedactJSON(body))
                return err
            }
            _, err := fmt.Fprintf(ctx.Stderr, "mytool: %s\n", ctx.RedactString(ctx.Err.Error()))
            return err
        },
    },
})
```

## Testing

Run your own commands in-process and assert on the captured output:

```go
res := testutil.Run(app, "widget", "list", "--json")
if res.Exit != rungrad.ExitSuccess { t.Fatal(res.Stderr) }
```

`testutil.Run`/`RunWith` capture stdout, stderr, and the classified exit code;
`testutil.MockServer` stands up an `httptest` server for an API client. Because
output is deterministic, tests pin it exactly.

## Help goldens, docs sync, and consistency

Declare one update flag in your test package and pass it to the helpers:

```go
var update = flag.Bool("update", false, "regenerate golden files")

func TestGeneratedDocsInSync(t *testing.T) {
    testutil.AssertDocsInSync(t, newApp, *update, "docs")
}

func TestHelpGoldensInSync(t *testing.T) {
    testutil.AssertHelpGoldens(t, newApp, *update, "testdata/help")
}

func TestHelpDocsManifestConsistent(t *testing.T) {
    testutil.AssertConsistent(t, newApp)
}
```

`testutil.AssertDocsInSync` checks committed `docsgen.Generate` output and, when
`-update` is set, calls `docsgen.Write` to regenerate pages and prune orphans.
`docsgen.Check` returns a `CheckResult` with grouped `Missing`, `Stale`, and
`Orphaned` pages so failures identify the exact drift class.

`testutil.AssertHelpGoldens` captures exact `--help` bytes for every visible
command, one `.txt` golden per command. Help is captured through a non-terminal
buffer, so the output is deterministic and the framework applies no
normalization.

`testutil.AssertConsistent` needs no committed files. It captures the manifest,
generated docs, and help from fresh apps, verifies that every command's manifest
metadata is reflected by help and docs, and runs `App.ValidateCatalog` when the
app declares a catalog.

Generated docs and help transcripts are different gates. `docsgen` pages are the
curated command reference checked by `TestGeneratedDocsInSync`; help goldens are
exact `--help` bytes checked by `TestHelpGoldensInSync`.
`TestHelpDocsManifestConsistent` and the catalog check verify that generated
docs, help, manifest metadata, and catalog rows agree on one visible command
surface.

The reference CLI exercises all three in
[`cmd/rgref/docs_test.go`](../cmd/rgref/docs_test.go) and
[`cmd/rgref/catalog_test.go`](../cmd/rgref/catalog_test.go); committed docs live
under [`cmd/rgref/docs/`](../cmd/rgref/docs/), and exact help goldens live under
[`cmd/rgref/testdata/help/`](../cmd/rgref/testdata/help/). The same `-update`
flag regenerates docs and help goldens together.

## Manifest

By default, apps built with `rungrad.New` expose `__rungrad_manifest`
automatically. The hidden command emits deterministic JSON generated from the
same command tree and metadata used by help and docs generation, without loading
credentials or running adopter handlers. Use `AppConfig.Surface.Manifest` to
disable, rename, or host-render that endpoint. See [The rungrad manifest](manifest.md)
for the protocol and schema. The reference manifest metadata and global flags
are pinned by `TestManifestReferenceCommands`, `TestManifestReferenceOutputModes`,
and `TestManifestGlobalFlagsIncludeAdvanced` in
[`cmd/rgref/manifest_test.go`](../cmd/rgref/manifest_test.go).

For product-owned metadata that needs to appear in the machine manifest but is not
part of rungrad's core command contract, set `Command.Extensions`:

```go
Extensions: manifest.ExtensionSet{
    "example.com/product": {
        "owner":     "platform",
        "status":    "beta",
        "docs_path": "docs/widget-list.md",
    },
},
```

Attach extensions through the typed `Command.Extensions` and
`CommandSpec.Extensions` fields, not by writing raw `rungrad.extensions`
annotations. Namespaces must be lowercase `example.com/product`-style names;
`rungrad/` and `rungrad.` are reserved. Values must be JSON-compatible objects
with no nulls, no non-finite numbers, no cycles, no custom JSON/text marshaling,
and no first-level field names that reuse core manifest command fields. The
`manifest` package exposes `ValidateExtensionSet`, `EncodeExtensions`, and
`DecodeExtensions` for the wire shape, plus `RequireExtensionFields`,
`RequireExtensionEnum`, and `RequireExtensionDocPath` for product tests. Use
those helpers against `app.ManifestDocument()` to assert product invariants such
as owner, lifecycle status, and docs-path rules.

Extensions are a boundary, not an override mechanism. Put docs/audit/ownership
metadata under product field names, and keep rungrad-owned semantics such as
`mutates`, `supports_dry_run`, `destructive`, and `supports_meta` in the core
fields. Generic manifest consumers tolerate valid unknown namespaces.

## Starting from the scaffold

Use `rungrad new <name>` for the compact widget starter. It includes a widget
resource, text/JSON output, mutating dry-run behavior, destructive confirmation,
an offline update command, generated tests, and a README without making
product-level choices.

Use `rungrad new <name> --product-profile` for a product-shaped CLI: custom
env-var prefix, profile/auth-file/config resolution,
service endpoint flags/env/config keys, product labels, a manifest extension
namespace, and a choice of global-flag ownership through `AppConfig.Surface`.
The product scaffold also demonstrates `manifest.ExtensionSet` on a command and
keeps the default manifest endpoint available for offline scoring. It does not
enable advanced output or host error rendering by default.

For an existing Cobra CLI, generate a product scaffold with matching names and
service placeholders, then use it as a comparison target while porting by hand:
compare root `AppConfig`, global flag ownership, command metadata, output
rendering, dry-run previews, destructive confirmation, manifest extensions, and
update wiring. There is no automated rename, migration, or porting command.

## Scoring yourself

Build your binary and run `rungrad score` against it (see
[Conformance and the spec](conformance.md)). A tool built with this guide should
score 100% on the rules that apply to it. `TestReferenceCLIScoresPerfect` in
[`cmd/rgref/conformance_test.go`](../cmd/rgref/conformance_test.go) pins this
for the reference CLI.
