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
| **Products** | `list` · `show` · `create` · `archive` · `restore` · `delete` |
| **Charts & metrics** | Interactive bar/line charts · daily/weekly/monthly/quarterly/yearly |
| **Apps** | `list` · `show` · `create` · `update` · `delete` |
| **Audit log** | `rc audit` with `--limit` and `--since` |
| **Webhooks** | `list` · `show` · `create` · `update` · `delete` |

```bash
rc --help          # see everything
rc customer --help # see subcommands for any noun
```

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
available in every project; `--project` creates the standard project-local skill
files and lock file instead. Marketplace installations for Codex, Claude Code,
Cursor, VS Code, and Gemini are documented at
https://www.revenuecat.com/docs/tools/overview.

The toolkit can discover every CLI contract with `rc commands --json` and
`rc schema <command> --json`.

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
