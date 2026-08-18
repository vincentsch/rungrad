# Contributing

rungrad is pre-1.0. The spec is versioned separately from the Go API, and the Go
API may still change before a stable release.

## Local checks

Run these before sending a change:

```bash
gofmt -l .
git diff --check
go vet ./...
go build ./...
go test ./...
go test -race ./...
```

Keep generated reference docs and help goldens in sync:

```bash
go test ./cmd/rgref -run 'TestGeneratedDocsInSync|TestHelpGoldensInSync' -update -count=1
```

## Style

Prefer small changes with tests that exercise the public CLI surface. Keep docs
plain and direct. Do not commit credentials, machine-specific files, local
workflow notes, or unreleased downstream repository names.
