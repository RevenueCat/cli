# Cookbook

Common workflows, one-liners, and pipelines. All examples assume `rc login`
has been run or `RC_API_KEY` is set in the environment.

## First-time setup

```bash
rc login                            # interactive
rc projects use                     # picker if multiple projects
rc whoami                           # confirm active profile + project
```

Or fully non-interactive (CI, scripts):

```bash
RC_API_KEY=sk_... RC_PROJECT_ID=proj_abc rc whoami --json
```

## Customer support workflows

Look up a customer end-to-end (customer record + active entitlements + subs +
purchases, merged into one envelope):

```bash
rc customers show cus_abc
rc customers show cus_abc --json | jq '.data.subscriptions.items'
```

Grant a one-month promotional entitlement:

```bash
rc customers grant --customer-id cus_abc --entitlement-id pro --duration monthly --yes
```

Refund a Web Billing subscription (this is `--yes` by intent — confirmation
matters):

```bash
rc subscriptions refund sub_abc
```

Extend a subscription's billing period one month (support gesture):

```bash
rc subscriptions extend sub_abc --by P1M
```

Merge two customer records into one:

```bash
rc customers transfer cus_old --to cus_new --yes
```

## Catalog management

List all entitlements and pick out the lookup keys:

```bash
rc entitlements list --json | jq -r '.data.items[].lookup_key'
```

Create an entitlement and immediately attach products:

```bash
rc entitlements create --lookup-key plus --display-name "Plus" --yes --json
rc entitlements attach plus prod_monthly prod_yearly
```

Find which offering is currently active:

```bash
rc offerings list --json | jq '.data.items[] | select(.is_current) | .id'
```

Archive a product (kept on file, hidden from new offerings):

```bash
rc products archive prod_legacy
rc products restore prod_legacy        # bring it back later
```

## Metrics + charts

Today's KPIs in one shot:

```bash
rc metrics
rc metrics --json | jq '.data.metrics[] | {(.id): .value}'
```

Discover available charts, fetch one, then inspect its filter options:

```bash
rc charts list
rc charts options mrr
rc charts show mrr --filter store=app_store
```

Pull a chart as CSV-friendly rows:

```bash
rc charts show mrr --json |
  jq -r '.data.values[] | [.cohort, .value] | @csv'
```

## Audit / compliance

Recent activity:

```bash
rc audit --limit 25
```

Find deletes in the last 100 events:

```bash
rc audit --limit 100 --json |
  jq '.data.items[] | select(.action_type | contains("delete"))'
```

## Scripting with multiple profiles

Separate staging and prod credentials cleanly:

```bash
rc login --profile staging --api-key sk_staging_...
rc login --profile prod    --api-key sk_prod_...

rc --profile staging customer list --limit 5
rc --profile prod metrics --json
```

Or per-invocation env (no on-disk profile needed for CI):

```bash
RC_PROFILE=staging rc customers list --json
RC_API_KEY=sk_... RC_PROJECT_ID=proj_x rc audit --limit 10 --json
```

## Driving the CLI from an AI agent

Discover the full surface:

```bash
rc commands --json
```

Get a single command's input/output schema:

```bash
rc schema customers grant
rc schema entitlements attach        # shows positional arg shape
```

Errors come back as JSON in `--json` mode with stable types and exit codes,
so the same parser handles both transport and CLI errors:

```bash
$ rc entitlements show entl_nope --json
{
  "error": {
    "type": "resource_missing",
    "message": "Could not find entitlement 'entl_nope' within this project",
    "exit_code": 5,
    "request_id": "fc53f50e-…",
    "doc_url": "https://errors.rev.cat/resource-missing"
  },
  "schema_version": 1
}
```

Exit code reference: `0` success, `1` generic, `2` usage, `4` unauthorized,
`5` not found, `6` rate limited.

## Useful patterns

**Pipe IDs into a loop**:

```bash
rc customers list --json --limit 100 |
  jq -r '.data.items[].id' |
  while read id; do
    rc customers show "$id" --json > "customers/$id.json"
  done
```

**Detect TTY for adaptive output** (already automatic — the CLI emits color +
prompts only when stdin/stdout are terminals; pass `--no-color` to force off
or `--no-input` to disable prompts):

```bash
rc customers list                    # pretty table on TTY
rc customers list | cat              # still pretty (we don't auto-switch on pipe)
rc customers list --json | cat       # explicit machine-readable
```
