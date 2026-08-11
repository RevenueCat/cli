# Plan — UX gaps and how we close them

Working document. Update as items land or priorities change. Phase 1 items
are pre-launch must-fix; everything else is sequenced after real internal
user feedback.

## Decisions already made

- **`--format` flag**: implement via `github.com/itchyny/gojq` (no exec dep,
  ~clean Go integration). Adds binary size but pulls in full jq semantics.
- **First-run browser open**: auto-open via `open`/`xdg-open` and also print
  the URL. Browser errors are silently ignored — URL is always printed as a
  fallback for headless contexts.
- **Pagination**: **no `--all` flag, no auto-pagination.** Keep the existing
  opt-in `--cursor <id>` model. Users who need to walk many pages will
  script it; the CLI surface stays predictable.
- **Auto-update prompts**: not planning, GoReleaser handles distribution.
- **Plugin system**: not planning.

## Phase 1 — Pre-launch must-fix

The four items that move "feels unfinished" → "feels real" for the launch audience.

### 1.1 Pretty `rc customers show` (M, 4–6h)

Replace the JSON dump with a card view in TTY mode. `--json` path unchanged.

**Acceptance:**
- Header: customer ID, last-seen platform/country, first-seen date.
- Entitlement chips: lookup keys with active/expired coloring.
- Subscriptions table: ID, product, store, status, period end.
- Purchases list (collapsed by default if >5).
- Same exact data still available via `--json`.

**Where:** new `internal/output/card.go` with a `RenderCard(spec)` API; the
spec is composable so other future detail views can reuse it.

### 1.2 ~~Implement `--format` with gojq~~ ✓ DONE

~~Currently the flag is documented but unimplemented.~~ Already implemented
in `internal/output/output.go` via `github.com/itchyny/gojq`.

**Acceptance (met):**
- ✓ `rc customers list --json --format '.data.items[].id'` works.
- ✓ Plain JSON path expressions (jq syntax).
- ✓ Errors in the expression go to stderr with a clear message; exit 2.
- ✓ `--format` is only meaningful with `--json` (warns otherwise).

### 1.3 `rc profiles` management (S, 2–3h)

Mirror `gh auth status` / `kubectl config get-contexts`. Pure config-layer
work, no new API calls.

**Acceptance:**
- `rc profiles list` — table of all profiles with active one marked.
- `rc profiles use <name>` — switches active profile (writes a small
  pointer file or env hint).
- `rc profiles delete <name>` — removes the profile file (with confirm).
- `rc profiles show [name]` — current resolved config (api-key redacted).

**Where:** `internal/cli/profiles.go` + a tiny `config.ActiveProfile` setter.

### 1.4 Actionable errors (S, 2h)

Make the most common API failures self-resolving.

**Acceptance:**
- 401 → message ends with "Your API key may be revoked. Run `rc login` again."
- 429 → surface `Retry-After` header value if present.
- 5xx → "API issue. Retry or check https://status.revenuecat.com."
- All preserved in `--json` error envelope (new fields: `hint`,
  `retry_after_seconds`).

**Where:** `internal/api/errors.go` (capture `Retry-After`),
`internal/cli/runtime.go:ExitCodeFor` (route by type), `internal/cli/run.go`
(emit `hint` field).

### 1.5 ~~Remove `--format` from help if not implemented yet~~ ✓ OBSOLETE

`--format` is already implemented; this item is no longer relevant.

---

**Phase 1 done when:** a new internal user can install, login (with URL
guidance), run `rc customers show` and read it without squinting, switch
between staging and prod profiles, and get a hint when their key is revoked.

## Phase 2 — Quality of life (after real feedback)

Sequence based on what users actually complain about. Initial guess at
priority:

### 2.1 State coloring in tables (S, 1–2h)

Per-cell styling: green=active, grey=archived/paused, red=expired/cancelled,
yellow=trial. Plumb a `CellStyleFn` into `output.Table`.

### 2.2 First-run UX (M, 3h)

- ✅ `rc login` now supports two methods: browser OAuth (PKCE + state, opens
  browser, local callback server) and API key paste. Interactive picker on bare
  `rc login`.
- ✅ Project selection removed from login. `requireProject` prompts on first
  use with a filterable picker; "Ask me every time" option clears the default.
  `rc projects use` also has the filterable picker + "ask every time" option.
- `rc init` runs `login` → `projects use` → `whoami` in sequence.
- `rc whoami` becomes richer: include resolved project name, not just ID.

### 2.3 Chart output that's actually usable (M, 4–6h)

- ✅ Interactive TUI chart viewer in TTY mode: bar/line toggle, resolution
  (daily/weekly/monthly/quarterly/yearly) + range controls via keyboard,
  braille line chart, scrollable bar chart. ETag-cached fetches.
- `--csv` flag for spreadsheet export.
- `--last 30d` / `--since YYYY-MM-DD` sugar that maps to the right
  start/end-date params.

### 2.4 Per-command tests (M, 4–6h)

Lift CLI-package coverage from 24% → ~55% with one happy-path test per
noun, using `httptest.Server` + existing fixtures.

## Phase 3 — Power features (when foundation is stable)

### 3.1 `rc metrics --watch [interval]` (S, 2h)

Re-run on interval. Respect `--no-color` and TTY detection.

### 3.2 `rc audit --follow` (S, 2h)

NDJSON-stream cursor-walking with delay. Agent-friendly.

### 3.3 `-f` as alias for `--yes` (XS)

Match `rm -f` muscle memory.

## Phase 4 — Blocked on external answers

Send one consolidated thread to whoever owns v2:

1. Query format for `/purchases/search` and `/subscriptions/search`.
2. Write-body shapes for `discounts`, `experiments`, `virtual_currencies`
   (project catalog).
3. Other sub-types under `/integrations/<type>` besides `webhooks`.
4. Sample response for `/apps/{id}/public_api_keys` (or confirm dashboard-only).
5. Public `/events` stream endpoint timeline.
6. `chat` endpoint shape (HTTP/SSE/gRPC/internal-only).

Each answer unblocks 1–3 commands in the existing roadmap (see README).

## Recommended sequencing

1. **Phase 1 in order: 1.4 → 1.3 → 1.1.** Smallest-and-most-embarrassing first; ends with the biggest UX win. (1.2 and 1.5 already complete.)
2. **Commit + push to GitHub.** Get real CI signal, get internal users on it.
3. **Collect feedback for 1 week.**
4. **Re-prioritize Phase 2** based on what actually comes up — likely a different order than what's written above.
5. **Phase 3 + 4 only after Phase 2 settles** and the open-questions thread has answers.
