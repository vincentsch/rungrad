# Getting started

rungrad is a Go framework for building CLIs and a `rungrad` binary for
scaffolding and scoring them.

## Install

Install the tool:

```bash
go install github.com/vincentsch/rungrad/cmd/rungrad@v0.2.2
```

Add the framework to an existing Go module:

```bash
go get github.com/vincentsch/rungrad@v0.2.2
```

## Scaffold a new CLI

```bash
rungrad new mytool
cd mytool
go mod tidy
go test ./...        # the generated tool's tests pass
go run . widget list
go run . widget list --json
go run . widget create gamma --dry-run
go run . widget delete alpha --dry-run
go run . update --check
```

`rungrad new` writes a project: `go.mod`, `main.go`, a `widget`
resource, tests, a manifest endpoint, and a README. The generated commands show
JSON output, dry-run previews, destructive-action confirmation, and an offline
`update` command.

Scaffold flags:

- `--module` sets the Go module path.
- `--dir` chooses the parent directory.
- `--dry-run` shows the files without writing them.

### Product profile

For a larger product CLI, generate the expanded scaffold:

```bash
rungrad new acmectl \
  --product-profile \
  --env-prefix ACME \
  --product-name "Acme Control" \
  --service api=https://api.example.invalid \
  --metadata-namespace example.com/acme \
  --surface host
```

`--product-profile` keeps the widget example but adds product identity,
profile/auth-file/config resolution, service endpoints, manifest extensions, and
release placeholders. The product-only flags are documented in the
[CLI reference](cli-reference.md).

## Score a CLI against the spec

Point the scorer at any executable and tell it which commands exercise each
behavior:

```bash
go build -o mytool .
rungrad score ./mytool \
  --read        "widget list" \
  --mutate      "widget create demo" \
  --destructive "widget delete alpha" \
  --update
```

The output is a per-section report and an overall score. If the target exposes a valid
manifest, the scorer also checks fixture paths and command metadata. Add `--json`
for a JSON score and `--strict` to fail CI when a required rule
fails. Commands you do not provide are reported as not-applicable, not as
failures.

See [Conformance and the spec](conformance.md) for the full flag list and
scoring rules.

## Next

- To build a CLI, read [Building a CLI with rungrad](building-a-cli.md).
- The worked reference tool is `cmd/rgref` in this repository; it scores 100%
  against the spec and is a good example to read.
