# Config and auth reference

rungrad handles the resolution shape for profiles, config paths, auth-file
paths, services, credential hooks, browser helpers, redaction of the primary
token, and exit-code mapping. Product CLIs handle file formats, login
protocols, URL derivation, and API validation.

## Precedence

Profile selection:

1. `--profile` when `ResolutionConfig.Profile` is enabled
2. `<TOOL>_PROFILE` or `ResolutionConfig.ProfileEnvVar`
3. config `current_profile`
4. `AppConfig.Profile`
5. `default`

Config path:

1. `--config`
2. `<TOOL>_CONFIG` or `ResolutionConfig.ConfigEnvVar`
3. default user config directory plus `<tool>/config.yaml`

Auth-file path:

1. `--auth-file` when `ResolutionConfig.AuthFile` is enabled
2. `<TOOL>_AUTH_FILE` or `ResolutionConfig.AuthFileEnvVar`
3. `credentials.json` beside the resolved config file

Service endpoint:

1. service flag, when `Service.Flag` is non-empty
2. service env var, when `Service.EnvVar` is non-empty
3. profile config value for `Service.ConfigKey`
4. global config value for `Service.ConfigKey`
5. `Service.Default`

Flag overrides are explicit. `--config ""` and `--auth-file ""` are usage
errors and do not fall back. A blank `--profile ""` wins and then fails profile
validation.

## Names and files

`<TOOL>` env vars are derived from `AppConfig.Name`: ASCII letters are
uppercased, digits are kept, and other bytes collapse to single underscores.
For example, `my-tool` uses `MY_TOOL_CONFIG`, `MY_TOOL_PROFILE`, and
`MY_TOOL_AUTH_FILE`.

Default non-secret config is YAML at `config.yaml` and is written `0644`.
Credentials are stored separately as JSON at `credentials.json` and written
`0600`. Both writes are atomic. Set `Store.Credentials` to point credentials at
an explicit auth file without changing `Store.Override` or the config path.

Service config lookup checks `Profile.BaseURL` for `base_url`, then
`Profile.Services[key]`, then `Profile.Defaults[key]`; global lookup checks
`Config.Services[key]`, then `Config.Defaults[key]`.

## Hooks

Use `ResolutionConfig.LoadConfig` to normalize adopter-owned config formats into
`config.Config` before generic resolution runs. A missing file is not an error.

Use `AppConfig.Auth` to provide a `CredentialResolver`. The resolver receives
`AuthContext`; use `AuthContext.Service(name)` for service endpoints and
`AuthContext.RegisterSecret` for additional secret values.

The default resolver preserves the compact behavior: credential env var, then
stored credential for the resolved profile, then `config.ErrMissingCredential`.

Use `Factory.BrowserOpener` or `testutil.Options.BrowserOpener` to inject browser
opening. Use `browser.LoginFlow` for the open-then-poll loop.

## Adopter responsibilities

Adopters handle product config and auth-file formats, browser/device-login
protocols, endpoint derivation, API validation, workspace or tenant semantics,
and registering any secret beyond `Credential.Token`.

rungrad auto-registers only the returned `Credential.Token`. Anything secret in
`Credential.Extra`, refresh tokens, echoed token fragments, or product-specific
auth metadata must be passed to `AuthContext.RegisterSecret` to be redacted at
framework output boundaries.

`config.Error` kinds exit 1, including malformed or unreadable local credential
files reported by the default resolver. `config.ErrMissingCredential` exits 3.
Adopter errors with `ExitCode() int` keep their declared code; plain errors
exit 2.
