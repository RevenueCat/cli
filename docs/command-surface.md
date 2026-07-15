# Command surface

The full `rc` command tree, design rationale, and build order. Source of truth
for what the CLI should look like. Verified against the real v2 API via
captured fixtures in `internal/api/testdata/v2/`. Update this file *before*
the code when adding/renaming a command.

## Design principles

1. **Resource grouping ≠ endpoint grouping.** Where multiple API resources are
   the same user concept, they collapse under one noun. The CLI tree reflects
   how humans think, not how the API is laid out.
2. **`/actions/foo` endpoints become verbs**, not subcommands of an `actions`
   group. `POST /customers/{id}/actions/grant_entitlement` → `rc customer grant`.
3. **Archive/unarchive collapses** to `archive` + `restore` (or one verb +
   flag) — don't expose REST's symmetry as user-facing symmetry when one verb
   is the common case.
4. **Cross-resource search is top-level.** `rc find purchase pi_123` rather
   than `rc purchases search`.
5. **Tail / live commands are first-class** for the future chat + observability
   story.
6. **Implicit project context.** `--project-id` is global, defaults to the
   profile's project, so most commands take just a resource ID.
7. **Composite views are explicit.** `rc customer show` already gets active
   entitlements for free (embedded in API response); add subscriptions +
   purchases for the full SE-debug view.
8. **Don't invent verbs the API can't fulfill.** Verified against fixtures.

## Verified API truths (from `internal/api/testdata/v2/`)

- **List envelope**: `{items: [...], next_page: string|null, object: "list", url: string}` — `next_page` is a full URL (Stripe-style), nullable when exhausted.
- **Error envelope**: `{type, message, doc_url, retryable, object: "error", param?: string}` — `parameter_error` includes a `param` field.
- **Object discriminator**: every object has an `object` field (`"project"`, `"app"`, `"customer"`, `"offering"`, `"package"`, `"product"`, `"paywall"`, `"customer.alias"`, `"audit_log"`, `"chart_data"`, `"overview_metric"`, `"chart_filter_option"`, `"benchmarks"`, `"error"`, `"list"`).
- **Timestamps**: **unix milliseconds** for most resources (`created_at`, `first_seen_at`, `expires_at`, etc.). **Unix seconds** on chart data (`start_date`, `end_date`, `cohort`). Audit logs include `*_iso8601` companion fields.
- **Pagination param**: `?limit=N&starting_after=<id>` (verified by inspecting `next_page` URL).
- **Auth**: `Authorization: Bearer sk_...`.

## The 16 documented resources (sidebar order)

App · Audit Log · Charts & Metrics · Collaborator · Customer · Entitlement ·
Offering · Package · Product · Virtual Currency · Purchase · Subscription ·
Invoice · Paywall · Integration · Project

## Resources NOT in the docs sidebar but found live

- **Discounts** — `GET /projects/{id}/discounts` returns 200 (empty for this project). Listed in audit-log permissions as `project_configuration:discounts:read_write`.
- **Experiments** — `GET /projects/{id}/experiments` returns 200 (empty). In permissions.
- **Benchmarks** — `GET /projects/{id}/benchmarks` returns 200 (`{metrics: [], object: "benchmarks"}`). In permissions.
- **Webhooks** — listed in docs as "Integration"; live path is `/projects/{id}/integrations/webhooks` (the bare `/integrations` 404s — the URL is sub-typed).

## Endpoints assumed-but-missing

| Probed | Result | Decision |
|---|---|---|
| `/projects/{id}/events` | 404 | No public events endpoint. Defer `rc events tail` until one exists. |
| `/projects/{id}/notifications` | 404 | Skip. |
| `/projects/{id}/api_keys` | 404 | API keys appear to be dashboard-only. Per-app `/apps/{id}/public_api_keys` may still work (untested with real app). |
| `/projects/{id}/purchases/search?store_purchase_identifier=foo` | 404 | Param name or shape wrong; needs docs confirmation. |
| `/projects/{id}/subscriptions/search?store_subscription_identifier=foo` | 404 | Same — needs docs confirmation. |
| `/projects/{id}/charts/{name}` for arbitrary name | 400 `parameter_error` | Chart names are a fixed enum (below). |

## Charts: fixed 22-name enum

From a `parameter_error` response, valid `chart_name` values are:

```
actives, actives_movement, actives_new, arr, churn, cohort_explorer,
conversion_to_paying, customers_active, customers_new, ltv_per_customer,
ltv_per_paying_customer, mrr, mrr_movement, prediction_explorer,
refund_rate, revenue, subscription_retention, subscription_status,
trial_conversion_rate, trials, trials_movement, trials_new
```

CLI surface: `rc charts <name>` should validate against this enum and offer
shell completion. `rc charts list` is a static command that prints these
names; there's no list endpoint.

## The tree

```
# Auth / meta
rc auth login                                            # browser OAuth or API key; rc login is a hidden alias
rc auth logout                                           # clears credentials from profile
rc auth status                                           # show auth state (method, expiry, project); rc whoami is a hidden alias
rc config                                                # show resolved config
rc commands                                              # agent discovery (tree)
rc schema <cmd>                                          # agent discovery (flags)
rc version
rc update                                                # self-update from GitHub Releases; --check exits 1 if stale (not supported on Windows)
rc completion <shell>

# Project / workspace
rc projects                                              # alias: list
rc projects list
rc projects show [id]
rc projects create                                      # create a project; --use saves it as active
rc projects use <id>                                     # switch profile default (interactive picker if no arg)
rc projects collaborators                                # list-only in API

# Apps (per-project)
rc apps list
rc apps show <id>
rc apps create
rc apps update <id>
rc apps delete <id>
rc apps keys <app-id>                                    # public_api_keys (per-app — needs verification)
rc apps storekit-config <app-id>                         # store_kit_config; optionally writes .storekit JSON
rc apps apple check [app-id]                             # POC: validate Apple login, 2FA, team, and key access
rc apps apple setup [app-id]                             # POC: create/upload missing IAP and ASC keys

# Customers — busiest noun
rc customer show [id]                                    # already embeds active_entitlements; we'll add subs + purchases
rc customer get [id]                                     # raw single endpoint
rc customer list
rc customer create
rc customer delete <id>
rc customer aliases <id>
rc customer attributes <id>                              # GET; --set k=v for POST
rc customer grant <id> <entitlement> [--duration ...]
rc customer revoke <id> <entitlement>
rc customer transfer <from> --to <id>
rc customer override-offering <id> --offering <id>
rc customer clear-override <id>
rc customer restore-google <id> --token <t>
rc customer subscriptions <id>
rc customer purchases <id>
rc customer entitlements <id>                            # active
rc customer invoices <id>
rc customer wallet <id>                                  # virtual_currencies per-customer
rc customer wallet-grant <id> <currency> --amount N      # /virtual_currencies/update_balance
rc customer wallet-tx <id> <currency> --amount N         # /virtual_currencies/transactions

# Entitlements (project catalog)
rc entitlements list
rc entitlements show <id>
rc entitlements create
rc entitlements update <id>
rc entitlements delete <id>
rc entitlements archive <id>
rc entitlements restore <id>
rc entitlements products <id>
rc entitlements attach <id> <product-id> [...]
rc entitlements detach <id> <product-id> [...]

# Offerings + packages
rc offerings list
rc offerings show <id>
rc offerings create
rc offerings update <id>
rc offerings delete <id>
rc offerings archive <id>
rc offerings restore <id>
rc offerings packages <offering-id>                      # nested resource
rc packages show <package-id>
rc packages create <offering-id>
rc packages update <package-id>
rc packages delete <package-id>
rc packages attach <package-id> <product-id> [...]
rc packages detach <package-id> <product-id> [...]

# Products
rc products list
rc products show <id>
rc products create
rc products update <id>
rc products delete <id>
rc products archive <id>
rc products restore <id>
rc products push <id>                                    # push to store
rc products prices list <id>                            # list configured product prices
rc products prices add <id> --price USD:9.99            # add prices (test-store only); conflicts if currency exists
rc products prices update <id> --price USD:12.99        # update an existing price (no delete supported)
rc products store sync [app-id] --file <catalog.csv>     # plan/review/apply canonical store-state CSV; --plan-only stops before apply

# Paywalls
rc paywalls list
rc paywalls show <id>
rc paywalls create
rc paywalls delete <id>                                  # no update in API yet

# Subscriptions
rc subscriptions show <id>
rc subscriptions cancel <id>                             # Web Billing only
rc subscriptions extend <id> --by <duration>
rc subscriptions refund <id>                             # Web Billing only
rc subscriptions transactions <id>
rc subscriptions entitlements <id>
rc subscriptions management-url <id>

# Purchases
rc purchases show <id>
rc purchases entitlements <id>
rc purchases refund <id>                                 # Web Billing only

# Invoices
rc invoices show <id>

# Virtual currencies (project catalog; per-customer lives under `customer wallet`)
rc currencies list
rc currencies show <id>
rc currencies create
rc currencies update <id>
rc currencies delete <id>
rc currencies archive <id>
rc currencies restore <id>

# Discounts (discovered live; not yet in docs sidebar)
rc discounts list
rc discounts show <id>
rc discounts create                                      # if API supports it (TBD)
rc discounts delete <id>

# Experiments (discovered live; not yet in docs sidebar)
rc experiments list
rc experiments show <id>
rc experiments results <id>                              # path TBD — /experiment_results 404'd

# Webhooks (under /integrations/webhooks)
rc webhooks list
rc webhooks show <id>
rc webhooks create
rc webhooks update <id>
rc webhooks delete <id>

# Charts & metrics
rc metrics                                               # overview (project-wide KPIs)
rc charts list                                           # static enum, prints the 22 names
rc charts show <name> [--filter k=v --segment ...]      # interactive TUI in TTY; --json for raw data
rc charts options <name>                                # GET /charts/{name}/options
rc benchmarks                                            # GET /benchmarks

# Cross-resource
rc find <type> <store-id>                                # purchases/subscriptions search (path TBD)
rc audit                                                 # /audit_logs with --limit, --since

# Future
rc events tail [--filter ...]                            # NO public endpoint yet
rc chat                                                  # internal agent chat
```

## Naming decisions worth revisiting

| Decision | Reasoning |
|---|---|
| `wallet` for per-customer virtual currencies | "Wallet" is the natural user term. |
| `currencies` (not `virtual-currencies`) for project catalog | Short, unambiguous in context. |
| `webhooks` (not `integrations`) | Webhooks is what users say; `integrations` is internal. Implementation hits `/integrations/webhooks`. |
| `restore` (not `unarchive`) | Matches `gh`, `stripe`. |
| `archive` + `restore` (separate verbs) | Each is an intent. |
| `customer show` (composite) vs `customer get` (raw) | Most users want composite; raw is available for scripted workflows. |
| `projects use` for switching | Matches `kubectl config use-context`. |
| No global `--account-id` | API key already scopes to account. |
| `find` as top-level | Search precedes knowing the resource type. |
| `charts show <name>` not `charts <name>` | Leaves room for `charts list`, `charts options`. |

## What's intentionally NOT here

- **No `update` on `projects` or `paywalls`** — API doesn't expose them.
- **No plugin system.** YAGNI; oclif model is a Node bet, not a Go one.
- **No `--account` flag.** Account is implicit in the API key.
- **No bulk-import** until there's a concrete ask.
- **No public `/events` stream.** Endpoint doesn't exist; `rc events tail` deferred.

## Build order

1. **`projects`** (list, show, use, create) — create is required for zero-to-project bootstrap.
2. **`customers`** ✅ — list, composite show, grant, revoke, aliases, attributes (+ set-attribute), transfer, override-offering, clear-override, restore-google, wallet.
3. **Catalog CRUD** ✅ — entitlements (+ archive/restore/products/attach/detach), offerings (+ archive/restore), products (+ update/archive/restore/push), packages (show/create/update/delete/products/attach/detach). No `crud` helper extracted — readable enough inline.
4. **Support toolkit**: `subscriptions` ✅ (show/transactions/entitlements/management-url/cancel/extend/refund), `purchases` ✅, `invoices` ✅. `customer wallet` still TODO.
5. **Long tail**: `webhooks` ✅ (under `/integrations/webhooks`), `paywalls` ✅. `currencies` catalog, `discounts`, `experiments` deferred — fixtures empty so write shapes unverified.
6. **Cross-resource utilities**: `metrics` ✅, `charts list/show/options` ✅ (with client-side enum validation + shell completion), `benchmarks` ✅, `audit` ✅. `find` still TODO (search query format unconfirmed).
7. **Apps** ✅ — list/show/create/update/delete/keys/storekit-config.
8. **Apple credential setup POC** — `apps apple check` validates App Store
   Connect login, trusted-device/SMS 2FA, team selection, and read-only key
   access. `apps apple setup` creates one-time downloadable In-App Purchase and
   App Store Connect API keys and uploads them through the public v2 app update
   endpoint. Vendor number remains an optional setup flag because Apple exposes
   no supported discovery endpoint. Small Business Program dates remain out of
   scope because the public RevenueCat v2 app update schema does not expose them.
9. **Product store-state CSV sync POC** — `products store sync` reads Khepri's
   canonical CSV format locally, creates a product store-state plan, waits for
   its preview, displays per-product diffs and warnings, and only applies after
   confirmation. `--plan-only` leaves the reviewed plan unapplied. This surface
   uses development-only v2 endpoints and requires the
   `PRODUCT_CATALOG_PRODUCT_PRICE_MANAGER` feature flag until they ship.
10. **Support toolkit**: `subscriptions`, `purchases`, `invoices`, `customer wallet`. The SE-debug surface.
11. **Long tail**: `webhooks` (via `/integrations/webhooks`), `paywalls`, `currencies` catalog, `discounts`, `experiments`.
12. **Cross-resource utilities**: `find`, `audit`, `metrics`, `charts`, `benchmarks`.
13. **Streaming**: `events tail`, then `chat` — both blocked on backend.

## Open questions (need RevenueCat-internal answers)

1. **Webhook integration path** — confirmed `/integrations/webhooks`; are there other integration sub-types (Slack, S3, Segment, etc.) under `/integrations/<type>`?
2. **`/purchases/search` and `/subscriptions/search`** — what's the actual query format? Both 404'd with the param name the docs implied.
3. **`/api_keys`** — managed in dashboard only, or is there a public per-project endpoint?
4. **Apps `/public_api_keys`** subpath — confirm shape with a real app fixture.
5. **`/experiment_results`** — docs imply it exists but 404'd. Maybe `/experiments/{id}/results`?
6. **Discounts** — the audit log mentions both `discounts` and `promo_codes` semantics. Same thing or two surfaces?
7. **Chart timestamps** — confirmed unix seconds (not millis like the rest). Document on the model page.

## Process

- Update this file before writing code for a new command. If a verb doesn't
  fit cleanly, the design is what needs revisiting first.
- When the API ships a new endpoint, capture a real fixture into
  `/tmp/rc-fixtures`, run the scrubber, commit, then update this doc.
