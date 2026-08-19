# CLI reference: `rungrad`

The `rungrad` command scaffolds new agent-ready CLIs and scores any CLI against the
spec. It is itself built on the framework, so it carries the global flags and dual
output it asks of other tools.

## Global flags

Available on every command:

| Flag | Meaning |
| --- | --- |
| `--json` | Emit stable JSON instead of the human view; also suppresses `Factory.Infof` hints |
| `--dry-run` | Preview an action without performing it |
| `--no-prompt` | Never block on an interactive prompt |
| `--quiet` | Suppress non-essential output emitted through `Factory.Infof` |
| `--config <path>` | Path to the config file |
| `--version` | Print the version |
| `--help` | Show help, with examples and related commands |

## `rungrad new <name>`

Scaffold a new rungrad CLI project under `<dir>/<name>`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--module <path>` | `example.com/<name>` | Go module path for the new project |
| `--dir <path>` | `.` | Parent directory to create the project in |
| `--dry-run` | | List the files that would be created without writing them |
| `--product-profile` | `false` | Generate the expanded product CLI scaffold |
| `--env-prefix <PREFIX>` | derived from `<name>` | Product env-var prefix |
| `--product-name <name>` | `<name> CLI` | Human product label |
| `--description <text>` | product starter description | Root long description |
| `--service name=url` | `api=https://api.example.invalid` | Product service endpoint; repeatable |
| `--metadata-namespace <namespace>` | `example.com/<name>` | Manifest extension namespace |
| `--surface rungrad\|host` | `rungrad` | Global-flag ownership |
| `--release-owner <slug>` | `example` | Comment/docs-only release owner placeholder |
| `--release-repo <slug>` | `<name>` | Comment/docs-only release repo placeholder |
| `--docs-label <title>` | product name | README title |
| `--example "<cmd>"` | | Extra generated command example; repeatable |

The generated project builds, runs, and passes its tests. It
refuses to overwrite an existing non-empty project directory. Product-only flags
require `--product-profile`; the product scaffold still uses offline widget and
update examples and keeps release wiring as comments/docs placeholders.

```bash
rungrad new mytool
rungrad new mytool --module github.com/me/mytool --dir ~/code
rungrad new mytool --dry-run --json
rungrad new acmectl --product-profile --env-prefix ACME --product-name "Acme Control"
```

## `rungrad score <target>`

Score an executable against the spec. `<target>` is the path to the binary.
Missing, non-executable, or directory targets are usage errors and exit 1.

| Flag | Meaning |
| --- | --- |
| `--read "<cmd>"` | A read/list command |
| `--mutate "<cmd>"` | A state-changing command (run with `--dry-run`) |
| `--auth "<cmd>"` | A command requiring a credential |
| `--ambiguous "<cmd>"` | A resolution command given an ambiguous name |
| `--not-found "<cmd>"` | A command naming a missing resource |
| `--api-error "<cmd>"` | A command that hits an upstream or runtime error (optional; backend-dependent) |
| `--forbidden "<cmd>"` | A command refused for lacking permission (optional; backend-dependent) |
| `--rate-limited "<cmd>"` | A command throttled by an upstream service (optional; backend-dependent) |
| `--destructive "<cmd>"` | A documented safe/stub destructive command, run only through its dry-run and refused-confirmation paths (the scorer never passes `--confirm`) |
| `--secret "<cmd>"` | A credential-handling command |
| `--secret-env "<VAR>"` | Environment variable carrying the credential |
| `--manifest auto\|off\|required` | Manifest discovery mode (default `auto`) |
| `--update` | The target has an `update` command |
| `--strict` | Exit non-zero if a required rule fails |
| `--json` | Emit the full JSON score |

Command values are passed as a single quoted string and split on spaces, for
example `--read "widget list"`. Flags you omit make their probes not-applicable.
In default `--manifest auto` mode, a valid target manifest also validates fixture
paths and declared command metadata before the black-box checks run.
The scorer never passes `--confirm` to the `--destructive` command, but it does
not sandbox a broken target, so supply only a safe or stub destructive command.

```bash
rungrad score ./mytool \
  --read        "widget list" \
  --mutate      "widget create demo" \
  --destructive "widget delete alpha" \
  --update
rungrad score ./mytool --read "widget list" --strict        # CI gate
rungrad score ./mytool --read "widget list" --json          # machine result
```

See [Conformance and the spec](conformance.md) for how scoring works.
