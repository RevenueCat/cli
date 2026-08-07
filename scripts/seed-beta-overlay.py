#!/usr/bin/env python3
"""Seed docs/specs/v2-beta-overlay.yaml from the backend's full dev OpenAPI spec.

The public spec we fetch from docs is release-filtered and omits endpoints the
CLI uses but that the backend marks `x-release-status: development` (the
store_state family). This extracts just those path keys — with request/response
bodies stripped — into a small overlay that preprocess-spec.py merges on top of
the public spec at codegen/diff time. The overlay declares that the paths exist
so gen-paths builds path helpers and spec-diff tracks coverage; the CLI
hand-writes the types, so oapi-codegen generates nothing from it.

This is a *seeding* tool, not part of CI: run it once against a local checkout
of the dev spec, then hand-maintain the result. Re-run to reseed when the
upstream store_state schema changes.

    python3 scripts/seed-beta-overlay.py \
        path/to/openapi-dev.yaml \
        docs/specs/v2-beta-overlay.yaml

Which paths to pull is controlled by PATH_SUBSTRINGS below — keep it to the
endpoints the CLI actually depends on.
"""

import re
import sys
import yaml

# Only pull paths whose key contains one of these — the development-status
# endpoints the CLI depends on: per-product store_state, the store_state plans
# tree, and product price management. Keep this to paths the CLI actually calls.
PATH_SUBSTRINGS = [
    "/products/{product_id}/store_state",
    "/store_state/plans",
    "/products/{product_id}/prices",
    "/products/{product_id}/test_store_prices",
]

REF_RE = re.compile(r"#/components/([A-Za-z0-9]+)/([A-Za-z0-9_.-]+)")


def find_refs(obj):
    """Yield (component_type, name) for every #/components/<type>/<name> ref."""
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k == "$ref" and isinstance(v, str):
                m = REF_RE.search(v)
                if m:
                    yield m.group(1), m.group(2)
            else:
                yield from find_refs(v)
    elif isinstance(obj, list):
        for item in obj:
            yield from find_refs(item)


def main():
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} DEV_SPEC OVERLAY_OUT", file=sys.stderr)
        sys.exit(1)
    dev_path, out_path = sys.argv[1], sys.argv[2]

    with open(dev_path) as f:
        dev = yaml.safe_load(f)

    paths = {
        p: item
        for p, item in dev.get("paths", {}).items()
        if any(sub in p for sub in PATH_SUBSTRINGS)
    }
    if not paths:
        print("no matching paths found — check PATH_SUBSTRINGS", file=sys.stderr)
        sys.exit(1)

    # The CLI hand-writes the types for these development-status endpoints (see
    # internal/api/store_state_*.go and products.go). The overlay only needs to
    # declare that the paths exist — so gen-paths builds path helpers and
    # spec-diff tracks coverage — not to generate models. Strip request/response
    # bodies so oapi-codegen emits nothing for them.
    methods = {"get", "put", "post", "delete", "patch", "options", "head", "trace"}
    for item in paths.values():
        for method, op in item.items():
            if method not in methods or not isinstance(op, dict):
                continue
            op.pop("requestBody", None)
            op["responses"] = {"200": {"description": "OK"}}

    # Walk component refs transitively, starting from the selected paths.
    dev_components = dev.get("components", {})
    wanted = {}  # component_type -> set(names)
    queue = list(find_refs(paths))
    while queue:
        ctype, name = queue.pop()
        wanted.setdefault(ctype, set())
        if name in wanted[ctype]:
            continue
        wanted[ctype].add(name)
        node = dev_components.get(ctype, {}).get(name)
        if node is not None:
            queue.extend(find_refs(node))

    components = {}
    for ctype, names in sorted(wanted.items()):
        components[ctype] = {
            n: dev_components[ctype][n]
            for n in sorted(names)
            if n in dev_components.get(ctype, {})
        }

    overlay = {
        "# NOTE": (
            "Hand-maintained beta overlay — see DX-880. Seeded from the "
            "backend's openapi-dev.yaml (x-release-status: development endpoints "
            "the CLI uses). Merged onto the public spec by "
            "scripts/preprocess-spec.py. Remove entries here once they graduate "
            "into the public spec."
        ),
        "openapi": dev.get("openapi", "3.0.0"),
        "info": {"title": "RevenueCat v2 — beta overlay", "version": "overlay"},
        "paths": paths,
        "components": components,
    }

    with open(out_path, "w") as f:
        yaml.dump(overlay, f, allow_unicode=True, sort_keys=False, default_flow_style=False)

    schema_count = len(components.get("schemas", {}))
    print(f"Wrote {out_path}: {len(paths)} paths, {schema_count} schemas")


if __name__ == "__main__":
    main()
