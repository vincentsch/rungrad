# Migrating from Cobra

This guide is for teams moving an existing Cobra CLI onto rungrad without
rewriting their public contract. For a new product CLI, start
from the product scaffold in [Getting started](getting-started.md#product-profile):

```bash
rungrad new acmectl --product-profile
```

For an existing CLI, use the scaffold as a comparison target, not as a
porting tool. The porting path is: pin the behavior users and scripts
already depend on, introduce rungrad behind those compatibility tests, then
delete local bridge shims once the rungrad APIs below replace them.

## Accepted default change

Human command-resolution errors keep their existing text unless the product
changes it through `AppConfig.ErrorPolicy`. The accepted rungrad default change
is machine-output intent: command-resolution and flag-parse failures with
`--json`, and with `--jq` or `--template` when advanced output is enabled, emit
the structured JSON error envelope instead of human text.

Treat the migration goal as public behavior preservation with that one
documented exception. Pin it explicitly in tests so callers know which stderr
shape to expect.

## Ownership boundary

rungrad handles CLI mechanics:

- execution lifecycle and validate-then-auth pre-runs
- framework global flags and changed-state tracking
- output dispatch, dry-run previews, terminal mode, and pager behavior
- error hooks and default exit classification
- manifest emission, docs/help/catalog gates, scaffolding, and conformance
  scoring

The product handles domain and service behavior:

- API and transport clients
- auth and login protocols
- config file schemas and compatibility
- public API/output envelopes
- command names, local flags, and domain actions
- installer, release, and live-service smoke checks

## Ordered path

1. Pin current behavior before moving code.

   Capture command paths, root and local flags, help goldens, JSON shapes, human
   error text, machine error bodies, exit codes, config/auth behavior, generated
   docs, manifest output if present, installer behavior, and live service smoke
   checks. In rungrad-based tests, use `testutil.AssertHelpGoldens`,
   `testutil.AssertDocsInSync`, and `testutil.AssertConsistent`.

2. Introduce rungrad behind the compatibility tests.

   Construct `rungrad.New`, move commands over while preserving public names and
   flags, adopt host-owned surfaces where the existing CLI already owns those
   names, add an `ErrorPolicy` for existing stderr and exit behavior, and keep
   the pinned tests green after each command family moves.

3. Delete local bridge shims.

   Once a shim maps directly to a rungrad API below, delete the local copy and
   test the rungrad-owned path. Do not keep duplicate compatibility layers after
   the framework handles the behavior.

## Replace bridge workarounds

### Slice-flag reset

In stock pflag, a slice flag remembers it was set. When a command tree is reused,
the next run may append to the previous run's values instead of replacing them.
rungrad resets that state: use ordinary pflag `StringArray`,
`StringSlice`, and the rest of the pflag `SliceValue` family directly on root,
persistent, local, inherited, nested, and reused-app command scopes. There is no
adopter method to call and no local marker/reset shim to keep.

### Host error rendering and exits

Use [host-owned error rendering and exit codes](building-a-cli.md#host-owned-error-rendering-and-exit-codes)
through `AppConfig.ErrorPolicy`. `Classify` returns the product exit code when
the product handles a category, and `Render` writes the product's stderr shape.
Both hooks receive an `ErrorContext` with one classified exit code, a copied
`GlobalFlags` snapshot, masked credential display fields, and
`RedactString`/`RedactText`/`RedactJSON` helpers. The context intentionally does
not expose `*Factory`.

Keep the accepted default change from [Accepted default change](#accepted-default-change)
in the pinned tests: machine-output command-resolution failures produce
structured JSON.

### Framework surface ownership

Use [framework surface customization](building-a-cli.md#framework-surface-customization)
through `AppConfig.Surface` instead of mutating Cobra state after
`rungrad.New`. `Surface.GlobalFlags` gives all-or-nothing global flag ownership
with `GlobalFlagBindings`, while still feeding rungrad's canonical flag state.
`Surface.Version` and `Surface.Completion` decide whether rungrad, the host, or
neither side owns those root surfaces. `Surface.Manifest` supports the default
hidden endpoint, a disabled endpoint, a renamed endpoint, and host-rendered
manifest output. `App.ManifestDocument()` and `ManifestDocumentChecked()` expose
the generated manifest without shelling out.

Global flag ownership is all-or-nothing for active rungrad globals. The
`json`, `jq`, and `template` bindings cannot use shorthands because those names
also drive raw-argv machine-output detection for early errors.

### Catalog sidecars and manifest extensions

Use [`manifest.ExtensionSet`](manifest.md#command-extensions) and
`manifest.ExtensionObject` on `rungrad.Command`, mirrored on `CommandSpec`, for
product-owned command metadata. The framework serializes that data through the
`rungrad.extensions` annotation and validates catalog drift through
`ValidateCatalog`. Product tests enforce invariants with
`RequireExtensionFields`, `RequireExtensionEnum`, and
`RequireExtensionDocPath`.

This replaces parallel product manifest projections. Keep product metadata under
product-owned field names; do not use extensions to contradict rungrad-owned
fields such as `mutates`, `supports_dry_run`, `destructive`, or
`supports_meta`.

### Product scaffold comparison

Use `rungrad new --product-profile` as the trusted reference tree for product
identity, config/profile/auth-file wiring, service endpoint precedence,
manifest extensions, update wiring, docs, generated tests, and optional
host-owned global flags. Use product-profile flags such as `--env-prefix`,
`--product-name`, `--description`, `--service name=url`,
`--metadata-namespace`, `--surface rungrad|host`, `--release-owner`,
`--release-repo`, `--docs-label`, and repeatable `--example` to make the
reference tree resemble the existing CLI. Compare its rungrad wiring to the
port, then move the existing domain behavior manually. There is no automated
rename or porting command.

### Docs, help, manifest, and score gates

Use rungrad's gates while porting:

- `testutil.AssertDocsInSync` for committed generated docs
- `testutil.AssertHelpGoldens` for committed help output
- `testutil.AssertConsistent` for help, docs, manifest, and catalog agreement
- [`rungrad score`](conformance.md#scoring-a-cli) for subprocess conformance

Keep product-local live checks outside `rungrad score`; the scorer verifies the
agent-ready CLI contract, not API availability.

## Checklist

- [ ] Inventory every command path, flag, env var, config key, credential source,
  output shape, error body, and exit code the existing CLI exposes.
- [ ] Commit compatibility tests for help, docs, JSON, stderr, exit codes,
  config/auth behavior, installer behavior, and live smoke checks.
- [ ] Build the rungrad app with names and command paths preserved.
- [ ] Choose rungrad-owned or host-owned surface modes for globals, version,
  completion, and the manifest endpoint.
- [ ] Add `ErrorPolicy` only where the product needs to preserve error text or
  exit classification.
- [ ] Mirror command metadata in a `CommandSpec` catalog and run
  `ValidateCatalog` or `testutil.AssertConsistent`.
- [ ] Move product metadata into `manifest.ExtensionSet` when generic consumers
  need to see it.
- [ ] Run `rungrad score` against the built binary with the commands that apply
  to the product.
- [ ] Delete local bridge shims once their rungrad replacement is covered.

## Product responsibilities

The product repository still owns API clients, auth and login protocols, config
schema migration, public output envelopes, domain commands, installer and
release checks, and live service smoke. Keep those tests in the product repo and
run them alongside the rungrad compatibility gates.

## Worked examples

The committed [`internal/adopterfixture`](../internal/adopterfixture/adopterfixture.go)
is an "acmectl" CLI that exercises host-owned globals, a host
`ErrorPolicy`, manifest extensions, catalog validation, docs/help/manifest
consistency, a default manifest endpoint, and scoring a compiled binary.

The [`cmd/rgref`](../cmd/rgref/main.go) reference CLI remains the compact
from-scratch example and the full scoring target for the framework.
