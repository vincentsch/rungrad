# rungrad documentation

rungrad is a Go framework and a language-neutral spec for command-line tools
that need stable behavior under human use, scripts, and automation.

- [Getting started](getting-started.md): install, scaffold a CLI, score a CLI
- [Building a CLI with rungrad](building-a-cli.md): the framework guide for tool authors
- [Migrating from Cobra](migrating-from-cobra.md): port an existing Cobra CLI while preserving public behavior
- [Config and auth reference](config-and-auth.md): profiles, auth files, services, and credential hooks
- [Conformance and the spec](conformance.md): the agent-ready spec and the `rungrad score` scorer
- [Machine manifest](manifest.md): the `__rungrad_manifest` protocol for agents and the scorer
- [CLI reference](cli-reference.md): the `rungrad` command (`score`, `new`)

The written specification lives in [`../spec/`](../spec/README.md). It stands on
its own: any CLI in any language can conform to it and be scored against it.
