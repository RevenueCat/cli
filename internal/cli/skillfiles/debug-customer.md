Diagnose why a customer does not have an expected entitlement or subscription.

## Steps

### 1. Pull the full customer record

```bash
rc customer show <customer-id> --json
```

Check:
- `entitlements` — active entitlements and their expiry dates
- `subscriptions` — store subscriptions and their status
- `non_subscriptions` — one-time purchases

If `entitlements` is empty or missing the expected key, continue below.

### 2. Check entitlement configuration

```bash
rc entitlements list --json
```

Verify the entitlement exists and its `lookup_key` matches what your app checks.
A misconfigured or missing entitlement means no customer will ever receive it.

### 3. Check which products grant the entitlement

```bash
rc entitlements show <entitlement-id> --json
```

Look at the attached products list. If the product the customer purchased
is not attached to the entitlement, they won't receive it even after a
valid purchase.

### 4. Check subscription status

```bash
rc customer show <customer-id> --json | jq '.data.subscriptions'
```

Status values:
- `active` — currently subscribed
- `expired` — subscription ended, no renewal
- `in_grace_period` — payment failed, still has access temporarily
- `in_billing_retry` — payment failed, retrying
- `cancelled` — cancelled but may still be active until period end
- `revoked` — manually revoked

If the status is `expired` or `revoked` with an unexpected date, check
the store dashboard for the transaction.

### 5. Grant a promotional entitlement if needed

If the customer should have access and the purchase is confirmed valid:

```bash
rc customer grant <customer-id> <entitlement-id> --duration <days>d
```

Duration examples: `1d`, `7d`, `30d`, `lifetime`.

### 6. Verify the fix

```bash
rc customer show <customer-id> --json | jq '.data.entitlements'
```
