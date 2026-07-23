# CLI design system

The CLI's look, copy, and interaction patterns come from one layered system.
Commands compose primitives; they never hand-roll colors, spacing, prompts, or
consent. Each layer only uses the layer below it, and the boundaries are
enforced by tests — bypasses fail CI, they don't wait for a human to squint at
a terminal.

```
tokens      internal/output/brand.go            colors, semantic styles
primitives  internal/output, internal/tui       one voice, one rhythm
components  Card, Table, Plan, chat, browser    assembled primitives
patterns    AGENTS.md "guided commands"         how a whole flow reads
```

## Tokens — `internal/output/brand.go`

The only file in the repo that names a color. Everything else uses the
semantic styles (`StyleTitle`, `StyleSuccess`, `StyleError`, `StyleDim`, …).

- **Brand red** (`#F2545B` dark / `#D40017` light) — identity landmarks only:
  title bars, the chat header. Never errors.
- **Error red** — failures only. Red text in a terminal means danger; the
  brand doesn't get to dilute that.
- **Accent violet** — the single interaction accent: focus, selection,
  cursors.
- Green = success, amber = warning, blue = information/notices, gray = chrome.

## Primitives

### Output (`internal/output`, on the `Renderer`)

| Primitive | Voice | Use for |
|---|---|---|
| `Title` | brand `▍` bar | section landmarks |
| `Lead` | one plain line | what a guided flow does — keep it to one sentence |
| `Field(k, v, note?)` | dim key · value · dim note | current state; the note carries per-fact education |
| `Answer(k, v)` | `✓` receipt | echo a form answer so decisions survive the vanished form |
| `Plan(steps)` | numbered | what will happen, terse and state-aware |
| `Notice(lines)` | blue `▐` bar | trust/safety statements — never dim, never skippable |
| `Success` / `Warn` / `Error` | ✓ / ! / ✗ | outcomes |
| `Info` | dim `·` | narration while working |
| `Hint` | dim | the next command to run |
| `Render(v)` | humanized fields | any API object (JSON only under `--json`) |
| `RenderTable` / `RenderCard` | | lists / result summaries |

### Interaction (`internal/tui`)

- `tui.Form(...)` and `tui.Confirm*` are the only ways to prompt. They apply
  `BrandTheme()` and print their own leading blank line — spacing is a
  property of the primitive, not a convention at call sites.
- `decide(rt, title, presetFromFlag, choices)` is the only way to make a branching choice (N options): flag preset, else TTY picker, else `--no-input` error naming every flag. Never hand-roll `tui.Select` in a command. Entity pickers use `requireID`.
- `confirmOrAbort(rt, msg)` (in `internal/cli`) is the only way to ask
  consent. It owns the `--yes` / `--no-input` / decline contract so every
  command answers those flags identically.

## Patterns

Guided flows (see AGENTS.md for the full rule): **State** (Title + Lead +
Fields) → **Decisions** (forms with `Answer` receipts) → **Plan** → one
consent **Confirm** → narrated execution → result **Card** + `Hints`.
Simple creates stay promptless.

## Enforcement

- `TestDesignSystemBoundaries` (`internal/cli/design_boundaries_test.go`) —
  colors outside the token layer, raw `huh.NewForm`, or hand-rolled
  `AssumeYes` checks fail CI. Deliberate exceptions live in an allow list
  with a written reason.
- `TestOutputSnapshots` — layout and copy of representative commands are
  locked into golden files (`UPDATE_SNAPSHOTS=1` to regenerate after
  intentional changes).
- `make preview` — renders the goldens to SVGs in `docs/previews/` for
  eyeballing before commit.

## Adding to the system

New visual need → add a primitive to `internal/output` (or a component built
from primitives), document it here, and cover it in a snapshot. If a rule is
worth stating, make it enforceable: encode it in the boundary test so it
cannot regress silently.
