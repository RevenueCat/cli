# Contributing

Contributions are welcome! Here's how to get going.

## Setup

```bash
brew install mise
git clone https://github.com/RevenueCat/revenuecat-cli && cd revenuecat-cli
mise install          # installs Go 1.25 per mise.toml
go mod tidy
make install-hooks    # installs pre-commit hook (fmt + vet + test)
make check            # verify everything passes clean
```

## Dev loop

```bash
go run ./cmd/rc --help
go run ./cmd/rc commands --json | jq    # full surface
go run ./cmd/rc schema customer grant   # per-command schema
```

Build a local binary:

```bash
go build -o /tmp/rc ./cmd/rc
/tmp/rc --version
```

## Running against the real API

Use env vars and an isolated config dir so dev runs don't touch your real profile:

```bash
export RC_API_KEY="sk_..."
export RC_PROJECT_ID="proj_..."
export RC_CONFIG_DIR="$(mktemp -d)"

go run ./cmd/rc projects list
go run ./cmd/rc customer show <id>
```

When done: `unset RC_API_KEY` and revoke the key in the dashboard.

## Running tests

```bash
make test       # go test -race ./...
make cover      # coverage report
make check      # fmt + vet + test (run before pushing)
```

Tests use scrubbed fixtures from `internal/api/testdata/v2/` — no API key needed.

## Capturing new fixtures

When the API surface changes, capture and scrub new responses:

```bash
# 1. Capture with curl
mkdir /tmp/rc-fixtures
curl -s -H "Authorization: Bearer $RC_API_KEY" \
  https://api.revenuecat.com/v2/projects/$PROJ/<path> \
  > /tmp/rc-fixtures/<name>.json

# 2. Scrub (replaces real IDs, emails, IPs with deterministic fakes)
go run ./internal/api/testdata/scrub \
  -in /tmp/rc-fixtures \
  -out internal/api/testdata/v2

# 3. Verify no leaks
git diff internal/api/testdata/v2/
grep -r "$REAL_ID" internal/api/testdata/v2/ && echo "LEAK"
```

## Make targets

| Target | What it does |
|---|---|
| `make build` | `go build ./...` |
| `make test` | `go test -race ./...` |
| `make cover` | Coverage report |
| `make fmt` | `gofmt -w` |
| `make fmt-check` | Fails if anything's unformatted (CI parity) |
| `make vet` | `go vet` |
| `make lint` | staticcheck |
| `make check` | fmt-check + vet + test |
| `make tidy` | `go mod tidy` |

## Sending a PR

1. Check [AGENTS.md](./AGENTS.md) — it documents the architecture and conventions.
2. Check [docs/command-surface.md](./docs/command-surface.md) before adding or renaming a command.
3. Open an issue first if you're unsure whether something's in scope.
4. Make sure `make check` passes before pushing.
5. Open a PR with a clear description of what changed and why.

## Architecture overview

```
cmd/rc/          entry point
internal/
  api/           typed REST client — one file per resource, no CLI concepts
  cli/           user-intent commands — composes api/ calls into UX
  config/        profile + env layering (~/.config/revenuecat/*.json)
  output/        TTY pretty-print vs --json renderer
  tui/           interactive prompts (huh) and chart viewer (BubbleTea)
```

The key rule: **`internal/api/` knows nothing about the CLI. `internal/cli/` owns all UX.** See [AGENTS.md](./AGENTS.md) for the full conventions.
