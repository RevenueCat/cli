Audit entitlement configuration — verify every entitlement has products
attached and that all packages have products for each target platform.

Run this before a launch or after adding new store SKUs to catch
configuration gaps before customers hit them.

## Steps

### 1. List all entitlements

```bash
rc entitlements list --json
```

Flag any with empty `products` arrays — they exist but can never be granted
by a purchase.

### 2. For each entitlement, check attached products

```bash
rc entitlements show <entitlement-id> --json
```

Verify the attached products cover all platforms you ship on (App Store,
Google Play, Stripe, etc.). A missing platform means purchases on that
store won't unlock the entitlement.

### 3. List all offerings and packages

```bash
rc offerings list --json
rc packages list --json
```

### 4. For each package, check attached products

```bash
rc packages products <offering-id> <package-id> --json
```

A package with no products won't appear in the SDK response. Check that:
- Each package has at least one product per platform
- Product `state` is not `archived`

### 5. Cross-reference

Products attached to packages but not to entitlements means a purchase
will be recorded but won't unlock access:

```bash
# Products on packages
rc packages products <offering-id> <package-id> --json | jq '[.data.items[].id]'

# Products on entitlements
rc entitlements show <entitlement-id> --json | jq '[.data.products[].id]'
```

Any product ID in packages but not in entitlements is a gap.

### 6. Fix gaps

Attach missing products to the entitlement:

```bash
rc entitlements attach <entitlement-id> <product-id>
```
