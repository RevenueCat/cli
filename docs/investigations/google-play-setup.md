# Investigation: automating Google Play setup from `rc` (Go CLI)

Status: investigation / feasibility. No command shipped yet.
Goal: an `rc setup google` flow that mirrors `rc apps apple setup` — the developer
authenticates with Google **locally**, and the CLI bootstraps the machine
credential RevenueCat needs without RevenueCat ever seeing human Google
credentials.

**Scope note (v1):** the thing being deleted is the **service-account JSON
credential dance**. RTDN/Pub/Sub is explicitly **out of v1 scope** — it carries
an unavoidable Console-only manual step and isn't needed for the credential
flow. It's parked as a follow-up (§13). The single hard gate for v1 is a
RevenueCat backend change so API v2 can *receive* the credential (§12).

Linear: parent **DX-979**, sub-tasks DX-980…DX-989.

All Google-API facts below were verified against official docs in August 2026
(citations at the end of each section). Two of them move fast (`x/oauth2`,
`google.golang.org/api` are both pre-1.0) — pin versions in `go.mod`.

---

## TL;DR verdict

**Most of it is automatable. Three things are not, and one of them is a real
release blocker.**

| Blocker | Nature | Severity |
|---|---|---|
| **Play developer account ID** cannot be discovered from any API | Hard Google gap | Medium — fall back to paste/parse the Console URL |
| **Pointing the Play app at the RTDN Pub/Sub topic** is Play-Console-only | Hard Google gap | Medium — print the topic string + deep-link |
| **RC API v2 cannot upload a Play service-account credential** | RevenueCat gap (ours to fix) | High — needs a backend/API change before the flow can end without "now go paste the JSON" |
| **100-user lifetime cap** on an unverified OAuth client with these scopes | Google verification | High for GA — needs sensitive-scope verification (not CASA) |

Everything else — OAuth, project discovery, enabling APIs, creating the service
account, IAM, the in-memory JSON key, adding the service account to Play,
package-scoped Play grants, Pub/Sub topic + publisher IAM — is doable natively
in Go with official libraries.

**Biggest architectural difference from Apple:** the Apple flow is *not* OAuth.
`internal/appleconnect` reimplements the App Store Connect web login (SRP +
2FA, like fastlane's spaceship) against private `idmsa`/`olympus`/`iris`
endpoints. Google is the opposite — it has first-class OAuth and official Go
SDKs — so the *auth* half is cleaner, but Google has hard API gaps Apple
doesn't, and our own API can't yet receive the credential.

---

## How this compares to the Apple flow (what we can reuse)

- **Localhost OAuth is already a solved pattern in this repo.** `rc auth login`
  (`internal/cli/auth.go:551` `loginWithOAuth`) already does PKCE + a
  `net.Listen("tcp", "localhost:0")` callback server + `openBrowser`. The
  Google flow is the same shape with `golang.org/x/oauth2` + `google.Endpoint`
  swapped in for our hand-rolled `api.OAuthService`.
- **The credential-upload shape is the model to copy.** Apple creates keys and
  pushes them via `rc.Apps.Update` with `AppStoreAppConfig`
  (`internal/cli/apps_apple.go:639-664`). Play needs the equivalent — but the
  field doesn't exist yet (see §12).
- **The UX pattern is fixed by the design system.** State (`Title`/`Lead`/
  `Field`) → decisions (`Answer` receipts) → `Plan` → one `Confirm` → narrated
  `Info` execution → result `Card` + `Hint`. `internal/appleconnect` is
  service-only (no CLI concepts); mirror that with `internal/google`.

---

## Feasibility per step

### 1. Local Google authentication — ✅ feasible

**Flow: Desktop-app OAuth client + loopback (`127.0.0.1:PORT`) redirect +
auth-code + PKCE.** This is the flow Google still supports for desktop clients
(`gcloud`, `gh` ship the same way).

- **OOB (`urn:ietf:wg:oauth:2.0:oob`) is dead** — blocked for new clients since
  Feb 2022, shut off Jan 2023. Do not use.
- **Device flow** exists but is for browserless devices — not a desktop CLI.
- Use `127.0.0.1` (or `[::1]`), **not** `localhost` — Google notes `localhost`
  can trip client firewalls. Ephemeral port chosen at runtime.

**Scopes (request the minimum):**

| Operation | Scope |
|---|---|
| Play users/grants + (later) app listing | `https://www.googleapis.com/auth/androidpublisher` |
| GCP: projects, service usage, IAM, Pub/Sub | `https://www.googleapis.com/auth/cloud-platform` |
| App listing via Play Developer Reporting (optional, §11) | `https://www.googleapis.com/auth/playdeveloperreporting` |

`cloud-platform` is broad; Google review flags asking for it when narrower
scopes suffice. If we only ever touch service usage / IAM / resource manager /
pubsub we *can* enumerate narrower scopes, but `cloud-platform` keeps the code
simple and is still "sensitive," not "restricted." Decision to make at
verification time.

**Shipping the client ID in a public binary — allowed and normal.** For a
Desktop client the "client secret" is explicitly *not* treated as confidential
(that's what PKCE is for). Embedding client ID + secret in the binary is the
intended model.

**Verification burden (the real cost):**
- Both `androidpublisher` and `cloud-platform` are **sensitive**, not
  **restricted**. → Google brand/scope verification (consent screen, logo,
  homepage, privacy policy, domain ownership, scope justification, **demo
  video**). **No CASA third-party security assessment** (that's restricted-only:
  Gmail/Drive/Fitness-type consumer data).
- **Until verified, the project is capped at 100 users for its entire lifetime
  — not resettable.** Fine for a prototype/beta; a blocker for GA. Start
  verification early; it takes weeks.

**Go libraries:**

| Package | Version (Aug 2026) | Use |
|---|---|---|
| `golang.org/x/oauth2` | v0.36.0 | PKCE + token exchange |
| `golang.org/x/oauth2/google` | (same module) | `google.Endpoint`, ADC helpers |
| `google.golang.org/api/option` | v0.293.0 | `option.WithTokenSource(ts)` |

PKCE helpers exist; the loopback server is hand-rolled (small, and we already
have one in `auth.go`):

```go
verifier := oauth2.GenerateVerifier()
conf := &oauth2.Config{
    ClientID:     clientID,
    ClientSecret: clientSecret, // non-confidential for desktop
    Endpoint:     google.Endpoint,
    RedirectURL:  fmt.Sprintf("http://127.0.0.1:%d/callback", port),
    Scopes:       []string{androidPublisherScope, cloudPlatformScope},
}
authURL := conf.AuthCodeURL(state,
    oauth2.AccessTypeOffline,
    oauth2.S256ChallengeOption(verifier))
// open browser, capture ?code= on the loopback listener, then:
tok, err := conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
ts := conf.TokenSource(ctx, tok) // -> option.WithTokenSource(ts) for every API client
```

**ADC / gcloud reuse:** possible (`google.FindDefaultCredentials`), but ADC's
default scopes do **not** include `androidpublisher`, so it can't be the only
path for Play. Recommendation: ship our own Desktop client as the primary path;
optionally accept ADC as a fallback for Cloud-only power users.

> Sources: [OAuth for iOS & Desktop](https://developers.google.com/identity/protocols/oauth2/native-app),
> [Loopback migration](https://developers.google.com/identity/protocols/oauth2/resources/loopback-migration),
> [Sensitive-scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification),
> [User cap](https://support.google.com/cloud/answer/15549945),
> [pkg.go.dev x/oauth2](https://pkg.go.dev/golang.org/x/oauth2)

### 2. Discover Google Cloud projects — ✅ feasible

`google.golang.org/api/cloudresourcemanager/v3`, `Projects.Search` (query-based,
no parent needed) returns `Project{ProjectId, DisplayName, State}`. Filter to
`State == "ACTIVE"`, present a picker. No manual project-ID copy needed.

### 3. Enable required APIs — ✅ feasible, idempotent

`google.golang.org/api/serviceusage/v1`, `Services.BatchEnable("projects/{p}",
&BatchEnableServicesRequest{ServiceIds: [...]})`. **Max 20 service IDs/call** —
our list is 5, non-issue. `batchEnable` is idempotent (already-enabled services
are a no-op), so re-runs are safe. The canonical five (confirmed against
RevenueCat's current Cloud Shell script):

```
cloudresourcemanager.googleapis.com
iam.googleapis.com
androidpublisher.googleapis.com
playdeveloperreporting.googleapis.com
pubsub.googleapis.com
```

`serviceusage.googleapis.com` itself must already be on to call the API — it's
enabled by default on projects, so not in the list.

### 4. Create the RevenueCat service account — ✅ feasible, idempotent

`google.golang.org/api/iam/v1`. Check first with
`Projects.ServiceAccounts.Get("projects/{p}/serviceAccounts/{email}")` (404 →
absent), then `Projects.ServiceAccounts.Create(...)` with `AccountId:
"revenuecat-service-account"`. Email lands as
`revenuecat-service-account@{PROJECT_ID}.iam.gserviceaccount.com`. "Found
existing" branch is trivial.

### 5. Grant Google Cloud IAM roles — ✅ feasible, idempotent

Read-modify-write on the *project* policy via
`cloudresourcemanager/v3`: `Projects.GetIamPolicy` → add member
`serviceAccount:{email}` to the two role bindings → `Projects.SetIamPolicy`.
Roles (confirmed still valid and current):

```
roles/pubsub.editor        # server notifications (docs suggest pubsub.admin if perms errors)
roles/monitoring.viewer    # queue-metrics visibility
```

Idempotent by construction — skip if the binding already contains the member.

### 6. Generate the service-account JSON key — ✅ feasible, stays in memory

`iam/v1` `Projects.ServiceAccounts.Keys.Create(...)` returns a
`ServiceAccountKey` whose **`PrivateKeyData` holds the base64-encoded full JSON
key file** — returned inline, only on `create`, never on `get`/`list`.

```go
key, _ := iamsvc.Projects.ServiceAccounts.Keys.Create(name, &iam.CreateServiceAccountKeyRequest{}).Do()
jsonBytes, _ := base64.StdEncoding.DecodeString(key.PrivateKeyData) // never hits disk
```

Never write it to disk, never log it, hand `jsonBytes` straight to the RC upload
(§12). This mirrors how Apple private keys are held in memory and pushed
(`appleconnect.Key.PrivateKey` → `rc.Apps.Update`).

### 7. Detect org-policy problems — ✅ feasible (attempt-and-recognize)

Two constraints commonly block this: `iam.disableServiceAccountCreation` and
`iam.disableServiceAccountKeyCreation`. A third — `iam.allowedPolicyMemberDomains`
(domain-restricted sharing) — silently rejects granting the Play system account
publisher on the topic (§13).

**Recommended: attempt, then match the error** (querying Org Policy ahead needs
`orgpolicy.policy.get`, which the bootstrapping user usually lacks). Google
returns **HTTP 400 / gRPC `FAILED_PRECONDITION`** with a `PreconditionFailure`
detail naming the constraint, e.g. key creation → *"Key creation is not allowed
on this service account."* with `type:
constraints/iam.disableServiceAccountKeyCreation`. Inspect `*googleapi.Error`
(`.Code == 400`, match the constraint string in `Details`/message) and emit the
useful guidance-to-Console error. Optional pre-check via
`google.golang.org/api/orgpolicy/v2` `Projects.Policies.GetEffectivePolicy` when
the permission is available.

### 8. Add the service account to Google Play — ✅ feasible (API supports it)

`google.golang.org/api/androidpublisher/v3`, `UsersService.Create("developers/{dev}",
&User{Email: saEmail, ...})`. **A service-account email is accepted as the user**
— nothing in the API distinguishes human vs machine identities, and inviting a
service account this way is the documented, standard path. Idempotent via
`Users.List` → skip if the email already appears.

**Requires the human to be Play Console Owner/Admin** (i.e. hold
`CAN_MANAGE_PERMISSIONS_GLOBAL`, or `CAN_MANAGE_PERMISSIONS` on the app). A
lesser role cannot create users/grants. (Inferred from Play's permission model;
the REST pages state only the OAuth scope.)

### 9. Grant required Play permissions — ✅ feasible, package-scoped

**All three RevenueCat permissions exist at the per-app grant level** — financial
data does *not* require account-wide scope. So we can scope to a single package,
which is the right default.

`GrantsService.Create("developers/{dev}/users/{email}",
&Grant{PackageName: pkg, AppLevelPermissions: [...]})`:

| RevenueCat / Play Console label | Per-app enum (`appLevelPermissions[]`) | Account-wide equivalent |
|---|---|---|
| View app information | `CAN_VIEW_NON_FINANCIAL_DATA` | `CAN_VIEW_NON_FINANCIAL_DATA_GLOBAL` |
| View financial data, orders, cancellation surveys | `CAN_VIEW_FINANCIAL_DATA` | `CAN_VIEW_FINANCIAL_DATA_GLOBAL` |
| Manage orders and subscriptions | `CAN_MANAGE_ORDERS` | `CAN_MANAGE_ORDERS_GLOBAL` |

(`CAN_ACCESS_APP` / `CAN_SEE_ALL_APPS` are deprecated — don't use.) Grants are
read back through `Users.List` → `User.Grants[]`; there is no `grants.get`/`list`,
so idempotency = list users, find our SA, check its grant for the package.

### 10. Discover the Play developer account ID — ❌ not possible via API

**Hard Google gap.** There is no Android-Publisher (or any Google) endpoint that
returns the ~19-digit developer account ID from an OAuth identity, and no "list
developers I belong to" endpoint. Every users/grants call *requires*
`developers/{developer}` as input. Official guidance is to read it from the Play
Console URL.

**Fallback:** prompt the user to paste their Play Console URL (or the raw ID) and
parse the digits:

```
? Paste your Play Console URL (or developer account ID):
  https://play.google.com/console/u/0/developers/5412345678901234567/app-list
  -> developer 5412345678901234567
```

Cache it in the profile so re-runs don't re-ask. This is the one place the ideal
"never open the Console" UX cannot be fully met.

### 11. Discover Play apps/packages — ⚠️ partial

Android Publisher has **no `applications.list`** — `edits.*`,
`inappproducts.list`, `monetization.*` all require you to already know the
package name.

**But** the **Play Developer Reporting API** `apps:search`
(`GET playdeveloperreporting.googleapis.com/v1beta1/apps:search`) "returns a list
of apps accessible by the user" with no developer ID or package needed — returns
`apps/{packageName}`. It's `v1beta1` and needs the separate
`playdeveloperreporting` scope.

**Recommended primary path:** read the package name from the local project
(cheap, no extra scope) —
`android/app/build.gradle[.kts]` (`applicationId`), `AndroidManifest.xml`
(`package`), Flutter/RN app config. Fall back to `apps:search` for a picker,
fall back again to manual entry.

### 12. Configure RevenueCat — ❌ blocked by our own API

**This is the gating gap on our side.** RC API v2 today:

- `AppStoreAppConfig` (`internal/api/apps.go:61`) has the full credential set
  (`SubscriptionPrivateKey`, `AppStoreConnectAPIKey`, issuer, vendor number…).
- `PlayStoreAppConfig` (`internal/api/apps.go:83`) has **only** `PackageName`.
  The generated `PlayStoreApp` / `PlayStoreAppCreate` types
  (`internal/api/types_gen.go:36819`, `:36877`) confirm it — no service-account
  credential field, no RTDN topic field.

So today the CLI **can** create a Play app record and set the package name via
`rc.Apps.Create`, but **cannot** upload the service-account JSON. The flow would
still end with "now paste `revenuecat-key.json` into the dashboard" — exactly
what we're trying to kill.

**Answers to the step-12 questions:**
1. Create an Android app via API v2 — ✅ yes (`PlayStoreAppCreate`).
2. Set the package name — ✅ yes.
3. Upload/set Play service credentials — ❌ **no public field exists**.
4. Configure RTDN settings via RC API — ❌ no field.
5. Internal Dashboard API that does credential upload — the dashboard clearly
   does this; needs confirmation whether an internal endpoint is reachable. **Do
   not** have the CLI drive an internal dashboard endpoint — that's the
   `store_state_direct` anti-pattern we already carry reluctantly.
6. Should v2 expose it — **yes. This is the required backend change (§13
   below).**

**Required RevenueCat backend/API change:** add a Play credential write to API
v2, e.g. extend `PlayStoreAppConfig` with a
`play_service_account_credentials_json` (write-only) field, plus a
`play_service_account_credentials_configured` boolean on the read model
(mirroring `AppStoreConnectAPIKeyConfigured`). Then the CLI's `internal/api`
layer gets the field for free on the next `oapi-codegen` regen and the upload is
one `rc.Apps.Update` call, identical to Apple. **Nothing in the CLI half is hard
once this field exists.** File this as the primary dependency.

### 13. Real-Time Developer Notifications — ⚠️ one step is Console-only

Automatable in Go:
- Enable Pub/Sub (§3), create topic, set topic IAM.
- Grant **`roles/pubsub.publisher`** to Play's publisher identity
  **`google-play-developer-notifications@system.gserviceaccount.com`** (confirmed
  address) on the topic.

Use the Cloud client for Pub/Sub — **`cloud.google.com/go/pubsub` (v2)**. The
REST wrapper `google.golang.org/api/pubsub/v1` is **deprecated** (maintenance-
only), so despite the original brief listing it, prefer the Cloud client:

```go
topic, _ := client.CreateTopic(ctx, topicID)
p, _ := topic.IAM().Policy(ctx)
p.Add("serviceAccount:google-play-developer-notifications@system.gserviceaccount.com",
      "roles/pubsub.publisher")
_ = topic.IAM().SetPolicy(ctx, p)
```

**The blocker: pointing the Play app at the topic is Play-Console-only.** No API
(Android Publisher or Play Developer Reporting) sets an app's RTDN "Topic name"
(Play Console → Monetize → Monetization setup → Real-time developer
notifications). Best we can do: create the topic, print the exact
`projects/{id}/topics/{name}` string, and deep-link the user to the Console page.

Also note: RevenueCat's modern "Connect to Google" flow often **creates the
topic + subscription on RevenueCat's side** and just asks you to paste the topic
ID into Play Console. If we keep that model, the CLI may not need to create the
topic at all — it would ask RC for the topic name (needs API support) and then
still hit the same Console-only paste. Coordinate the topic ownership decision
with the backend change in §12.

`iam.allowedPolicyMemberDomains` (domain-restricted sharing) will reject the
`@system.gserviceaccount.com` binding — catch and guide (§7).

### 14. Credential validation — ✅ feasible

After upload, validate and distinguish real failures from Google's propagation
delay (credentials can take minutes to become usable):
- SA credential valid + Android Publisher reachable: call a cheap read (e.g.
  `androidpublisher` `inappproducts.list` or an `edits.insert`/`delete` for the
  package) using the *service account* credential.
- Pub/Sub reachable: `topics.get`.
- RC accepted the credential: read back the `..._configured` flag (§12).

On a transient `403`/permission error shortly after setup, emit the "accepted
but not propagated yet — re-run `rc setup google --verify`" message rather than a
hard failure. Mirror the `--verify` / `--repair` idempotency contract.

### 15. Who needs what permissions

- **Human Google account** must be a Play Console **Owner/Admin** (manage-
  permissions authority) to run users/grants (§8/§9), and must have
  resourcemanager/serviceusage/iam/pubsub authority on the GCP project
  (typically Owner/Editor) for §2-§6/§13.
- **Generated service account** needs, on GCP: `roles/pubsub.editor`,
  `roles/monitoring.viewer`. On Play: the three per-app grants in §9.

---

## Answers to the 15 required questions (index)

1. Best OAuth flow → Desktop client, `127.0.0.1` loopback, auth-code + PKCE (§1).
2. List projects automatically → yes, `cloudresourcemanager/v3 Projects.Search` (§2).
3. Enable all APIs via Go → yes, `serviceusage/v1 BatchEnable` (§3).
4. SA + IAM entirely via Go → yes, `iam/v1` + `cloudresourcemanager/v3` (§4/§5).
5. SA key stays in memory → yes, `ServiceAccountKey.PrivateKeyData` (§6).
6. OAuth human can `users.create` a service account → yes (§8).
7. Exact Play permission enums → `CAN_VIEW_NON_FINANCIAL_DATA`,
   `CAN_VIEW_FINANCIAL_DATA`, `CAN_MANAGE_ORDERS` (§9).
8. Scope to a single package → yes, all three at grant level (§9).
9. Discover developer account ID → **no API; manual paste/parse** (§10).
10. List apps/packages → local project first; `apps:search` fallback (§11).
11. RTDN via API → Pub/Sub yes; app→topic pointing **Console-only** (§13).
12. Upload credential via RC API v2 → **no field today** (§12).
13. Backend change needed → add write-only Play credential field to API v2 (§12).
14. Common blocking org policies → `disableServiceAccountKeyCreation`,
    `disableServiceAccountCreation`, `allowedPolicyMemberDomains` (§7/§13).
15. Owner/Admin vs lesser → users/grants need Play Owner/Admin (§8/§15).

---

## Security

- Human OAuth token/refresh token: memory-only for the command's lifetime; never
  sent to RevenueCat; never logged. (Same posture as `appleconnect` sessions.)
- SA private key: base64-decoded in memory, handed to the RC upload, never
  written to disk, never printed. If a temp file ever becomes unavoidable:
  `0600`, upload, delete immediately — but §6 shows it isn't needed.
- Request the minimum OAuth scopes; prefer package-level Play grants over
  `_GLOBAL`; grant only the two GCP roles.
- Show exactly what will be created/granted in the `Plan` before the single
  consent `Confirm` — no silent state changes (matches the Apple flow's
  per-decision `Answer` receipts).
- `.gitignore`-safe by construction (nothing written).

---

## Recommended CLI UX

Follow the design-system guided-flow: **State → Decisions → Plan → one Confirm →
narrated execution → Card + Hint.** Mark `requires_human: true` (browser + Play
Console admin), agent-only surface tier at first (`docs/command-surface.md`).

```
rc setup google [app-id]

  Google Play configuration — My App
  Signs in to Google locally and configures the credential RevenueCat needs.
  Nothing changes without your OK.

  → Sign in to Google in your browser        (127.0.0.1 loopback)
  ✓ Signed in as developer@example.com

  ? Google Cloud project   > my-app-production
  ? Play developer account (paste Console URL — Google exposes no API for this)
  ? Google Play app        > com.example.myapp   (read from android/app/build.gradle)

  Plan:
    1. Enable 5 Google APIs
    2. Create revenuecat-service-account@my-app-production.iam.gserviceaccount.com
    3. Grant pubsub.editor + monitoring.viewer
    4. Create a service-account key (kept in memory)
    5. Add the service account to Play and grant com.example.myapp access
    6. Create a Pub/Sub topic for notifications
    7. Upload the credential to RevenueCat
  ? Continue > Yes

  ✓ ... (narrated, idempotent) ...
  ! One manual step remains: in Play Console → Monetization setup → Real-time
    developer notifications, set the topic to:
        projects/my-app-production/topics/rc-rtdn
    rc open  # deep-links you there

  Google Play connected 🎉  (run `rc setup google --verify` after ~a few minutes)
```

Flags mirror Apple: `--project`, `--developer-id`, `--package`, `--yes`,
`--json`, `--no-input`, plus `--verify` and `--repair`. Every prompt is also a
flag and env var (dual-mode contract).

---

## Proposed code structure

```
internal/google/
    auth.go            # Desktop OAuth: loopback + PKCE (reuse auth.go pattern), token source
    projects.go        # cloudresourcemanager/v3: Search/List
    services.go        # serviceusage/v1: BatchEnable (idempotent)
    iam.go             # cloudresourcemanager/v3: project IAM read-modify-write
    service_account.go # iam/v1: get-or-create SA, create in-memory key, org-policy error mapping
    play.go            # androidpublisher/v3: Users/Grants (get-or-create, package grant)
    play_apps.go       # playdeveloperreporting apps:search + local-project package detection
    pubsub.go          # cloud.google.com/go/pubsub v2: topic + publisher IAM
internal/api/
    (regenerated)      # PlayStoreAppConfig gains write-only credential field  <-- backend dep
cmd/ / internal/cli/
    setup_google.go    # the guided flow, composing internal/google + internal/api
```

Keep `internal/google` free of CLI concepts (services only), exactly like
`internal/appleconnect`.

---

## Implementation plan (small commits / PRs)

Ordered so each lands independently and the human-visible flow only turns on once
its dependency (the API field) exists.

**Dependency PR (RevenueCat backend, not this repo) — do first:**
- **PR 0:** API v2 — write-only `play_service_account_credentials_json` on
  `PlayStoreAppConfig` + `..._configured` read flag. Blocks §12. Regen CLI types
  after it ships.

**This repo (lean v1 — RTDN excluded):**
1. **PR 1 — deps + OAuth skeleton.** Add `golang.org/x/oauth2`,
   `google.golang.org/api/*`. `internal/google/auth.go` with the loopback+PKCE
   Desktop flow and a `TokenSource`. Unit-test the callback handler like
   `auth_test.go`. Ship behind a hidden `rc setup google` that only authenticates
   and prints the email. (Prototypes the highest-uncertainty piece.)
2. **PR 2 — GCP discovery + enable.** `projects.go`, `services.go`. `rc setup
   google` lists projects and enables the 5 APIs idempotently.
3. **PR 3 — service account + IAM + key.** `service_account.go`, `iam.go`.
   Get-or-create SA, grant roles, create in-memory key. Org-policy error mapping.
4. **PR 4 — Play users/grants.** `play.go`. Manual developer-ID paste/parse
   (§10). Add SA as user, package-scoped grant. Idempotent via `Users.List`.
5. **PR 5 — package discovery.** `play_apps.go`. Local-project detection first,
   `apps:search` fallback.
6. **PR 6 — RC upload (the payoff).** Wire the (now-existing) credential field;
   end-to-end `rc setup google` creates/updates the app and uploads the
   credential. Removes the "paste JSON" ending. **This completes lean v1.**
7. **PR 7 — verify/repair.** `--verify`, `--repair`, propagation-delay handling
   (§14). Promote surface tier once DX-tested.

**Deferred (post-v1, not in the stack):** Pub/Sub + RTDN (`pubsub.go`: topic +
publisher IAM, print topic + deep-link for the Console-only step). Tracked as
DX-987 — pick up after deciding topic ownership with the backend.

**Verification (Google) — parallel track:** stand up the Desktop OAuth client +
consent screen, submit sensitive-scope verification with the demo video early;
the 100-user cap makes it a GA blocker (§1).

---

## Open questions to resolve with the backend team

1. Who owns the RTDN topic — CLI-created, or RC-created (and the CLI just reads
   the name back)? Drives whether §13 creates the topic or fetches it.
2. Shape of the API v2 credential field (write-only JSON blob vs structured).
3. Whether to expose an RC-side "expected topic name" so the CLI can print the
   exact string for the Console paste.
4. Scope decision: broad `cloud-platform` vs narrow enumerated scopes, weighed
   against verification review friction.
