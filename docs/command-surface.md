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
   group. `POST /customers/{id}/actions/grant_entitlement` → `rc customers grant`.
3. **Archive/unarchive collapses** to `archive` + `restore` (or one verb +
   flag) — don't expose REST's symmetry as user-facing symmetry when one verb
   is the common case.
4. **Cross-resource search is top-level**, not nested under a resource group.
5. **Tail / live commands are first-class** for the observability story.
6. **Implicit project context.** `--project-id` is global, defaults to the
   profile's project, so most commands take just a resource ID.
7. **Composite views are explicit.** `rc customers show` already gets active
   entitlements for free (embedded in API response).
8. **Don't invent verbs the API can't fulfill.** Verified against fixtures.
9. **Surface tiers scope who sees what, not who can run what.** Every command
   is one of two tiers (set via the `surface` annotation, enforced in
   `internal/cli/surface.go`):
   - **default** — shown in `rc --help`, in `rc commands`/`rc schema`, and runnable.
   - **experimental** (`surface: "experimental"` annotation) — hidden from `--help`
     until the feature ships (revealed by `rc --all` / `RC_SURFACE=full`), but
     still present in `rc commands` / `rc schema` marked `"experimental": true`
     so agents can detect it and choose to skip it. Still runnable.
   Hidden never means disabled — a skill naming an experimental command still works.

## Verified API truths (from `internal/api/testdata/v2/`)

- **List envelope**: `{items: [...], next_page: string|null, object: "list", url: string}` — `next_page` is a full URL (Stripe-style), nullable when exhausted.
- **Error envelope**: `{type, message, doc_url, retryable, object: "error", param?: string}` — `parameter_error` includes a `param` field.
- **Object discriminator**: every object has an `object` field (`"project"`, `"app"`, `"customer"`, `"offering"`, `"package"`, `"product"`, `"paywall"`, `"customer.alias"`, `"audit_log"`, `"chart_data"`, `"overview_metric"`, `"chart_filter_option"`, `"error"`, `"list"`).
- **Timestamps**: **unix milliseconds** for most resources (`created_at`, `first_seen_at`, `expires_at`, etc.). **Unix seconds** on chart data (`start_date`, `end_date`, `cohort`). Audit logs include `*_iso8601` companion fields.
- **Pagination param**: `?limit=N&starting_after=<id>` (from the `next_page` URL).
- **Auth**: `Authorization: Bearer sk_...`.

## The 16 documented resources (sidebar order)

App · Audit Log · Charts & Metrics · Collaborator · Customer · Entitlement ·
Offering · Package · Product · Virtual Currency · Purchase · Subscription ·
Invoice · Paywall · Integration · Project

## Charts: fixed 22-name enum

Valid `chart_name` values:

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
# Onboarding entry points (the two scoped npx surfaces)
rc setup                                                 # one-shot agent-driven bootstrap; featured in --help/home/README, runs non-interactively for the prompt
rc setup google [app-id]                                 # interactive: local Google sign-in, bootstrap the Play service-account credential, grant package-scoped access, upload to RC
rc setup apple [app-id]                                  # interactive: App Store Connect sign-in + 2FA, create/upload IAP & ASC keys, vendor number (rc apps apple setup is a hidden alias)
rc                                                       # bare rc -> getting-started help; --all reveals experimental commands

# Auth / meta
rc auth login                                            # browser OAuth or API key; offers MCP-token import on npx runs; rc login is a hidden alias
rc auth signup                                           # browserless signup; create/generate password; optional macOS Keychain save; returns agent next steps
rc auth logout                                           # clears credentials from profile
rc auth status                                           # show auth state plus cached account identity; auth whoami and root rc whoami are aliases
rc commands                                              # agent discovery (tree)
rc schema <cmd>                                          # agent discovery (flags)
rc skills install                                        # install the core skills globally for RC-supported agents without a picker; --agent overrides
rc skills prompts                                        # show copy-ready starter prompts; --json returns prompts for agent UIs
rc open [section] [id]                                   # open the dashboard deep-linked to the active project (uses existing browser session)
rc version
rc completion <shell>

# Project / workspace
rc projects                                              # alias: list
rc projects list
rc projects show [id]
rc projects create                                      # create a project; --use saves it as active
rc projects use <id>                                     # switch profile default (interactive picker if no arg)
rc browse                                                # interactive project hub TUI (customers, offerings, apps, ...)

# Profiles (workspace switching)
rc profiles list                                         # list configured profiles (API key + default project + base URL)
rc profiles show [name]                                  # show the resolved active or named profile
rc profiles use <name>                                   # switch the active profile
rc profiles delete <name>                                # remove a profile

# Apps (per-project)
rc apps list
rc apps show <id>
rc apps create
rc apps update <id>
rc apps delete <id>
rc apps keys <app-id>                                    # typed public SDK keys for app integration
rc apps storekit-config <app-id>                         # store_kit_config; optionally writes .storekit JSON
rc apps apple check [app-id]                             # validate Apple login, 2FA, team, and key access (read-only)
rc apps apple setup [app-id]                             # hidden alias of rc setup apple (kept for back-compat)

# Customers — busiest noun
rc customers show [id]                                    # embeds active_entitlements
rc customers list
rc customers aliases <id>
rc customers attributes <id>                              # GET all subscriber attributes
rc customers set-attribute <id> <key> <value>            # set a single subscriber attribute
rc customers grant <id> <entitlement> [--duration ...]
rc customers revoke <id> <entitlement>
rc customers transfer <from> --to <id>
rc customers override-offering <id> --offering <id>
rc customers clear-override <id>
rc customers restore-google <id> --token <t>
rc customers simulate-purchase                            # Test Store only; --app-id/--product/--app-user-id + confirmation/--yes
rc customers wallet <id>                                  # virtual_currencies per-customer

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
rc offerings verify [id]                                 # graph: packages → products/prices → entitlements + paywall state
rc offerings preview [app-id]                           # SDK-eye v1 offerings response; --app-user-id/--public-api-key
rc offerings create
rc offerings update <id>
rc offerings set-current <id>                            # make this the SDK's current offering; confirmation/--yes
rc offerings delete <id>
rc offerings archive <id>
rc offerings restore <id>
rc offerings packages <offering-id>                      # nested resource
rc packages show <package-id>
rc packages create <offering-id>
rc packages update <package-id>
rc packages delete <package-id>
rc packages products <package-id>                       # list attached products and eligibility criteria
rc packages attach <package-id> <product-id> [...]
rc packages detach <package-id> <product-id> [...]

# Products
rc products list
rc products show <id>
rc products create                                        # --title supports required Test Store product titles
rc products update <id>
rc products delete <id>
rc products archive <id>
rc products restore <id>
rc products push <id>                                    # push to store
rc products prices [product-id]                         # list Test Store / Web Billing prices
rc products prices set [product-id] --price USD=9.99    # idempotently create or update Test Store prices
rc products store sync [app-id]                          # human flow: input → plan → review → confirm → apply
rc products store plan [app-id]                          # persist desired state + diff on the backend; accepts --file <path|->
rc products store list                                   # list persisted store plans
rc products store show <plan-id>                         # inspect the exact persisted plan from any process
rc products store apply <plan-id>                        # apply that same reviewed plan; requires confirmation/--yes
rc products store discard <plan-id>                      # discard without applying; requires confirmation/--yes
rc products store screenshot <plan-id>                   # manage store listing screenshots for a plan

# Paywalls
rc paywalls                                              # help; under npx a TTY shows a generate/edit picker (npm launcher sets RC_GUIDED — paywalls-only for now)
rc paywalls list
rc paywalls show <id>
rc paywalls generate [--offering-id <id>] --prompt "..." # create a paywall; standalone unless --offering-id attaches it to an offering
rc paywalls edit <paywall-id>|--session <file> --prompt  # AI-edit any paywall (draft components fetched via v2) or continue a session
rc paywalls rewind --session <file>                      # undo the last editor action
rc paywalls publish [id]                                 # publish the current draft; confirmation/--yes
rc paywalls unpublish [id]                               # remove the published paywall; confirmation/--yes
rc paywalls attach <paywall-id> <offering-id>            # attach or move a paywall to an offering (one paywall per offering); confirms when published
rc paywalls detach <paywall-id>                          # make it standalone; unpublish a published paywall first
rc paywalls delete <id> [--force]                        # attached/published paywalls refuse to delete without --force

# Rico (AI assistant)
rc rico [message]                                        # streaming chat window in a TTY (--plain for a line loop)
rc rico --continue                                       # continue the most recent conversation
rc rico -r                                               # pick a past conversation to resume (last one on top)
rc rico --print "..." --json                             # headless: one answer and exit (for scripts/agents)
rc rico "..." --approve-tools --yes --no-input           # agent mode: auto-approve tool calls (destructive need --yes)
rc rico conversations list|show <id>|delete <id>
rc rico feedback <run-id> <good|bad>

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
rc invoices for <customer-id>                            # list a customer's invoices

# Media Assets
rc media-assets list                                    # list images in the project Media Gallery
rc media-assets upload <file>                            # upload an image (jpg/png/webp/avif/heic/heif, ≤2 MiB) to the project Media Gallery

# Fonts
rc fonts list                                            # list fonts uploaded to the project
rc fonts upload <file>                                   # upload a font (ttf/otf, ≤5 MiB) to the project for use in paywalls

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

# Cross-resource
rc audit                                                 # /audit_logs with --limit, --since
```

## What's intentionally NOT here

- **No `update` on `projects`** — API doesn't expose it. Paywall draft update is
  deferred because its component schema needs a purpose-built editing UX.
- **No plugin system.** YAGNI; oclif model is a Node bet, not a Go one.
- **No `--account` flag.** Account is implicit in the API key.
- **No bulk-import** until there's a concrete ask.

## Build order

1. **`projects`** (list, show, use, create) — create is required for zero-to-project setup.
2. **`customers`** ✅ — list, composite show, grant, revoke, aliases, attributes (+ set-attribute), transfer, override-offering, clear-override, restore-google, wallet.
3. **Catalog CRUD** ✅ — entitlements (+ archive/restore/products/attach/detach), offerings (+ archive/restore), products (+ update/archive/restore/push), packages (show/create/update/delete/products/attach/detach). No `crud` helper extracted — readable enough inline.
4. **Support toolkit**: `subscriptions` ✅ (show/transactions/entitlements/management-url/cancel/extend/refund), `purchases` ✅, `invoices` ✅, `customer wallet` ✅.
5. **Long tail**: `webhooks` ✅ (under `/integrations/webhooks`), `paywalls` ✅.
6. **Cross-resource utilities**: `metrics` ✅, `charts list/show/options` ✅ (with client-side enum validation + shell completion), `audit` ✅.
7. **Apps** ✅ — list/show/create/update/delete/keys/storekit-config.
8. **Product store-state sync** — `products store sync` is the one-process
   human flow: gather desired state in memory (interactive input, a file, or
   stdin), create a server-side plan, review its diffs and warnings, then apply
   only after confirmation. Agents use the explicit `plan` → `show` → `apply`
   lifecycle so a later CLI process applies the exact backend-persisted plan ID
   it reviewed; `discard` abandons it. `--file - --input-format csv|json` avoids
   any filesystem requirement. A future `.revenuecat` workspace may provide
   optional defaults, but is never a prerequisite and desired state is never
   stored globally.
9. **Apple credential setup** — `apps apple check` validates App Store
   Connect login, trusted-device/SMS 2FA, team selection, and read-only key
   access. `apps apple setup` shows the app's current configuration, asks per
   key before creating or replacing (never silently), can create a missing App
   Store Connect app record (Developer Portal bundle ID + ASC app, like
   fastlane produce), fetches the sales-report vendor number from the Apple
   session with confirmation before setting it, and uploads everything through
   the public v2 app update endpoint. Both commands are marked
   `requires_human` in `rc schema`/`rc commands` because Apple sign-in needs a
   person with 2FA. Small Business Program dates remain out of scope because
   the public RevenueCat v2 app update schema does not expose them.

## Process

- Update this file before writing code for a new command. If a verb doesn't
  fit cleanly, the design is what needs revisiting first.
</content>
</invoke>
