# rc — the RevenueCat CLI

Manage your entire [RevenueCat](https://www.revenuecat.com) integration from the
terminal — customers, subscriptions, products, paywalls, and store credentials —
built to be driven equally well by you and by AI coding agents.

- **Developers** who'd rather stay in the terminal than click through the dashboard
- **CI pipelines** — stable `--json`, `--no-input`, and predictable exit codes
- **AI agents** — full schema discovery, one-command setup, and installable skills

```
rc auth login
rc customers show cus_abc
rc charts show mrr
```

## Set up in one command

From your app's directory — no install, no dashboard clicking:

```bash
npx @revenuecat/cli setup
```

An AI agent walks the whole RevenueCat setup while you approve each step, and
emits a prompt your own coding agent can run non-interactively. Already have a
project? Point it straight at a store:

```bash
rc setup apple <app-store-app-id>      # App Store Connect: create + upload keys, vendor number
rc setup google <play-store-app-id>    # Google Play: bootstrap the service-account credential
```

Installed via Homebrew or npm, drop the `npx @revenuecat/cli` and just run
`rc setup`. Details on the store flows are in
[Store credential setup](#store-credential-setup-experimental) below.

## Install

**Homebrew** (macOS/Linux):
```bash
brew install RevenueCat/tap/rc
```

**npm / npx** — no Homebrew required; ideal for CI, agent sandboxes, and React Native projects. The package ships the same native Go binary; Node only dispatches:
```bash
npx @revenuecat/cli --help
# or install it
npm install -g @revenuecat/cli
```

Or download a binary directly from the [releases page](../../releases).

Every command also runs without installing, via `npx @revenuecat/cli <command>` —
handy for CI and agent sandboxes.

## Quick start

The fastest way in is [`rc setup`](#set-up-in-one-command) above. Prefer to do it
by hand:

```bash
# 1. Log in (browser OAuth or paste an API key)
rc auth login

# Or create a new account and log in without a browser
rc auth signup

# 2. Set a default project — or choose "Ask me every time" for multi-project workflows
rc projects use

# Set up store-side credentials (App Store Connect / Google Play)
rc setup apple app_abc

# Look up a customer
rc customers show cus_abc

# See your MRR in an interactive chart
rc charts show mrr
```

## What you can do

| Area | Commands |
|---|---|
| **Setup** | `setup` (guided) · `setup apple` · `setup google` |
| **Customers** | `show` · `list` · `grant` · `revoke` · `transfer` · `aliases` · `attributes` · `simulate-purchase` |
| **Subscriptions** | `show` · `cancel` · `extend` · `refund` · `transactions` |
| **Entitlements** | `list` · `show` · `create` · `update` · `attach` · `detach` |
| **Offerings** | `list` · `show` · `verify` · `preview` · `create` · `update` · `set-current` · `archive` |
| **Packages** | `list` (across all offerings) · `show` · `products` · `create` · `update` · `delete` · `attach` · `detach` |
| **Products** | `list` · `show` · `create` · `archive` · `restore` · `delete` · `store sync` |
| **Paywalls** | `list` · `show` · `create` · `generate` (AI) · `edit` (AI) · `rewind` · `publish` · `unpublish` · `attach` · `detach` · `delete` |
| **Rico (AI assistant)** | `rico` (streaming chat, tool approvals) · `conversations list/show/delete` · `feedback` |
| **Charts & metrics** | Interactive bar/line charts · daily/weekly/monthly/quarterly/yearly |
| **Apps** | `list` · `show` · `create` · `update` · `delete` · `apple check` |
| **Audit log** | `rc audit` with `--limit` and `--since` |
| **Webhooks** | `list` · `show` · `create` · `update` · `delete` |

```bash
rc --help          # see everything
rc customers --help # see subcommands for any noun
```

### Verify a Test Store setup

Agents can inspect the complete offering graph, see the payload delivered to an
SDK, and create a real headless Test Store transaction without raw API calls:

```bash
rc offerings verify ofrng_default --json --no-input
rc offerings preview app_test --app-user-id demo-user --json --no-input
rc customers simulate-purchase \
  --app-id app_test --product premium_monthly --app-user-id demo-user \
  --yes --json --no-input
```

`offerings verify` includes packages, attached products and prices, matching
entitlements, paywall publication state, and an `issues` array. `offerings
preview` uses the app's public SDK key and exposes the v1 SDK response; a null
`paywall_components` value indicates fallback content rather than a published
dashboard paywall.

### Product store-state plans (experimental)

Manage your App Store / Google Play catalog as a reviewable plan: describe the
desired state, review the computed diff, then apply it.

```bash
rc products store sync app_abc                       # interactive: review, then apply
rc products store sync app_abc --file catalog.csv    # from a CSV or JSON adapter
```

For CI and agents, separate review from mutation. `plan` returns a persisted
plan ID that later commands apply by reference — nothing local needs to survive
between steps:

```bash
plan_id=$(rc products store plan app_abc --file catalog.csv --json --format '.data.id')
rc products store show "$plan_id" --json             # review the diff
rc products store apply "$plan_id" --yes             # or: discard "$plan_id"
```

CSV/JSON input is optional (`RC_STORE_STATE_FILE` can replace `--file`); the
backend, not the local filesystem, is the durable handoff. This bulk import is
experimental and may not be available on every account yet.

### Store credential setup (experimental)

Set up the store-side credentials RevenueCat needs, without manual key downloads
or console clicking. Both run locally so your store credentials go straight to
Apple/Google and never through RevenueCat or a model.

**Apple (App Store Connect)** — create the missing In-App Purchase and App Store
Connect API keys, and upload them to an existing RevenueCat App Store app:

```bash
rc setup apple app_abc
```

**Google (Google Play)** — sign in locally, bootstrap the service-account
credential, grant package-scoped Play access, and upload it to RevenueCat:

```bash
rc setup google app_xyz
```

Preview Apple sign-in, two-factor authentication, team selection, and read-only
key-management access without creating keys or changing RevenueCat:

```bash
rc apps apple check app_abc
```

The command supports trusted-device and SMS verification. Your Apple Account
credentials are sent directly to Apple; they are never sent to RevenueCat or
stored by `rc`. Newly created private keys are uploaded directly to RevenueCat
and are never saved locally or printed. Interactive password entry is masked.
For scripts, prefer `RC_APPLE_PASSWORD` over `--apple-password` to avoid shell
history and process-list exposure. Pass `--vendor-number` if RevenueCat should
also receive it. Small Business Program dates are intentionally deferred.

## Profiles

Keep separate credentials for staging and production:

```bash
rc auth login --profile staging
rc auth login --profile prod

rc --profile staging customers list
rc --profile prod   customers list
```

## Agentic signup

An agent can create an account and receive renewable OAuth credentials without
opening a browser:

```bash
rc auth signup \
  --email dev@example.com \
  --name "Example Developer" \
  --generate-password \
  --save-password \
  --accept-terms \
  --no-input --json
```

Key points:

- **Consent is explicit.** Pass `--accept-terms` only after the user authorizes
  accepting the Terms of Service and Privacy Policy; add `--marketing-emails`
  only on a separate opt-in. `--name` is a personal display name, not a company.
- **Passwords stay out of the shell.** `--generate-password --save-password`
  creates a one-time password in memory and stores it in the macOS login
  Keychain without printing it. Prefer `RC_PASSWORD` over `--password`, which
  can leak via shell history and process listings.
- **Verify the result.** Check that `account_created`, `authenticated`, and
  `password_saved_to_keychain` are `true`. Tokens are saved to the active
  mode-0600 profile, same as `rc auth login`.

The returned JSON points the agent to verify the email and use the AI Toolkit
(below) to configure the project. For a manual start: `rc projects create --name "My App" --use`.

## Scripting and agents

Every command supports `--json` for stable, machine-readable output:

```bash
rc customers list --json | jq '.data.items[].id'
rc customers show cus_abc --json
RC_API_KEY=sk_... rc entitlements list --json --no-input
```

Discover the full surface programmatically:

```bash
rc commands --json       # full command tree with capabilities
rc schema <cmd>          # flags, args, and examples for any command
```

Hit any API endpoint not yet in the CLI surface:

```bash
rc api GET /projects/proj_abc/customers
rc api POST /projects/proj_abc/offerings --body '{"lookup_key":"sale"}'
```

## AI Toolkit

RevenueCat's official [AI Toolkit](https://www.revenuecat.com/docs/tools/overview)
provides agent workflows for project setup, SDK integration, catalog management,
and health checks. Install its skills through the standard Skills CLI:

```bash
rc skills install            # global; installs the core project-setup skills
rc skills install --project  # repository-local instead
rc skills install --all      # the full skill catalog
```

`rc` delegates to the Skills CLI (`npx skills add RevenueCat/ai-toolkit`) so the
workflows update in one place rather than shipping a stale copy. Global installs
run in an isolated temp dir — no lock file or hidden directory in your repo.
Re-run `rc skills install` to update, then reload your agent to pick up changes.

Skills don't run on install; the agent selects one when a request matches its
description. You can also name one explicitly:

```text
Use the create-revenuecat-project skill to make the app in this directory
RevenueCat Test Store-ready end to end, then report every production-store
stage separately.
```

Run `rc skills prompts` for more copy-ready starter prompts. Under the hood the
toolkit discovers every command with `rc commands --json` and
`rc schema <cmd> --json`, and drives typed primitives (`rc products prices set`,
`rc offerings set-current`, `rc apps keys`) instead of raw API calls.

## Global flags

| Flag | Description |
|---|---|
| `--json` | Machine-readable output |
| `--no-input` | Fail rather than prompt (for scripts/CI) |
| `--profile <name>` | Config profile (precedence: flag → `$RC_PROFILE` → `rc profiles use` → `"default"`) |
| `--api-key <key>` | Override profile key (`$RC_API_KEY`) |
| `--yes, -y` | Skip confirmation prompts |
| `--all` | Also show experimental (unreleased) commands in help |
| `--no-color` | Disable ANSI color (also respects `$NO_COLOR`) |
| `--format <expr>` | jq expression applied to `--json` output |

## Custom request headers

`RC_HEADERS` sends extra HTTP headers on every RevenueCat request (v2 API, Rico,
and the Paywall AI editor) — newline-separated `Name: Value` pairs. Use it to
route or tag traffic without baking anything into the binary:

```bash
RC_HEADERS=$'X-Some-Header: value' rc offerings list
```

One header per line; supplied headers override the CLI's defaults.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Error |
| 2 | Bad usage |
| 4 | Authentication / authorization |
| 5 | Not found |
| 6 | Rate limited |

## Releasing

Releases are fully automated via GoReleaser. To cut a release:

1. Make sure `main` is green on CI.
2. Tag the commit:
   ```bash
   git tag v0.1.0 && git push --tags
   ```
3. GitHub Actions runs GoReleaser, which:
   - Cross-compiles for macOS, Linux, and Windows (amd64 + arm64)
   - Creates a GitHub release with binaries and checksums
   - Updates the [homebrew-tap](https://github.com/RevenueCat/homebrew-tap) formula automatically

**Prerequisites** (one-time setup):
- Add a `HOMEBREW_TAP_GITHUB_TOKEN` secret to this repo with write access to `RevenueCat/homebrew-tap`

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for dev setup, testing, and how to send a PR.

For deeper conventions — architecture, agent-friendly patterns, where things live — see [AGENTS.md](./AGENTS.md).

## License

MIT — see [LICENSE](./LICENSE).
