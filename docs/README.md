# rungrad documentation

rungrad is a Go framework, CLI tool, and spec for command-line tools used in a
terminal, scripts, and CI.

Start with [Getting started](getting-started.md) to generate a starter CLI and
score it. Use [Building a CLI with rungrad](building-a-cli.md) when replacing the
starter commands with your own code.

- [Getting started](getting-started.md): install, scaffold a CLI, score a CLI
- [Building a CLI with rungrad](building-a-cli.md): the framework guide for tool authors
- [Migrating from Cobra](migrating-from-cobra.md): port an existing Cobra CLI while preserving public behavior
- [Config and auth reference](config-and-auth.md): profiles, auth files, services, and credential hooks
- [Conformance and the spec](conformance.md): the agent-ready spec and the `rungrad score` scorer
- [Machine manifest](manifest.md): the `__rungrad_manifest` protocol for agents and the scorer
- [CLI reference](cli-reference.md): the `rungrad` command (`score`, `new`)

The written specification lives in [`../spec/`](../spec/README.md). It stands on
its own: any CLI in any language can follow it and be scored against it.
