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
| **Customers** | `show` · `list` · `grant` · `revoke` · `transfer` · `aliases` · `attributes` |
| **Subscriptions** | `show` · `cancel` · `extend` · `refund` · `transactions` |
| **Entitlements** | `list` · `show` · `create` · `update` · `attach` · `detach` |
| **Offerings** | `list` · `show` · `create` · `update` · `archive` |
| **Packages** | `list` (across all offerings) · `show` · `create` · `update` · `delete` · `attach` · `detach` |
| **Products** | `list` · `show` · `create` · `archive` · `restore` · `delete` · `store sync` |
| **Charts & metrics** | Interactive bar/line charts · daily/weekly/monthly/quarterly/yearly |
| **Apps** | `list` · `show` · `create` · `update` · `delete` |
| **Audit log** | `rc audit` with `--limit` and `--since` |
| **Webhooks** | `list` · `show` · `create` · `update` · `delete` |

```bash
rc --help          # see everything
rc customer --help # see subcommands for any noun
```

### Store-state CSV sync (experimental)

Preview a canonical Khepri product store-state CSV without changing Apple or
RevenueCat products:

```bash
rc products store sync app_abc --file catalog.csv --plan-only
```

The command parses the file locally, creates a RevenueCat store-state plan,
waits for Khepri to compare it with the live store, and returns every diff and
warning. Remove `--plan-only` to review the same preview and confirm before it
is applied:

```bash
rc products store sync app_abc --file catalog.csv
```

For CI, use `--yes --no-input --json`. `RC_STORE_STATE_FILE` can replace
`--file`. A full CSV exported by Khepri is accepted; this is a minimal App Store
example:

```csv
row_type,store,store_identifier,product_type,display_name,title,duration,territory,amount,currency,start_date,available,available_in_new_territories,locale,localized_name,localized_description
price,app_store,com.example.pro_monthly,subscription,Pro Monthly,Premium Monthly,P1M,US,9.99,USD,,true,true,,,
localization,app_store,com.example.pro_monthly,subscription,Pro Monthly,Premium Monthly,P1M,,,,,,,en-US,Premium Monthly,Monthly premium access
```

These v2 endpoints are still development-only and require Khepri's
`PRODUCT_CATALOG_PRODUCT_PRICE_MANAGER` feature flag.

## Profiles

Keep separate credentials for staging and production:

```bash
rc auth login --profile staging
rc auth login --profile prod

rc --profile staging customer list
rc --profile prod   customer list
```

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

## Skills

Skills are step-by-step workflow guides for common multi-step tasks. Read
them directly or install them as Claude Code slash commands:

```bash
rc skills list                    # see available skills
rc skills show setup-offering     # read a skill
rc skills install                 # write all to .claude/commands/ in current repo
rc skills install --global        # write to ~/.claude/commands/ for all projects
```

Once installed, skills are available as `/project:rc-<name>` slash commands
in Claude Code. Commit `.claude/commands/` to share them with your whole team.

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
