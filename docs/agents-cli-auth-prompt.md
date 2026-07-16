# Task: let the Rico HTTP API accept CLI OAuth access tokens

Repo: `RevenueCat/agents` (Python monorepo; Rico lives in `packages/rico`).

## Problem

The RevenueCat CLI (`RevenueCat/revenuecat-cli`) authenticates developers via
khepri's OAuth (`/oauth2/token`) and holds an API2 access token (`atk_…`
prefix, validated by khepri via `/auth/introspect`). That token works against
the public v2 API, but every Rico HTTP endpoint returns:

```
HTTP 401 {"detail":"Invalid or expired token"}
```

Verified against production `rico.revenuecat.com` with the token passed both
as `Authorization: Bearer <atk_…>` and as an `rc_auth_token` cookie. The same
token succeeds against `api.revenuecat.com` v2 endpoints (e.g. list projects).

## Root cause (already located — do not re-derive)

`packages/rico/src/rico/adapters/http/utils/dependencies.py::authenticate`
takes the credential (cookie `rc_auth_token`, else bearer header) and calls
`state.http_auth_client.authenticate(token)`.

`RCTokenExchangeClient.authenticate` (`packages/rico/src/rico/rc_api_client.py`,
~line 368) unconditionally runs a token exchange against khepri with:

```python
"grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
"subject_token": cookie_token,
"subject_token_type": "urn:revenuecat:rc_auth_token",
```

An `atk_…` API2 access token is not an `rc_auth_token`, so khepri rejects the
exchange, `authenticate` returns `None`, and the dependency raises the 401.

The fix is almost already present: `RCTokenExchangeClient.authenticate_access_token`
(same file, ~line 405) authenticates an already-issued API2 access token via
`/auth/introspect` and returns the same `(api2_token, introspection)` tuple.
It is simply not wired into the HTTP `authenticate` dependency.

## Desired change

In the HTTP `authenticate` dependency (or inside the auth client, whichever
fits the codebase's style better):

- If the credential is already an API2 access token, authenticate it via the
  introspection path (`authenticate_access_token`) instead of token exchange.
  Prefer detection by the `atk_` prefix; if there is an existing helper for
  that classification, use it. A reasonable alternative is: bearer-header
  credentials try introspection first and fall back to token exchange (the
  dashboard web app sends the cookie, so cookie behavior must not change).
- The returned `AuthContext` must be identical in shape: `api2_token` is the
  CLI token itself (it already works against API2, which is what
  `project_list_provider.get_projects(rc_auth_token=…)` and the AG-UI tool
  execution paths consume), `developer_id`/email/name come from introspection.
- Add caching parity if needed: the token-exchange path caches in Valkey with
  a TTL derived from `expires_in`. Introspection already has a 5-minute cache
  (`_INTROSPECT_CACHE_TTL_SECONDS`); confirm `authenticate_access_token`
  benefits from it (or add the same caching) so per-request introspection
  doesn't hammer khepri on streaming endpoints.

## Must not change

- Cookie-based dashboard auth behavior (web app) — byte-for-byte identical.
- Downstream authorization: `rico_access_level` gating, project access checks
  (`require_readable_project` / `require_writable_project`), conversation
  visibility gates, and per-developer rate limits all key off
  `AuthContext.developer_id` / `api2_token` and must keep working unchanged
  for both credential types.
- Do not mint new tokens for the CLI path; the CLI token is used as-is.

## Scope considerations (decide and document in the PR)

- Should Rico require a minimum scope on introspected CLI tokens? The CLI
  requests the full CLI OAuth scope; introspection returns `scopes`. If Rico
  has a scope convention, enforce it explicitly and return 403 (not 401) on
  insufficient scope; otherwise document that any valid developer token is
  accepted, same as a dashboard session.

## Tests

Follow the existing patterns in
`packages/rico/src/rico/test_http_auth_client.py` and
`packages/rico/src/rico/adapters/http/_test_support.py`:

- bearer `atk_…` token → introspection path → 200, correct `AuthContext`.
- bearer `atk_…` token failing introspection → 401 "Invalid or expired token".
- cookie `rc_auth_token` → still uses token exchange (assert the exchange call
  happens and introspection-first is NOT applied to cookies, if that's the
  chosen design).
- rate limiting and project-access gates behave identically under a
  CLI-token-authenticated context.

## Verification

From a machine with the RevenueCat CLI logged in (OAuth):

```bash
# Should flip from 401 to 200 after deploy:
curl -s -H "Authorization: Bearer $ATK_TOKEN" https://rico.revenuecat.com/v1/conversations
rc rico chat "hello"   # revenuecat-cli branch pr-13-astra-rico (PR #25)
```

## Out of scope (note in PR description)

Astra (`astra.revenuecat.com`, repo `RevenueCat/astra`) has the same problem
(`401 {"message":"Unauthorized."}` for CLI tokens) and will need the analogous
change; this PR covers Rico only.
