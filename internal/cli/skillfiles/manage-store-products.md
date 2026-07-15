# Safely manage App Store and Play Store products with persisted plans

Use RevenueCat product store-state plans to preview and apply store product
configuration. A repository and local file are optional. The durable boundary
between agent invocations is the server-side `plan_id`, not process memory.

## Discover the contract

```bash
rc schema products store plan --json
rc schema products store show --json
rc schema products store apply --json
rc schema products store discard --json
```

## Create a plan

Provide canonical CSV or desired-state JSON from a file or stdin. Under
`--no-input`, input is required and the command never prompts.

```bash
rc products store plan app_abc \
  --file catalog.csv \
  --json --no-input
```

Without a filesystem:

```bash
cat desired-states.json | rc products store plan app_abc \
  --file - --input-format json \
  --json --no-input
```

JSON may be either a `desired_states` envelope or the array itself. Capture
`.data.id` from the response. Planning persists the complete desired state and
computed diff in RevenueCat but does not change products or either store.

## Review the persisted plan

```bash
rc products store show plan_123 --json --no-input
```

Inspect:

- `.data.status`: continue only from `planned`.
- `.data.plan_items[].diff`: exact fields that will change.
- `.data.warnings`: plan-level warnings.
- `.data.plan_items[].warnings`: product warnings.
- `.data.actions`: `apply` must be present.

Never apply a plan containing a warning whose `severity` is `blocker`. Never
rerun `plan` between review and apply: that would create a different plan ID.

## Apply the exact reviewed plan

```bash
rc products store apply plan_123 --yes --json --no-input
```

`--yes` is mandatory for non-interactive mutation. The command fetches and
re-displays the persisted plan before asking Khepri to apply that same ID, then
waits for `applied` or reports per-product apply errors.

## Discard instead

```bash
rc products store discard plan_123 --yes --json --no-input
```

Discarding never applies store changes. Plans may also expire server-side.

## Human shortcut

`rc products store sync app_abc` performs input, planning, review, confirmation,
and apply in one process. Prefer the explicit lifecycle above for agents because
the reviewed `plan_id` is auditable across separate invocations.
