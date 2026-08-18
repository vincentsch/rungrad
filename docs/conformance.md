# Conformance and the spec

rungrad turns "agent-ready" from a claim into something you can measure. The spec
is a short, versioned, language-neutral checklist of the properties that make a
CLI safe and legible for an automated caller. The scorer drives any CLI and
reports how it does against that spec.

## The spec

The written specification lives in [`../spec/`](../spec/README.md), version
`rungrad-spec/1`, with one document per section:

| Section | What it requires |
| --- | --- |
| Output contract | Human view by default, stable `--json` from the same model |
| Exit-code model | A small, stable set of exit codes (0–6) |
| Dry run | Mutating commands preview under `--dry-run` without acting |
| Determinism | Same input, same output; no incidental noise |
| Name resolution | Names accepted; ambiguous names never block under `--no-prompt` |
| Self-describing help | `--help` carries examples and related commands |
| Self-update | A read-only `update --check`, with a JSON form |
| Auth and config | Documented credential precedence; secrets never printed |

Each section lists testable assertions. Every assertion is encoded as a rule in
[`../spec/ruleset.yaml`](../spec/ruleset.yaml) with a stable id, a severity
(`required` or `recommended`), and a probe name. The prose and the ruleset are kept
in step by a test in the `conformance` package.

The spec stands on its own. A tool in any language can conform to it; the Go
framework is just one implementation that provides the behaviors by default.

## Machine manifest

Tools built with the Go framework expose `__rungrad_manifest` by default, a
hidden command that emits deterministic JSON describing the command tree, flags,
and rungrad metadata. It is a producer protocol for structured metadata: the
command does not require `--json`, load credentials, or run adopter handlers.
Adopters can customize that endpoint through `AppConfig.Surface.Manifest`: the
default and host-rendered default endpoint are discoverable by the scorer, while
disabled or renamed endpoints are valid customized surfaces that score through
black-box probing. See [The rungrad manifest](manifest.md) for the schema and
emission contract.

### Manifest discovery during scoring

`rungrad score` can discover that hidden endpoint before black-box probing:

| Mode | Behavior |
| --- | --- |
| `--manifest auto` | Default. Try discovery at the default `__rungrad_manifest` endpoint and fall back to black-box scoring when the manifest is missing, invalid, or unsupported. |
| `--manifest off` | Skip discovery and report manifest status `disabled`. |
| `--manifest required` | Fail before scoring when no valid supported manifest is available at the default endpoint. |

The score reports one of five manifest statuses: `disabled`, `missing`, `present`,
`invalid`, or `unsupported`. The summary field `schema_version` is the manifest
schema, such as `rungrad-manifest/1`, and is distinct from the scored spec
version, `rungrad-spec/1`.

When a valid manifest is `present`, the scorer validates every configured fixture
command path against the manifest before running that probe's normal black-box
subprocess check. It also applies structured metadata pre-checks where the
manifest has the relevant declaration:

- `output.json-parseable` and `output.dual-form` check the read command's
  `output_modes`; dual form requires both `json` and a human mode.
- `dryrun.accepted` and `dryrun.no-side-effects` require the mutate command to
  declare both `mutates` and `supports_dry_run`.
- The destructive dry-run and confirmation rules require both `destructive` and
  `requires_confirmation`.
- `exit.missing-credential-auth` and `auth.secret-not-printed` require
  `requires_auth`.
- `auth.config-flag` requires the global `config` flag.
- `update.check-readonly` validates that the manifest lists `["update"]`;
  `update.check-json` also requires a `json` output mode.
- `help.examples` and `help.related` use the read command's manifest
  `examples`/`related` lists and require one listed entry to appear in `--help`.
  The older broad substring checks are used only when no manifest is active.

Backend-dependent exit fixtures, determinism, and `resolution.no-prompt` stay
black-box after path validation. `exit.unknown-usage` is fully black-box and does
not consult the manifest. An omitted fixture still reports `not_applicable` even
with a present manifest.

Manifest command matching is by longest visible command-path prefix, because
fixtures often include positional arguments after the command path. A fixture for
an unlisted top-level hidden command fails manifest path validation when a
manifest is present because hidden commands are intentionally excluded. A fixture
that names an unlisted child under a visible parent may resolve to that parent,
then still has to satisfy that rule's metadata and black-box checks. Use
`--manifest off` to score an intentionally hidden fixture, disabled manifest
endpoint, or renamed manifest endpoint entirely through the legacy black-box
path. There is no endpoint-name override: renamed endpoints are an adopter
surface, not an alternate scorer discovery target. The JSON fields
`used_rule_count` and `used_rules` report which rules consulted manifest data,
including rules that failed a manifest pre-check. Human reports show a
`Manifest-backed:` line only for `present` runs.

## Scoring a CLI

```bash
rungrad score ./mytool \
  --read        "widget list" \
  --mutate      "widget create demo" \
  --destructive "widget delete alpha" \
  --auth        "whoami" \
  --ambiguous   "widget get dup" \
  --not-found   "widget get ghost" \
  --secret      "whoami" \
  --secret-env  "MYTOOL_TOKEN" \
  --update
```

Each flag names one of the target's commands that exercises a behavior:

| Flag | Used for |
| --- | --- |
| `--read` | a read/list command, for output, determinism, help, and config probes |
| `--mutate` | a state-changing command, run with `--dry-run` |
| `--auth` | a command that requires a credential, run with none available |
| `--ambiguous` | a resolution command given a name that matches more than one resource |
| `--not-found` | a command naming a resource that does not exist |
| `--api-error` | a command that hits an upstream or runtime error (optional; backend-dependent) |
| `--forbidden` | a command refused because the caller lacks permission (optional; backend-dependent) |
| `--rate-limited` | a command throttled by an upstream service (optional; backend-dependent) |
| `--destructive` | a destructive command, run only through its `--dry-run` and refused-confirmation paths (optional; supply a documented safe/stub command) |
| `--secret` + `--secret-env` | a credential-handling command and the env var that carries the secret |
| `--update` | set when the target has an `update` command |
| `--manifest auto\|off\|required` | control manifest discovery before black-box probing |

A flag you do not provide makes its probes **not-applicable**, which neither helps
nor hurts the score. The scorer runs the target as a subprocess in an isolated
config home with no credentials. Most probes use empty stdin, so prompts cannot
hang the scorer; the destructive refusal probes instead use a blocking stdin pipe
for bare no-terminal, `--no-prompt`, and `--json` runs so any attempted prompt or
stdin read is detected as a timeout. It does not sandbox the operating system
network stack; target commands supplied to the scorer should be read-only,
dry-run capable, or pointed at a safe stub backend.
The destructive probes never pass `--confirm`, so the scorer never authorizes the
real action; even so, black-box scoring cannot stop a broken target from acting,
so the `--destructive` command must be a documented safe or stub command whose
dry-run and refused-confirmation paths are safe to run repeatedly.

## Reading the result

The default output is a per-section report with an overall percentage and a
pass/fail/not-applicable line per rule. `--json` emits the full `Score` (spec
version, overall, per-section, per-rule) for dashboards or CI.

The overall and section percentages are weighted, with required rules counting
more than recommended rules. The pass counts beside them are raw rule counts, and
the overall line also shows how many rules were applicable. Treat a high
percentage with very few applicable rules as an incomplete measurement, not a
badge-ready score.

`--strict` makes `rungrad score` exit non-zero when any **required** rule fails, so
you can gate a pull request on conformance:

```bash
rungrad score ./mytool --read "widget list" --update --strict
```

Scoring weights required rules above recommended ones and excludes not-applicable
rules from the denominator. A score always reports the spec version it was computed
against, so it stays meaningful as a tool evolves.
A required rule reported not-applicable, for example the destructive rules when no
`--destructive` fixture is configured, is excluded from the denominator and does
not trip `--strict`; only a required rule that actually fails does.

The `auth.config-flag` probe is intentionally black-box: it can verify that a read
command accepts an isolated `--config` path and still succeeds, but it cannot prove
that target-specific config values are semantically used unless the target exposes a
read command whose output depends on config.

## Example: the reference CLI

The `rgref` tool in this repository scores 100%. A test builds it and scores it
against the embedded spec on every run, which is how the framework proves its own
output matches the spec it ships.
