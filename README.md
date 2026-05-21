# rc — RevenueCat CLI

The official command line interface for [RevenueCat](https://www.revenuecat.com).

```
rc auth login
rc customer show cus_abc
rc charts show mrr
```

## Install

```bash
brew install RevenueCat/tap/rc
```

Or download a binary from the [releases page](../../releases).

## Quick start

```bash
# Log in (browser OAuth or paste an API key)
rc auth login

# Pick a project
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
rc commands --json   # full command tree
rc schema <cmd>      # flags, args, and examples for any command
```

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

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for dev setup, testing, and how to send a PR.

For deeper conventions — architecture, agent-friendly patterns, where things live — see [AGENTS.md](./AGENTS.md).
