# Task: expose the custom URL scheme on the API v2 App read model

Repo: `RevenueCat/khepri`. Follow-up to PR #22858 (`feat(api-v2): expose App
Store vendor number on the App read model`) — same change pattern, one new
field. Branch off #22858's branch (`api-v2-app-store-vendor-number`) if it is
not yet merged, otherwise off main, and mirror its structure commit-for-file:
spec schema, controller serialization, blueprint tests, regenerated public
spec variants.

## Problem

Every app has a "Custom URL Scheme" shown in its dashboard settings — used
for paywall preview deep links
(https://www.revenuecat.com/docs/tools/paywalls/testing-paywalls) and
Redemption Links. The dashboard's internal project-apps model exposes it as
`customUrlScheme`, but the public v2 App read model does not, so the
RevenueCat CLI and AI agents cannot fetch it.

Consumer: AI integration skills register this scheme in an app's
Info.plist / AndroidManifest and wire `presentPaywall(from:)` /
`previewPaywall(...)`. Today the agent must stop and ask the user to copy the
value from the dashboard; with this field it reads
`GET /v2/projects/{project_id}/apps/{app_id}` and proceeds.

## Desired change

Add `custom_url_scheme` (string, nullable/omitted when unset) to the v2 App
read model, on both the get and list endpoints.

- Placement: unlike #22858's field (nested under `app_store`), the scheme is
  not App Store-specific — the internal model carries it per app regardless
  of store type. Put it at the App level unless the API owners prefer
  otherwise; note the decision in the PR.
- Not sensitive: the value ships verbatim inside every app binary's
  Info.plist / manifest, so exposing it read-only leaks nothing. Read model
  only — do not add write support in this PR.
- Match the existing fields' `x-release-status` in the App schema, and
  regenerate the derived public spec variants (dev / beta / strict), exactly
  as in #22858.

## Tests

Mirror #22858's blueprint tests: scheme set → returned on get and list; never
set → omitted/null; cross-project isolation suite still passes.

## Verification

From the RevenueCat CLI once deployed:

```bash
rc apps show <app-id> --json   # response should include custom_url_scheme
```
