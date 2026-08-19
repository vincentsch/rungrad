# Name resolution

## Criterion

Where an API uses opaque identifiers, a tool accepts human names too. When a name
is ambiguous it disambiguates interactively on a terminal, and under `--no-prompt`
or `--json` it never blocks: it fails with the candidates listed so a caller
chooses deterministically.

## Rationale

Humans think in names, not IDs. Agents cannot answer an interactive prompt, so a
tool that blocks waiting for input hangs the automation. Returning the candidate
set instead lets a non-interactive caller pick without a human.

## Testable assertions

- `resolution.no-prompt` (required): resolving an ambiguous name with
  `--no-prompt` exits non-zero without blocking, and reports the candidates
  rather than hanging on a prompt.
