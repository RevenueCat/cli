# rc — RevenueCat CLI

A CLI for [RevenueCat](https://www.revenuecat.com) designed for humans **and**
AI agents.

```
brew install RevenueCat/tap/rc   # (future)
rc login
rc customer show cus_abc
```

## Docs

- [docs/command-surface.md](./docs/command-surface.md) — full command tree,
  naming decisions, and build order.
- [docs/cookbook.md](./docs/cookbook.md) — common workflows and pipelines.
- [AGENTS.md](./AGENTS.md) — conventions for humans and AI agents working
  in this repo.

## Design

Two layers, hard separation:

```
cmd/rc/                  # entry point
internal/
  cli/                   # user-intent commands (designed for humans + agents)
    root.go              # cobra root, global flags, runtime injection
    run.go               # final error formatting (JSON envelope under --json)
    login.go
    customers.go         # `rc customer show/grant` — composes multiple API calls
    schema.go            # `rc schema <cmd>` / `rc commands` for agent discovery
    runtime.go           # exit codes, context plumbing
    ... (one file per resource)
  api/                   # typed REST client (1:1 with v2 API)
    client.go            # do() + stream() chokepoints
    errors.go            # stable error codes -> CLI exit codes
    customers.go         # one file per resource
    ... (17 services)
  config/                # profile + env layering (~/.config/revenuecat/*.json)
  output/                # pretty TTY vs --json renderer (stdout=data, stderr=chatter)
  tui/                   # huh wrapper: every prompt also a flag, fails clean under --no-input
```

The CLI shape is **not** a mirror of the API. `rc customer show` composes the
customer record + active entitlements + subscriptions + purchases into one
user-intent view. The `api/` package stays REST-shaped; the `cli/` package
owns UX.

## Dual-mode contract

| | Human (TTY) | Agent / script |
|---|---|---|
| Output | pretty tables, colors, spinners | `--json` (stable envelope: `{data, schema_version}`) |
| Missing input | interactive `huh` form | clean error listing missing flags under `--no-input` |
| Confirmations | inline prompt | required `--yes` |
| Discovery | `rc <cmd> --help` | `rc commands --json`, `rc schema <cmd>` |
| Errors | red message + hint on stderr | structured `{error:{type,message,exit_code,request_id,doc_url}, schema_version}` |
| Streams | live tail | NDJSON (one event per line) |

Every prompt is also a flag and env var. Three ways to drive any command:

```bash
# interactive
rc customer grant

# explicit flags
rc customer grant --customer-id cus_x --entitlement-id pro --duration monthly --yes

# env / scripted
RC_API_KEY=sk_... rc customer grant --customer-id cus_x --entitlement-id pro --duration monthly --yes --json
```

## Agent-friendly surface

Built in from day one — no scraping required:

```bash
rc commands --json     # full command tree with aliases
rc schema <cmd>        # per-command: positional args, flags, aliases, examples, subcommands
rc version --json      # machine-readable version
rc <cmd> --json        # data on stdout, status helpers silent
rc <cmd> --no-input    # fail rather than prompt
rc <cmd> --yes         # skip confirmations
```

Errors in `--json` mode emit the same envelope shape as the v2 API, so one
parser handles both transport errors and CLI errors:

```json
{
  "error": {
    "type": "resource_missing",
    "message": "...",
    "exit_code": 5,
    "request_id": "fc53f50e-...",
    "doc_url": "https://errors.rev.cat/resource-missing"
  },
  "schema_version": 1
}
```

## Global flags

```
--json              machine-readable output (stable schema)
--no-input          fail rather than prompt for missing inputs
--profile <name>    config profile (default: $RC_PROFILE or "default")
--api-key <key>     override profile API key (or set RC_API_KEY)
--format <jsonpath> projection applied to --json (planned)
--yes, -y           assume yes for confirmations
--quiet, -q         suppress non-essential stderr
--verbose, -v       extra logging
--no-color          disable ANSI (also respects NO_COLOR)
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | generic error |
| 2 | usage error |
| 4 | authentication / authorization |
| 5 | not found |
| 6 | rate limited |

## Development

```bash
brew install go
make check               # gofmt + vet + race tests
make cover               # coverage report
go run ./cmd/rc --help
```

## Roadmap

Tracked in [docs/command-surface.md](./docs/command-surface.md) — this is the
short version of what's intentionally not done yet.

**Deferred — needs RevenueCat-internal answers:**
- `rc find <type> <store-id>` — the v2 search endpoints (`/purchases/search`,
  `/subscriptions/search`) return 404 with the param names the docs imply.
  Need the actual query format.
- `rc discounts` and `rc experiments` create/update — endpoints exist but
  fixtures are empty, so write-body shapes aren't verified.
- `rc currencies` (project-level virtual currency catalog) — same situation.
- Integration sub-types beyond `/integrations/webhooks` — confirm what else
  lives under `/integrations/<type>`.
- `rc apps keys <id>` — shape unverified (didn't capture a fixture to avoid
  leaking the test project's public API keys).

**Deferred — blocked on backend:**
- `rc events tail` — no public events stream endpoint yet.
- `rc chat` — internal agent chat experience; design + endpoint TBD.

**Nice-to-have polish:**
- Generated per-command reference docs (`cobra gendocs` → `docs/reference/*.md`).
- `Long:` + `Example:` on the ~30 mechanical CRUD leaves. Currently only the
  intent commands have them; mechanical leaves are self-documenting from name
  and flags.
- Per-command happy-path tests via `httptest.Server` to lift CLI coverage
  past the current contract-only level.
- Wire `--format` jsonpath projection (flag is parsed but the evaluator is a
  TODO in `internal/output/output.go`).

**Not planned:**
- Plugin system (oclif-style). Go's distribution model doesn't reward it,
  and no concrete partner ask exists.
- Auto-generated commands from the OpenAPI spec. The hand-crafted UX (composite
  `customer show`, renamed verbs like `wallet`/`webhooks`, charts client-side
  enum) is the whole pitch.

## Release

Tag and push; GitHub Actions runs GoReleaser:

```bash
git tag v0.1.0 && git push --tags
```

Outputs cross-compiled binaries (darwin/linux/windows × amd64/arm64) and
updates the `RevenueCat/homebrew-tap` formula.
