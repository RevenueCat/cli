Set up a new RevenueCat offering with packages and products attached.

An offering groups packages (e.g. monthly, annual, lifetime) that are
presented together on a paywall. Each package must have at least one
product (a store SKU) attached before it appears in the SDK.

## Steps

### 1. Check what already exists

```bash
rc offerings list --json
rc products list --json
```

Pick an existing offering to add packages to, or proceed to create one.

### 2. Create the offering (skip if adding to an existing one)

```bash
rc offerings create --lookup-key <lookup-key> --display-name "<Display Name>"
```

`lookup-key` is the identifier your app code uses (e.g. `default`, `sale`, `annual-promo`).
Save the returned `id` — you'll need it for packages.

### 3. Create packages inside the offering

Repeat for each package (monthly, annual, lifetime, etc.):

```bash
rc packages create <offering-id> --lookup-key <package-key> --display-name "<Package Name>"
```

Standard RevenueCat lookup keys: `$rc_monthly`, `$rc_annual`, `$rc_lifetime`, `$rc_weekly`, `$rc_six_month`, `$rc_three_month`, or any custom string.
Save each returned package `id`.

### 4. Find or create the products to attach

List existing products to see if the store SKUs already exist:

```bash
rc products list --json
rc apps list --json   # needed if creating new products
```

Create a product if needed:

```bash
rc products create --store-id <store_sku> --type subscription --app-id <app-id> --display-name "<Name>" --duration P1M
```

`--duration` is ISO 8601: P1W (weekly), P1M (monthly), P3M (3-month), P6M (6-month), P1Y (annual).

### 5. Attach products to packages

```bash
rc packages attach <offering-id> <package-id> <product-id>
```

Repeat for each package. A package can have multiple products (one per platform/store).

### 6. Verify

```bash
rc offerings show <offering-id> --json
rc packages list --json | jq '.data.items[] | select(.offering_id == "<offering-id>")'
```
