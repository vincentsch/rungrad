# rungrad

rungrad is a Go framework, command-line tool, and open specification for CLIs
that need stable output, safe mutation previews, and predictable behavior under
automation.

It is built on Cobra. You can use the framework to build a new CLI, use the
`rungrad` binary to scaffold or score a CLI, or use the written spec without the
Go framework.

## Install

Requires Go 1.22.2 or newer.

```bash
go install github.com/vincentsch/rungrad/cmd/rungrad@v0.2.1
```

Add the framework to a Go module:

```bash
go get github.com/vincentsch/rungrad@v0.2.1
```

## Start a CLI

```bash
rungrad new mytool
cd mytool

go mod tidy
go test ./...
go run . widget list
go run . widget list --json
go run . widget create gamma --dry-run
```

`rungrad new` creates a working project with commands, tests, JSON output,
dry-run behavior, destructive-action confirmation, a manifest endpoint, and an
offline update command.

## Score a CLI

```bash
go build -o mytool .
rungrad score ./mytool \
  --read        "widget list" \
  --mutate      "widget create demo" \
  --destructive "widget delete alpha" \
  --update
```

The scorer reports pass, fail, or not-applicable for each rule in
`rungrad-spec/1`. Add `--json` for a machine-readable score and `--strict` to
fail CI when a required rule fails.

## What it provides

- Stable JSON and human output from the same command result.
- `--dry-run` previews for mutating commands.
- Destructive-action confirmation.
- Stable exit codes for scripts and agents.
- Name-to-ID resolution with safe non-interactive failures.
- Hidden machine manifest for tools that need to inspect a CLI.
- Generated command docs and help-golden test helpers.
- A conformance scorer for any executable, not only Go tools.

## Docs

- [Getting started](docs/getting-started.md): install, scaffold a CLI, score a CLI
- [Building a CLI with rungrad](docs/building-a-cli.md): the framework guide for tool authors
- [Migrating from Cobra](docs/migrating-from-cobra.md): port an existing Cobra CLI without changing its public surface
- [Machine manifest](docs/manifest.md): the hidden manifest protocol
- [Conformance and the spec](docs/conformance.md): the agent-ready spec and the scorer
- [CLI reference](docs/cli-reference.md): the `rungrad` command (`score`, `new`)

The written specification lives in [`spec/`](spec/README.md).

## Status

rungrad is pre-1.0. The spec version is `rungrad-spec/1`; the Go API may still
change before a stable 1.0 release.

## License

MIT
