# rc — RevenueCat CLI

The official command line interface for [RevenueCat](https://www.revenuecat.com).

```
rc auth login
rc customer show cus_abc
rc charts show mrr
```

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

**Direct install** — no Xcode Command Line Tools required:
```bash
curl -fsSL https://raw.githubusercontent.com/RevenueCat/revenuecat-cli/main/install.sh | sh
```

Installs to `/usr/local/bin` by default. Use `--install-dir` to change:
```bash
curl -fsSL https://raw.githubusercontent.com/RevenueCat/revenuecat-cli/main/install.sh | sh -s -- --install-dir ~/.local/bin
```

Or download a binary directly from the [releases page](../../releases).

## Updating

```bash
rc update          # download and install the latest version
rc update --check  # exit 1 if an update is available (useful in CI)
```

> **Windows:** `rc update` is not supported on Windows. Download the latest release directly from the [releases page](../../releases).

## Quick start

```bash
# 1. Log in (browser OAuth or paste an API key)
rc auth login

# Or create a new account and log in without a browser
rc auth signup

# 2. Set a default project — or choose "Ask me every time" for multi-project workflows
rc projects use

# Look up a customer
rc customer show cus_abc

# See your MRR in an interactive chart
rc charts show mrr
```

## What you can do

| Area | Commands |
|---|---|
| **Customers** | `show` · `list` · `grant` · `revoke` · `transfer` · `aliases` · `attributes` · `simulate-purchase` |
| **Subscriptions** | `show` · `cancel` · `extend` · `refund` · `transactions` |
| **Entitlements** | `list` · `show` · `create` · `update` · `attach` · `detach` |
| **Offerings** | `list` · `show` · `verify` · `preview` · `create` · `update` · `set-current` · `archive` |
| **Packages** | `list` (across all offerings) · `show` · `products` · `create` · `update` · `delete` · `attach` · `detach` |
| **Products** | `list` · `show` · `create` · `archive` · `restore` · `delete` · `store sync` |
| **Paywalls** | `list` · `show` · `create` · `generate` (AI) · `edit` (AI) · `rewind` · `publish` · `unpublish` · `delete` |
| **Rico (AI assistant)** | `chat` (streaming, tool approvals) · `conversations list/show/delete` · `feedback` |
| **Charts & metrics** | Interactive bar/line charts · daily/weekly/monthly/quarterly/yearly |
| **Apps** | `list` · `show` · `create` · `update` · `delete` · `apple check` · `apple setup` |
| **Audit log** | `rc audit` with `--limit` and `--since` |
| **Webhooks** | `list` · `show` · `create` · `update` · `delete` |

```bash
rc --help          # see everything
rc customer --help # see subcommands for any noun
```

### Verify a Test Store setup

Agents can inspect the complete offering graph, see the payload delivered to an
SDK, and create a real headless Test Store transaction without raw API calls:

```bash
rc offerings verify ofrng_default --json --no-input
rc offerings preview app_test --app-user-id demo-user --json --no-input
rc customer simulate-purchase \
  --app-id app_test --product premium_monthly --app-user-id demo-user \
  --yes --json --no-input
```

`offerings verify` includes packages, attached products and prices, matching
entitlements, paywall publication state, and an `issues` array. `offerings
preview` uses the app's public SDK key and exposes the v1 SDK response; a null
`paywall_components` value indicates fallback content rather than a published
dashboard paywall.

### Product store-state plans (experimental)

Files and repositories are optional. For a one-time human workflow, run:

```bash
rc products store sync app_abc
```

The CLI keeps interactive answers only in process memory, persists the complete
desired state and computed diff as a RevenueCat plan, displays that plan, and
asks before applying it. CSV and JSON are optional input adapters:

```bash
rc products store sync app_abc --file catalog.csv
cat desired-states.json | rc products store sync app_abc --file - --input-format json
```

Agents and CI should separate review from mutation. `plan` returns a persisted
plan ID; later CLI processes use that exact ID, so no local file or process
memory must survive between commands:

```bash
plan_id=$(cat desired-states.json |
  rc products store plan app_abc \
    --file - --input-format json --json --no-input \
    --format '.data.id')

rc products store show "$plan_id" --json --no-input
rc products store apply "$plan_id" --yes --json --no-input
# Or: rc products store discard "$plan_id" --yes --json --no-input
```

Do not rerun `plan` after reviewing it; apply the returned ID. Khepri, rather
than the local filesystem, is the durable handoff. A future `.revenuecat`
workspace may provide optional defaults, but it is never required and desired
state is never silently stored globally. `RC_STORE_STATE_FILE` can replace
`--file`. A full canonical CSV exported by Khepri is accepted:

```csv
row_type,store,store_identifier,product_type,display_name,title,duration,territory,amount,currency,start_date,available,available_in_new_territories,locale,localized_name,localized_description
price,app_store,com.example.pro_monthly,subscription,Pro Monthly,Premium Monthly,P1M,US,9.99,USD,,true,true,,,
localization,app_store,com.example.pro_monthly,subscription,Pro Monthly,Premium Monthly,P1M,,,,,,,en-US,Premium Monthly,Monthly premium access
```

These v2 endpoints are still development-only and require Khepri's
`PRODUCT_CATALOG_PRODUCT_PRICE_MANAGER` feature flag.

### Apple credential setup (experimental)

Create the missing In-App Purchase and App Store Connect API keys in your
Apple account, download each private key once, and upload it directly to an
existing RevenueCat App Store app:

```bash
rc apps apple setup app_abc
```

Test Apple sign-in, two-factor authentication, team selection, and read-only
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

rc --profile staging customer list
rc --profile prod   customer list
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

`--name` is the user's personal/display name, not a project or company name.
An agent may pass `--accept-terms` only after the user explicitly authorizes
accepting the RevenueCat Terms of Service and Privacy Policy. Add
`--marketing-emails` only when the user separately opts in.

The macOS recipe above generates a one-time password in memory and saves it as
the app.revenuecat.com internet password in the local login Keychain without
printing it or placing it in process arguments. It does not appear in Apple
Passwords or synchronize through iCloud Keychain because those stores require
app entitlements unavailable to a standalone CLI. The agent must verify that the response has
`account_created`, `authenticated`, and `password_saved_to_keychain` set to
`true`. A locked Keychain may require local user approval. A user can instead
provide `RC_PASSWORD`; avoid `--password` because command arguments can appear
in shell history and process listings. The temporary login session is discarded
after it is exchanged for renewable OAuth tokens, which are saved in the active
mode-0600 profile just like `rc auth login`.

The returned JSON tells the agent to verify the email and use the RevenueCat AI
Toolkit to configure the project, apps, products, entitlements, and offerings.
Install those maintained workflows with `rc skills install` if needed. For a
manual start, run `rc projects create --name "My App" --use`.

## Scripting and agents

Every command supports `--json` for stable, machine-readable output:

```bash
rc customer list --json | jq '.data.items[].id'
rc customer show cus_abc --json
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

RevenueCat's official AI Toolkit owns agent workflows such as project setup,
SDK integration, catalog management, and project health checks. Install its
current skills through the standard Skills CLI:

```bash
rc skills install
# equivalent to: npx skills add RevenueCat/ai-toolkit --global

rc skills install --project # opt into repository-local installation
```

`rc` delegates installation to the standard Skills CLI, which also owns future
updates, instead of shipping a second, potentially stale copy of RevenueCat
workflows. Global installation is the default so RevenueCat workflows are
available in every project. The CLI installs the four project-setup skills
for Claude Code, Codex, Cursor, Gemini CLI, and GitHub Copilot/VS Code without
showing the underlying agent or 36-skill pickers; pass `--agent` to override
the targets or `--all` when the full catalog is wanted.
`--project` creates the standard project-local skill files
and lock file instead. Global installs run in an isolated temporary directory,
so they do not add a lock file or hidden skill directory to the customer's
current repository. Marketplace installations for Codex, Claude Code, Cursor,
VS Code, and Gemini are documented at
https://www.revenuecat.com/docs/tools/overview.

Run `rc skills install` again to update an existing installation, then start a
new agent session or reload the agent so it discovers the new skill metadata.
Skills do not run when installed. The agent selects one when the user's request
matches its description. For predictable project creation, say:

To test skills from an unreleased AI Toolkit branch, set the branch for that
installation. The explicit flag overrides the environment variable:

```bash
RC_SKILLS_BRANCH=rc-cli-project-setup-workflows rc skills install
rc skills install --branch rc-cli-project-setup-workflows
```

```text
Use the create-revenuecat-project skill to make the app in this directory
RevenueCat Test Store-ready end to end, then report every production-store
stage separately.
```

Agents may also select that skill automatically for requests such as “Set up
RevenueCat for my new iOS app.” Explicit naming is useful when testing or when a
client does not reliably auto-select skills.

Run `rc skills prompts` at any time to display copy-ready starter prompts.
Bare `rc skills` only shows help; it never installs or changes anything.
Examples:

```text
Use the create-revenuecat-project skill to inspect the app in this directory,
create my RevenueCat account if needed, and finish the Test Store-ready stage
end to end: products and prices, entitlement, offering and packages, dashboard
paywall, dependencies, debug test_ key, app code, build, and simulated purchase.

Continue this app's RevenueCat setup with the Apple stage of the
create-revenuecat-project skill. Run the read-only Apple check first, then give
me the local interactive rc apps apple setup command for Apple sign-in and 2FA.

Use the revenuecat-store-state skill to plan App Store Connect products matching
the verified Test Store catalog. Wait for approval before applying that same
plan ID, then attach the Apple products and configure the release appl_ key.

Use the revenuecat-status skill to audit my RevenueCat project, identify
missing or inconsistent configuration, and give me exact recovery steps
without changing anything first.
```

The toolkit can discover every CLI contract with `rc commands --json` and
`rc schema <command> --json`. Project-setup primitives include
`rc products prices set <id> --price USD=9.99`,
`rc offerings set-current <id> --yes`, and `rc apps keys <app-id> --json`, so
an agent can finish Test Store catalog setup and return typed public SDK keys
without raw API calls.

## Global flags

| Flag | Description |
|---|---|
| `--json` | Machine-readable output |
| `--no-input` | Fail rather than prompt (for scripts/CI) |
| `--profile <name>` | Config profile (`$RC_PROFILE` or `"default"`) |
| `--api-key <key>` | Override profile key (`$RC_API_KEY`) |
| `--yes, -y` | Skip confirmation prompts |
| `--no-color` | Disable ANSI color (also respects `$NO_COLOR`) |
| `--format <expr>` | jq expression applied to `--json` output |

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
