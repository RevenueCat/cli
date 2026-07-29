# GitHub Copilot instructions

The canonical agent guide for this repository is [AGENTS.md](../AGENTS.md).
Read it before suggesting changes.

Quick reference:

- **Two layers**: `internal/api/` is a typed REST client (1:1 with endpoints,
  no CLI concepts). `internal/cli/` holds user-intent commands that compose
  API calls. Never mix.
- **Dual-mode**: every command serves humans (TTY prompts, pretty output) and
  agents (`--json`, `--no-input`, `--yes`, stable exit codes).
- **Every prompt is also a flag and an env var.** No exceptions.
- **stdout = data, stderr = chatter.** `--json` is opt-in, never auto.
- **Don't auto-generate from OpenAPI.** This is a hand-crafted CLI; that's
  the whole pitch.
- **Don't name commands after HTTP shape.** `rc customer grant`, not
  `rc post-promotional-entitlement`.
- **Errors**: typed `*api.APIError` from the API layer; CLI maps to exit codes
  via `internal/cli/runtime.go:ExitCodeFor`.

For the full guide, including the "add a new resource" recipe and the
"where things live" table, see AGENTS.md.
