# Release checklist

Use this checklist before publishing a source release.

## Prepare

1. Update public-facing files when needed: `README.md`, `docs/`,
   `SECURITY.md`, `CONTRIBUTING.md`, and this checklist.
2. Bump embedded release versions in `cmd/rungrad` and `cmd/rgref`.
3. Bump the generated project dependency in
   `scaffold/templates/gomod.tmpl`.
4. Run:

```bash
gofmt -l .
git diff --check
go vet ./...
go build ./...
go test ./...
go test -race ./...
```

5. Scan the tracked tree:

```bash
git ls-files | rg '(^|/)(\.env|\.netrc|credentials|token|secret)'
git grep -n -i -E 'token|secret'
```

Review every hit. Auth, config, and redaction tests may be legitimate; docs,
scripts, and unexpected files need closer review.

## Tag

Use annotated tags:

```bash
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
```

Go module tags are effectively immutable once the module proxy and checksum
database observe them. Never move or recreate a public tag. Publish a new patch
tag instead.

## Verify

Use a clean environment with public module settings and an empty module cache:

```bash
go install github.com/vincentsch/rungrad/cmd/rungrad@vX.Y.Z
rungrad new demo
cd demo
go mod tidy
go test ./...
```
