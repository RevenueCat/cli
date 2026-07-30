# Task: paywall localizations subresource on API v2

Repo: `RevenueCat/khepri`. New endpoints on the developer API v2, following
the structure of recent v2 paywall PRs (#22803 publish/unpublish, #22858 App
read-model fields): spec path files + schemas, project_configuration
controller, blueprint tests, regenerated public spec variants.

## Problem

Paywall component localizations (`components_localizations`: per-locale
dictionaries of lid → localized value) can only be written through
`PATCH /v2/projects/{project_id}/paywalls/{paywall_id}`, whose body
(`PaywallDraftUpdate`) **requires** `components_config`, `revision`, and
`default_locale` alongside the localizations. A translations workflow (CSV
export → external translation tooling → CSV import) therefore has to
round-trip the entire paywall design just to change strings — the wrong risk
profile: a localization import should never be able to alter design state.

This is NOT about machine translation. The internal
`/internal/v1/developers/translations` service is out of scope. This is
storage: reading and writing localization dictionaries as a first-class
subresource.

Consumer: the RevenueCat CLI (`rc paywalls translations export/import/set`,
CSV + JSON interchange) and AI-toolkit localization workflows (Linear
DX-856; customer: Lifesum, 13 locales, own localization MCP).

## Desired endpoints

Under `/v2/projects/{project_id}/paywalls/{paywall_id}/localizations`:

1. `GET` — all locales for the draft version (fall back to published when no
   draft exists, or take `?version=draft|published`; pick one and document
   it). Response: `{ "object": "paywall_localizations", "revision": int,
   "default_locale": "en_US", "locales": { "en_US": {...}, "de_DE": {...} } }`.
2. `PUT /{locale}` — replace one locale's full dictionary. Body:
   `{ "revision": int, "values": { "<lid>": <value>, ... } }`. Creates the
   locale if new (this is how "add a language" works); 409 on stale revision,
   like the existing draft PATCH.
3. `PATCH` (bulk) — upsert multiple locales in one write:
   `{ "revision": int, "locales": { "de_DE": {...}, "fr_FR": {...} } }` with
   replace-per-locale semantics (a listed locale is replaced wholly; unlisted
   locales untouched). This is the CSV-import call.
4. `DELETE /{locale}` — remove a locale. Refuse deleting `default_locale`.

Design constraints:

- Writes target the **draft** version only, sharing the draft revision
  counter used by `PaywallDraftUpdate` so concurrent design edits and
  localization imports conflict loudly (409) instead of clobbering.
- Writes must not touch `components_config`, `default_locale`, or any other
  draft field.
- Validation to include (this is what makes the CLI workflow trustworthy):
  locale codes validated against the platform's accepted locale set; values
  must match the shapes already accepted inside `components_localizations`
  (strings and the existing rich-value objects). Decide and document whether
  keys are validated against lids referenced by the draft's
  `components_config` — if full rejection is too strict (translations may
  arrive before a design references them), return accepted-but-unreferenced
  keys in the response as a `warnings` list rather than silently accepting.
- Scope: `project_configuration:offerings:read_write` for writes (match the
  existing paywall endpoints), read scope for GET.
- Release status: match the existing paywall endpoints' `x-release-status`.

## Tests

Blueprint tests mirroring the existing paywall suites: GET round-trip after
PUT/PATCH; stale-revision 409; new-locale creation; delete + default-locale
refusal; unknown-lid warning behavior; cross-project isolation; draft
untouched fields verified (components_config byte-identical after a
localization write).

## Verification

From the RevenueCat CLI once deployed (paywall pw505aa79911c94808 in project
proj9d48bef3 has real Paywall AI-generated localizations to read):

```bash
rc api get "/projects/proj9d48bef3/paywalls/pw505aa79911c94808/localizations"
```
