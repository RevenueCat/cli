# Task: expose the App Store vendor number and custom URL scheme on the API v2 App read model

Repo: `RevenueCat/khepri`. Two additive read-model fields, same pattern.

## Problem

API v2's `App` response exposes only booleans for App Store credential state:

```json
"app_store": {
  "bundle_id": "com.example.app",
  "subscription_key_configured": true,
  "app_store_connect_api_key_configured": true
}
```

The vendor number (`app_store_connect_vendor_number`) is **write-only**: v2
accepts it on app create/update, but neither GET app nor list apps returns it
(or any indication that one is set).

Consumer: the RevenueCat CLI's `rc apps apple setup`
(`RevenueCat/revenuecat-cli`, branch `pr-13-astra-rico`, PR #25) shows a
status table of the app's Apple configuration before asking what to change:

```
Current Apple configuration for Moodly (App Store) (appa76509af23):
  In-app purchase key:        configured
  App Store Connect API key:  configured
```

It cannot show a vendor-number line, and its confirm prompt has to say
"replaces any previously saved value" without being able to show that value —
the CLI auto-fetches the number from Apple and may overwrite a deliberately
different one blind.

## Desired change

Add the vendor number to the v2 App read model's `app_store` object:

- Preferred: return the value itself, e.g.
  `"app_store_connect_vendor_number": "81234567"` (nullable/omitted when
  unset). A vendor number is not a secret credential — it appears on invoices
  and sales reports and is already write-accepted in plaintext — but confirm
  with the API owners; if they class it as sensitive, fall back to
  `"vendor_number_configured": true|false` for parity with the two key
  booleans.
- Apply to both `GET /v2/projects/{project_id}/apps/{app_id}` and the list
  endpoint (they share the App schema).
- Follow the existing pattern for the app_store read model: the schema lives
  in the v2 OpenAPI spec (`khepri/api/developer_api_v2/spec/...`, see the App
  schema and how `subscription_key_configured` is derived) with the
  serialization in the corresponding project_configuration controller. The
  recent publish/unpublish PR (#22803) is a good reference for the spec-file
  layout and release-status flags.
- Match the release status of the existing app_store fields (they are
  generally available in v2 responses; keep whatever `x-release-status` the
  App schema already uses).

## Second field: custom URL scheme

The dashboard's internal project-apps model exposes `customUrlScheme` (used
for paywall preview deep links per
https://www.revenuecat.com/docs/tools/paywalls/testing-paywalls and
redemption links), but the public v2 App response does not. Add
`custom_url_scheme` to the same App read model (nullable/omitted when unset,
all app types that have it). It is not sensitive — the value ships inside the
app binary's Info.plist/manifest. This lets AI agents register the scheme in
an app's project files without a dashboard round-trip.

## Scope / cautions

- Read model only — create/update input handling for
  `app_store_connect_vendor_number` already exists and must not change.
- Do not add the actual private keys or key IDs to the read model; the
  configured booleans stay as they are.
- Update the OpenAPI spec files and regenerate whatever public spec variants
  the repo derives (openapi-dev / beta / strict), same as any v2 field
  addition.

## Tests

Extend the existing v2 apps blueprint tests (see
`khepri_tests/blueprints/v2/`) with: vendor number set → returned; never set →
omitted/null; and confirm cross-project isolation tests still pass.

## Verification

From the RevenueCat CLI once deployed:

```bash
rc apps list --json   # app_store objects should now include the vendor number
```

The CLI will then render the current value in `rc apps apple setup`'s status
table (CLI-side change tracked separately in PR #25's follow-ups).
