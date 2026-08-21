# Design proposal: a human-first "guided" tier for setup commands

Status: proposal, agreed in principle. Not built yet.
Scope: the `requires_human` commands only — `setup`, `setup google`, `apps apple
setup`/`check`, `auth login`/`signup`, `capital setup`. Data commands are
untouched.

## Why

The CLI's design system optimizes every command for "humans and agents,
equally" — terse stderr chatter, `--json`-first, dual-mode branching. That's
right for data commands (`customers`, `products`), which agents really do run.

It's wrong for the setup flows. They all carry `requires_human: "true"` — an
agent *cannot* run them (browser, 2FA, Play Console admin). Forcing them to obey
the agent-friendly terseness optimizes for a user who is never there. The result
(felt firsthand building `rc setup google`): a flat column of dim `·` lines with
no hierarchy, URLs you hunt-and-copy, and raw provider errors. Hard to grok.

The `requires_human` flag should **license a richer UX tier** — treat these like
a great interactive installer — not just be metadata.

## What the best CLIs actually do (research, Aug 2026)

Surveyed the browser-auth and multi-step-wizard UX of Stripe, GitHub, Vercel,
Netlify, Supabase, Sentry, Fly, Railway, Doppler, PlanetScale, Turso, Heroku,
Firebase, gcloud, AWS, Prisma, Convex, EAS, and the create-* scaffolders.

Decision-driving findings:

- **"Press Enter to open (or visit `<URL>`)" is the universal auth pattern.**
  Stripe, GitHub, Railway, Heroku all pause on a keypress before opening a
  browser, and *always print the URL inline* as the escape hatch. We already do
  this — keep it.
- **Nobody auto-copies to the clipboard.** Even `gh` is only adding it as
  opt-in. → don't auto-copy.
- **Nobody offers an `[o]pen/[c]opy` keypress menu.** → cut it; unproven.
- **Clickable OSC 8 hyperlinks are free and safe.** Supporting terminals make
  them clickable; others show plain text. We added this — keep it.
- **No CLI uses a "Step 3 of 6" numeric stepper**, and none use a full-screen
  alt-screen TUI for setup. → no step counter.
- **The one "checklist filling in" pattern that exists is the spinner→checkmark
  task line** (ora / Listr2 / Convex / EAS): a spinner while a step runs,
  resolving in place to `✔`/`✖` and persisting. This is the ledger.
- **The modern aesthetic bar is the clack left rail** (`│` with `◇◆●` glyphs) —
  create-vite/nuxt/t3 and the Sentry wizard. Cheap visual containment.
- **Fly's `fly launch` plan-with-rationale + single `y/N`** is the best-liked
  confirmation model: a column-aligned summary where each defaulted value
  justifies itself in a parenthetical, then one confirm.
- **Prisma's structured errors** (stable code + one-line cause + one-line fix,
  abort-and-hand-off on danger) are the friendliness gold standard.
- **In-place retry that keeps prior progress is unheard of** — every surveyed
  CLI aborts and hands off a command. We already did it (the Android Publisher
  ToS loop). This is a genuine differentiator; generalize it.
- **Sentry's wizard writes the secret to config itself** so the human never
  copy-pastes a credential — exactly the goal of the RC credential-upload API
  gap (DX-985).

## The tier

Target rendered output (TTY):

```
◇  Connect Google Play

   ✔ Signed in             mostlygoodllc@gmail.com
   ✔ Google Cloud project  oops-laundry
   ✔ Required APIs
   ⠹ Service account       creating…
   ○ Play access
   ○ Credentials
```

Primitives (a new `internal/guided` layer, or an extension of `internal/output`
+ `internal/tui`):

1. **Ledger** — the spinner→`✔`/`✖`/`○` task list, repainted in place. Replaces
   the flat `·` narration stream.
2. **Rail + section glyph** — the clack `│`/`◇` containment for visual hierarchy.
3. **Plan-with-rationale → one confirm** — each line explains its default in a
   parenthetical; a single consent before any mutation. (Extends today's Plan.)
4. **Action line** — "Press Enter to open `<url>` (or copy it) · ^C to cancel",
   URL always printed, rendered as a clickable OSC 8 link. No auto-copy, no
   `[o]/[c]` menu.
5. **Friendly error block** — cause + fix + clickable action, abort-and-hand-off
   on danger; raw provider error only under `--verbose`.
6. **Idempotency affordance** — "found existing" / `[bracketed default]`; never
   silently clobber.
7. **Retry-in-place** — on a recoverable step failure, guide + wait + retry
   without discarding prior progress (generalize the ToS loop).

## Rendering

**bubbletea in *inline* mode** (no `WithAltScreen`): it repaints only the ledger
region and leaves scrollback intact — not a full-screen TUI. `bubbles/spinner`
for the live glyph, `lipgloss` for style, `huh` for the ask-the-user steps
between phases. **All of these are already dependencies** — no new heavy deps.

Fallback: plain append-only lines (`[ok]`/`[..]`/`[FAIL]`) on non-TTY / CI /
`--plain` / `NO_COLOR` / `TERM=dumb`. Detect via `x/term.IsTerminal` + `CI` env.

Since these commands are TTY-only by definition, the guided code can **drop the
`--json`/`--no-input`/`CanPrompt` dual-mode branching** entirely — simpler *and*
nicer.

## Scope & enforcement

- Applies only to `requires_human` commands.
- A `design_boundaries_test` rule keeps `guided` primitives out of data commands
  and keeps data-command terseness out of guided flows. No drift.

## Build plan (small PRs)

1. **guided primitive package** — ledger (bubbletea inline) + rail + plain
   fallback + TTY detection. Snapshot-tested.
2. **action + link + friendly-error primitives** — the "press Enter to open"
   line, OSC 8 (already have `Link`), Prisma-style error block.
3. **Pilot: refit `rc setup google`** onto the tier end-to-end. This is the
   proving ground.
4. **Refit `apps apple setup`/`check`** and **`auth login`/`signup`**.
5. **Refit `rc setup`** (the onboarding entry point).
6. **design-system.md** — document the tier; add the boundary-test rule.

## Non-goals

- No full-screen alt-screen TUI.
- No change to data commands or their dual-mode contract.
- No auto-copy-to-clipboard by default; no `[o]/[c]` keypress menu.
