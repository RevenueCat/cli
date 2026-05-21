# AGENTS.md

Instructions for AI coding assistants (Claude Code, Cursor, Aider, Codex, etc.)
working in this repository. Humans should read this too — it documents the
non-obvious conventions.

## Key documents

- **[docs/command-surface.md](./docs/command-surface.md)** — the full
  command tree, naming decisions, and build order. Read before adding,
  renaming, or removing a command. Update it *before* the code.
- **README.md** — user-facing pitch + install + global flags.
- This file — agent + contributor conventions.

## What this project is

`rc` is the RevenueCat command line interface, written in Go. It wraps the
[RevenueCat v2 REST API](https://www.revenuecat.com/docs/api-v2) and is
designed to be driven equally well by humans and by LLM agents.

The rest of RevenueCat's stack is Python / Node. This is the only Go project.
Don't try to "harmonize" it with the rest of the codebase — its independence is
the point.

## The one rule that matters: two layers, hard separation

```
internal/api/   ← typed REST client, 1:1 with API endpoints. NO CLI concepts.
internal/cli/   ← user-intent commands. Composes api/ calls into UX.
```

**The CLI shape is not the API shape.** `rc customer show` calls three
endpoints to build one composite view; the `api/` package never knows that
command exists.

When you add functionality, ask: "is this a new API endpoint, or a new user
intent?" If endpoint: add a method in `internal/api/<resource>.go`. If intent:
add a command in `internal/cli/<resource>.go` that composes existing API
methods. **Do not** auto-generate commands from API endpoints. Do not mirror
REST shape in command names.

If you find yourself naming a command after the HTTP verb + path
(`rc post-customer-entitlement`), stop. The right name is the user's intent
(`rc customer grant`).

## Dual-mode contract (humans + agents)

Every command must support both:

| | Human (TTY) | Agent / script |
|---|---|---|
| Output | pretty default | `--json` (envelope: `{data, schema_version}`) |
| Missing input | `huh` prompt | error listing missing flags |
| Confirmations | inline prompt | required `--yes` |
| Errors | red + hint on stderr | `{error: {code, message, request_id, docs_url}}` |

**Hard rules:**
- Every interactive prompt MUST also be a flag and SHOULD also be an env var.
- stdout is data. stderr is chatter (spinners, hints, success messages).
- Never auto-switch output mode based on pipe detection. `--json` is opt-in.
- Confirmations under `--no-input` or non-TTY must fail with a clear "pass
  `--yes`" message, never silently proceed.
- Exit codes are stable and documented in README. Don't invent new ones without
  updating the table and `runtime.go`'s `ExitCodeFor`.

## Adding a new command

1. Check [docs/command-surface.md](./docs/command-surface.md). If the command
   is already designed, follow it. If not, update that doc first and get
   alignment before writing code.
2. Then follow the mechanical pattern below.

## Adding a new resource (mechanical pattern)

To add e.g. `offerings`:

1. **API layer** — create `internal/api/offerings.go`:
   ```go
   type OfferingsService struct{ c *Client }
   type Offering struct { ID string `json:"id"` /* ... */ }

   func (s *OfferingsService) List(ctx context.Context, projectID string) (*Page[Offering], error) { ... }
   func (s *OfferingsService) Get(ctx context.Context, projectID, id string) (*Offering, error) { ... }
   ```
   Wire it into `Client` struct + `NewClient` in `internal/api/client.go`.

2. **CLI layer** — create `internal/cli/offerings.go` with commands designed
   around user intent (not endpoint shape). Register in `root.go`.

3. **Test both layers separately.** API tests hit a recorded fixture or
   `httptest.Server`. CLI tests use `cmd.SetArgs(...)` + buffer capture.

## Don'ts

- Don't add comments that explain *what* the code does. Names should do that.
  Comments are for *why* (non-obvious constraint, surprising decision).
- Don't add error handling for scenarios that can't happen. Trust internal
  callers; validate at boundaries.
- Don't introduce premature abstractions. Three similar commands is fine; wait
  for the fourth before extracting.
- Don't add a dependency without checking if `stdlib` covers it.
- Don't auto-generate. This is a hand-crafted CLI; that's the whole pitch.
- Don't put CLI concerns (flags, prompts, color) in `internal/api/`.
- Don't put HTTP concerns (URLs, headers, retries) in `internal/cli/`.

## Conventions

- **Module path**: `github.com/revenuecat/cli`
- **Go version**: 1.25 (pinned via `mise.toml`; run `mise install` once)
- **Formatting**: `gofmt` / `go vet` clean. CI enforces.
- **Errors**: return typed `*api.Error` from the API layer. CLI layer maps to
  exit codes via `runtime.go:ExitCodeFor`. Use `errors.As`, never string match.
- **Context**: every API method takes `ctx context.Context` first. Pass
  `cmd.Context()` through.
- **Config**: lives in `~/.config/revenuecat/<profile>.json`. Env vars
  (`RC_API_KEY`, `RC_PROJECT_ID`, `RC_BASE_URL`, `RC_PROFILE`) override file.
  Flags override env. OAuth fields (`access_token`, `refresh_token`,
  `token_expires_at`) live alongside `api_key` in the same file; `BearerToken()`
  picks the right one. Tokens are silently refreshed by `Runtime.API()` when
  within 5 minutes of expiry.
- **Time**: store API timestamps as `string` (ISO 8601) in structs. Parse to
  `time.Time` only when needed for display. Avoids JSON round-trip lossiness.
- **No globals.** State flows through `cli.Runtime` on `cmd.Context()`.

## Local development

```bash
brew install mise            # only needed once
mise install                 # installs Go 1.25 from mise.toml
go mod tidy
go run ./cmd/rc --help
go test ./...
go vet ./...
```

For LLM-driven exploration of the surface:

```bash
go run ./cmd/rc commands --json    # full command tree
go run ./cmd/rc schema login       # per-command flag schema
```

## Release

Tag and push. GoReleaser handles cross-compile + Homebrew tap update.

```bash
git tag v0.1.0 && git push --tags
```

## Where things live

| Thing | File |
|---|---|
| New endpoint | `internal/api/<resource>.go` |
| New command | `internal/cli/<resource>.go` (register in `root.go`) |
| New global flag | `internal/cli/root.go` (`Globals` struct + `pf.*Var` block) |
| Exit code mapping | `internal/cli/runtime.go:ExitCodeFor` |
| Output rendering | `internal/output/output.go` |
| Interactive prompts | use `internal/tui/prompt.go` (don't call `huh` directly) |
| Interactive chart TUI | `internal/tui/chartview.go` — BubbleTea model; inject data + fetchFn |
| OAuth token flow | `internal/api/oauth.go` — PKCE helpers, token exchange, refresh |
| Profile / config | `internal/config/config.go` |
| HTTP behavior (retry, timeout, headers) | `internal/api/client.go` only |
| Release / distribution | `.goreleaser.yaml`, `.github/workflows/release.yml` |

## When in doubt

Read `README.md` for the user-facing pitch, then re-read the "two layers" rule
at the top of this file. Most design questions resolve to "which layer does
this belong in?"
