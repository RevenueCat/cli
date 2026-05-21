#!/usr/bin/env python3
"""
spec-diff.py — compare two OpenAPI YAML snapshots and print a Markdown summary.

Usage:
    python3 scripts/spec-diff.py OLD.yaml NEW.yaml [--title "Spec Name"]

Exit codes:
    0  no differences found
    1  differences found (use --no-exit-on-diff to always exit 0)
"""

import argparse
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("ERROR: pyyaml is required — pip install pyyaml", file=sys.stderr)
    sys.exit(2)


def load_yaml(path: str) -> dict:
    with open(path, encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


def endpoints(spec: dict) -> dict[str, set[str]]:
    """Return {path: {method, ...}} for all paths in the spec."""
    result: dict[str, set[str]] = {}
    http_methods = {"get", "put", "post", "delete", "options", "head", "patch", "trace"}
    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        methods = {m for m in item if m.lower() in http_methods}
        if methods:
            result[path] = methods
    return result


def schema_names(spec: dict) -> set[str]:
    return set((spec.get("components") or {}).get("schemas") or {})


def operation_summary(spec: dict, path: str, method: str) -> str:
    item = (spec.get("paths") or {}).get(path) or {}
    op = item.get(method.lower()) or {}
    return op.get("summary") or op.get("operationId") or ""


def diff_specs(old: dict, new: dict) -> dict:
    old_eps = endpoints(old)
    new_eps = endpoints(new)

    old_paths = set(old_eps)
    new_paths = set(new_eps)

    added_paths = new_paths - old_paths
    removed_paths = old_paths - new_paths
    common_paths = old_paths & new_paths

    added_endpoints: list[tuple[str, str, str]] = []  # (path, method, summary)
    removed_endpoints: list[tuple[str, str, str]] = []
    changed_endpoints: list[tuple[str, str, str, str]] = []  # (path, method, old_summary, new_summary)

    for path in sorted(added_paths):
        for method in sorted(new_eps[path]):
            summary = operation_summary(new, path, method)
            added_endpoints.append((path, method.upper(), summary))

    for path in sorted(removed_paths):
        for method in sorted(old_eps[path]):
            summary = operation_summary(old, path, method)
            removed_endpoints.append((path, method.upper(), summary))

    for path in sorted(common_paths):
        added_methods = new_eps[path] - old_eps[path]
        removed_methods = old_eps[path] - new_eps[path]
        for method in sorted(added_methods):
            summary = operation_summary(new, path, method)
            added_endpoints.append((path, method.upper(), summary))
        for method in sorted(removed_methods):
            summary = operation_summary(old, path, method)
            removed_endpoints.append((path, method.upper(), summary))

        # Detect changed operations (summary/description/parameters/requestBody/responses)
        for method in sorted(old_eps[path] & new_eps[path]):
            old_op = ((old.get("paths") or {}).get(path) or {}).get(method) or {}
            new_op = ((new.get("paths") or {}).get(path) or {}).get(method) or {}
            if _op_changed(old_op, new_op):
                old_sum = old_op.get("summary") or old_op.get("operationId") or ""
                new_sum = new_op.get("summary") or new_op.get("operationId") or ""
                changed_endpoints.append((path, method.upper(), old_sum, new_sum))

    old_schemas = schema_names(old)
    new_schemas = schema_names(new)
    added_schemas = sorted(new_schemas - old_schemas)
    removed_schemas = sorted(old_schemas - new_schemas)

    return {
        "added_endpoints": added_endpoints,
        "removed_endpoints": removed_endpoints,
        "changed_endpoints": changed_endpoints,
        "added_schemas": added_schemas,
        "removed_schemas": removed_schemas,
    }


def _op_changed(old_op: dict, new_op: dict) -> bool:
    """Shallow change detection on the parts users care about most."""
    for key in ("summary", "description", "deprecated", "parameters", "requestBody", "responses"):
        if old_op.get(key) != new_op.get(key):
            return True
    return False


def render_markdown(diff: dict, title: str) -> str:
    lines: list[str] = []
    has_changes = any(diff[k] for k in diff)

    lines.append(f"## {title}")
    lines.append("")

    if not has_changes:
        lines.append("_No changes detected._")
        return "\n".join(lines)

    def endpoint_row(path: str, method: str, summary: str) -> str:
        s = f" — {summary}" if summary else ""
        return f"- `{method} {path}`{s}"

    if diff["added_endpoints"]:
        lines.append(f"### Added endpoints ({len(diff['added_endpoints'])})")
        lines.append("")
        for path, method, summary in diff["added_endpoints"]:
            lines.append(endpoint_row(path, method, summary))
        lines.append("")

    if diff["removed_endpoints"]:
        lines.append(f"### Removed endpoints ({len(diff['removed_endpoints'])})")
        lines.append("")
        for path, method, summary in diff["removed_endpoints"]:
            lines.append(endpoint_row(path, method, summary))
        lines.append("")

    if diff["changed_endpoints"]:
        lines.append(f"### Changed endpoints ({len(diff['changed_endpoints'])})")
        lines.append("")
        for path, method, old_sum, new_sum in diff["changed_endpoints"]:
            if old_sum != new_sum and (old_sum or new_sum):
                lines.append(f"- `{method} {path}` — _{old_sum}_ → _{new_sum}_")
            else:
                lines.append(f"- `{method} {path}`")
        lines.append("")

    if diff["added_schemas"]:
        lines.append(f"### New schemas ({len(diff['added_schemas'])})")
        lines.append("")
        for s in diff["added_schemas"]:
            lines.append(f"- `{s}`")
        lines.append("")

    if diff["removed_schemas"]:
        lines.append(f"### Removed schemas ({len(diff['removed_schemas'])})")
        lines.append("")
        for s in diff["removed_schemas"]:
            lines.append(f"- `{s}`")
        lines.append("")

    return "\n".join(lines).rstrip()


def main() -> int:
    parser = argparse.ArgumentParser(description="Diff two OpenAPI YAML snapshots")
    parser.add_argument("old", help="Path to the old snapshot")
    parser.add_argument("new", help="Path to the new snapshot")
    parser.add_argument("--title", default="API changes", help="Section heading in the Markdown output")
    parser.add_argument("--no-exit-on-diff", action="store_true",
                        help="Always exit 0 even when differences are found")
    args = parser.parse_args()

    if not Path(args.old).exists():
        print(f"ERROR: {args.old} not found", file=sys.stderr)
        return 2
    if not Path(args.new).exists():
        print(f"ERROR: {args.new} not found", file=sys.stderr)
        return 2

    old = load_yaml(args.old)
    new = load_yaml(args.new)

    diff = diff_specs(old, new)
    md = render_markdown(diff, args.title)
    print(md)

    has_changes = any(diff[k] for k in diff)
    if has_changes and not args.no_exit_on_diff:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
