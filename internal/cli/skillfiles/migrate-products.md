Move products from one app to another — for example when migrating from a
legacy app to a new one, or copying store SKUs across platforms.

Products in RevenueCat are app-scoped, so you create new product records in
the target app and then re-attach them to the same packages.

## Steps

### 1. Identify source app and products

```bash
rc apps list --json
rc products list --json --app-id <source-app-id>
```

Note the `store_identifier`, `type`, and `subscription.duration` for each
product you want to migrate.

### 2. Identify the target app

```bash
rc apps list --json
```

Save the target `app-id`.

### 3. Create each product in the target app

For each product from step 1:

```bash
rc products create \
  --store-id <store_identifier> \
  --type <type> \
  --app-id <target-app-id> \
  --display-name "<display name>" \
  --duration <P1M|P1Y|...>
```

Save the new product IDs.

### 4. Find the packages that reference the old products

```bash
rc packages list --json
```

For each package, check attached products:

```bash
rc packages products <offering-id> <package-id> --json
```

### 5. Attach new products to packages

```bash
rc packages attach <offering-id> <package-id> <new-product-id>
```

### 6. Optionally detach the old products

Only do this after confirming the new products are correctly configured:

```bash
rc packages detach <offering-id> <package-id> <old-product-id>
```

### 7. Archive old products

Once detached and no longer needed:

```bash
rc products archive <old-product-id>
```

Use `archive` (not `delete`) — archived products are recoverable.

### 8. Verify

```bash
rc products list --json --app-id <target-app-id>
rc packages list --json
```
