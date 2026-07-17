#!/usr/bin/env python3
"""
spec-diff.py — deep-diff two OpenAPI YAML snapshots and report what changed,
categorised by impact on the CLI implementation.

Usage:
    python3 scripts/spec-diff.py OLD.yaml NEW.yaml \\
        [--title "Spec Name"] \\
        [--coverage docs/specs/cli-coverage.yaml] \\
        [--no-exit-on-diff]

Exit codes:
    0  no differences found
    1  differences found
    2  usage / file-not-found error

Output is Markdown suitable for a PR body or terminal review.
Severity levels:
    CRITICAL  CLI-implemented endpoint has a breaking change (new required
              param/field, removed endpoint, type change)
    WARNING   CLI-implemented endpoint changed non-breakingly, or is deprecated
    NEW       Endpoint added to spec that CLI does not yet implement
    INFO      Change to a spec endpoint the CLI doesn't use
"""

import argparse
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("ERROR: pyyaml is required — pip install pyyaml", file=sys.stderr)
    sys.exit(2)

# ---------------------------------------------------------------------------
# Spec loading
# ---------------------------------------------------------------------------

def load_yaml(path: str) -> dict:
    with open(path, encoding="utf-8") as f:
        return yaml.safe_load(f) or {}


# ---------------------------------------------------------------------------
# Coverage: which spec paths does the CLI implement?
# ---------------------------------------------------------------------------

def load_coverage(path: str | None) -> set[str]:
    """
    Return a set of normalised 'METHOD /path/pattern' strings that the CLI
    implements. Loaded from a YAML file with the structure:

        endpoints:
          - method: GET
            path: /projects/{project_id}/apps

    Path parameter names are normalised to '{param}' so they match regardless
    of what the variable is called in both the spec and coverage file.
    """
    if not path:
        return set()
    data = load_yaml(path)
    covered = set()
    for ep in data.get("endpoints") or []:
        method = (ep.get("method") or "").upper().strip()
        p = _norm_path(ep.get("path") or "")
        if method and p:
            covered.add(f"{method} {p}")
    return covered


def _norm_path(path: str) -> str:
    """Normalise path-param names to '{param}' for fuzzy matching."""
    import re
    return re.sub(r"\{[^}]+\}", "{param}", path)


# ---------------------------------------------------------------------------
# Endpoint helpers
# ---------------------------------------------------------------------------

HTTP_METHODS = {"get", "put", "post", "delete", "options", "head", "patch", "trace"}


def all_endpoints(spec: dict) -> dict[tuple[str, str], dict]:
    """Return {(METHOD, path): operation_dict} for all paths in the spec."""
    result = {}
    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        for method, op in item.items():
            if method.lower() in HTTP_METHODS and isinstance(op, dict):
                result[(method.upper(), path)] = op
    return result


def is_covered(method: str, path: str, covered: set[str]) -> bool:
    return f"{method.upper()} {_norm_path(path)}" in covered


# ---------------------------------------------------------------------------
# Deep diff helpers
# ---------------------------------------------------------------------------

def diff_params(old_params: list, new_params: list) -> list[str]:
    """
    Return a list of human-readable change strings for parameter lists.
    Parameters are keyed by (name, in).
    """
    def key(p):
        return (p.get("name", ""), p.get("in", ""))

    old_map = {key(p): p for p in (old_params or [])}
    new_map = {key(p): p for p in (new_params or [])}
    changes = []

    for k in sorted(set(new_map) - set(old_map)):
        p = new_map[k]
        req = p.get("required", False)
        label = "**required**" if req else "optional"
        changes.append(f"+ New {label} `{k[1]}` parameter: `{k[0]}`")

    for k in sorted(set(old_map) - set(new_map)):
        changes.append(f"- Removed `{k[1]}` parameter: `{k[0]}`")

    for k in sorted(set(old_map) & set(new_map)):
        op, np = old_map[k], new_map[k]
        sub = []
        old_req = op.get("required", False)
        new_req = np.get("required", False)
        if old_req != new_req:
            sub.append(f"required: {old_req} → **{new_req}**")
        old_type = _schema_type(op.get("schema") or {})
        new_type = _schema_type(np.get("schema") or {})
        if old_type and new_type and old_type != new_type:
            sub.append(f"type: `{old_type}` → `{new_type}`")
        old_desc = op.get("description", "")
        new_desc = np.get("description", "")
        if old_desc != new_desc and (old_desc or new_desc):
            sub.append("description changed")
        if sub:
            changes.append(f"~ `{k[1]}` param `{k[0]}`: {'; '.join(sub)}")

    return changes


def diff_request_body(old_op: dict, new_op: dict) -> list[str]:
    """Return change strings for requestBody schema properties (1-level deep)."""
    old_schema = _extract_json_schema(old_op.get("requestBody") or {})
    new_schema = _extract_json_schema(new_op.get("requestBody") or {})
    if old_schema is None and new_schema is None:
        return []
    if old_schema is None:
        return ["+ Request body added"]
    if new_schema is None:
        return ["- Request body removed"]
    return _diff_object_schema(old_schema, new_schema, "request body")


def diff_responses(old_op: dict, new_op: dict) -> list[str]:
    """Return change strings for response status codes."""
    old_resp = old_op.get("responses") or {}
    new_resp = new_op.get("responses") or {}
    changes = []
    for code in sorted(set(new_resp) - set(old_resp)):
        changes.append(f"+ New response code `{code}`")
    for code in sorted(set(old_resp) - set(new_resp)):
        changes.append(f"- Removed response code `{code}`")
    return changes


def _extract_json_schema(body: dict) -> dict | None:
    """Drill into requestBody → content → application/json → schema."""
    if not body:
        return None
    content = body.get("content") or {}
    json_content = content.get("application/json") or {}
    return json_content.get("schema") or None


def _diff_object_schema(old_s: dict, new_s: dict, label: str) -> list[str]:
    """Shallow diff of object schema properties."""
    old_props = old_s.get("properties") or {}
    new_props = new_s.get("properties") or {}
    old_req = set(old_s.get("required") or [])
    new_req = set(new_s.get("required") or [])
    changes = []

    for name in sorted(set(new_props) - set(old_props)):
        is_req = name in new_req
        label_str = "**required**" if is_req else "optional"
        changes.append(f"+ New {label_str} {label} field: `{name}`")

    for name in sorted(set(old_props) - set(new_props)):
        changes.append(f"- Removed {label} field: `{name}`")

    # Existing properties — check for type changes or required status change
    for name in sorted(set(old_props) & set(new_props)):
        sub = []
        was_req = name in old_req
        now_req = name in new_req
        if was_req != now_req:
            sub.append(f"required: {was_req} → **{now_req}**")
        old_type = _schema_type(old_props[name])
        new_type = _schema_type(new_props[name])
        if old_type and new_type and old_type != new_type:
            sub.append(f"type: `{old_type}` → `{new_type}`")
        if sub:
            changes.append(f"~ {label} field `{name}`: {'; '.join(sub)}")

    return changes


def _schema_type(schema: dict) -> str:
    if not schema:
        return ""
    if "$ref" in schema:
        return schema["$ref"].split("/")[-1]
    t = schema.get("type", "")
    fmt = schema.get("format", "")
    return f"{t}({fmt})" if fmt else t


def _schema_names(spec: dict) -> set[str]:
    return set(((spec.get("components") or {}).get("schemas") or {}).keys())


# ---------------------------------------------------------------------------
# Severity classification
# ---------------------------------------------------------------------------

CRITICAL = "CRITICAL"
WARNING  = "WARNING"
NEW      = "NEW"
INFO     = "INFO"


def classify_operation_changes(param_changes, body_changes, resp_changes, depr_changed: bool) -> str:
    """Return the highest severity for the set of changes to one operation."""
    if depr_changed:
        return WARNING
    all_changes = param_changes + body_changes + resp_changes
    for c in all_changes:
        is_type_change = "type: `" in c and "` → `" in c
        if c.startswith("+ New **required**") or c.startswith("- Removed") or "→ **true**" in c.lower() or is_type_change:
            return CRITICAL
    if all_changes:
        return WARNING
    return INFO


# ---------------------------------------------------------------------------
# Main diff
# ---------------------------------------------------------------------------

def diff_specs(old: dict, new: dict, covered: set[str]) -> dict:
    old_eps = all_endpoints(old)
    new_eps = all_endpoints(new)

    old_keys = set(old_eps)
    new_keys = set(new_eps)

    results = {
        CRITICAL: [],   # (method, path, summary, [change_str])
        WARNING:  [],
        NEW:      [],   # (method, path, summary) — in spec but not in CLI
        INFO:     [],   # (method, path, summary, [change_str])
        "removed": [],  # (method, path, summary, covered)
        "added_schemas": [],
        "removed_schemas": [],
    }

    # Removed endpoints
    for (method, path) in sorted(old_keys - new_keys):
        op = old_eps[(method, path)]
        summary = op.get("summary") or op.get("operationId") or ""
        cov = is_covered(method, path, covered)
        results["removed"].append((method, path, summary, cov))

    # New endpoints
    for (method, path) in sorted(new_keys - old_keys):
        op = new_eps[(method, path)]
        summary = op.get("summary") or op.get("operationId") or ""
        cov = is_covered(method, path, covered)
        if cov:
            # Somehow we call it but it wasn't in the old spec — treat as info
            results[INFO].append((method, path, summary, ["Endpoint newly documented in spec"]))
        else:
            results[NEW].append((method, path, summary))

    # Changed endpoints
    for (method, path) in sorted(old_keys & new_keys):
        old_op = old_eps[(method, path)]
        new_op = new_eps[(method, path)]

        param_changes = diff_params(
            old_op.get("parameters"), new_op.get("parameters")
        )
        body_changes = diff_request_body(old_op, new_op)
        resp_changes = diff_responses(old_op, new_op)
        depr_changed = bool(old_op.get("deprecated") != new_op.get("deprecated")
                            and new_op.get("deprecated"))

        all_changes = param_changes + body_changes + resp_changes
        if not all_changes and not depr_changed:
            continue  # identical in all the ways we check

        if depr_changed:
            all_changes = ["⚠️ **DEPRECATED** — this endpoint is now marked deprecated"] + all_changes

        summary = new_op.get("summary") or new_op.get("operationId") or ""
        cov = is_covered(method, path, covered)

        if cov:
            sev = classify_operation_changes(param_changes, body_changes, resp_changes, depr_changed)
            results[sev].append((method, path, summary, all_changes))
        else:
            results[INFO].append((method, path, summary, all_changes))

    # Schema names
    results["added_schemas"] = sorted(_schema_names(new) - _schema_names(old))
    results["removed_schemas"] = sorted(_schema_names(old) - _schema_names(new))

    return results


# ---------------------------------------------------------------------------
# Markdown rendering
# ---------------------------------------------------------------------------

def render_markdown(diff: dict, title: str, has_coverage: bool) -> str:
    lines: list[str] = []

    n_critical = len(diff[CRITICAL])
    n_removed_cov = sum(1 for _, _, _, cov in diff["removed"] if cov)
    n_warning  = len(diff[WARNING])
    n_new      = len(diff[NEW])
    n_info     = len(diff[INFO]) + len(diff["removed_schemas"]) + len(diff["added_schemas"])
    n_removed_uncov = len(diff["removed"]) - n_removed_cov
    total_critical = n_critical + n_removed_cov

    has_any = total_critical or n_warning or n_new or n_info or n_removed_uncov

    lines.append(f"## {title}")
    lines.append("")

    if not has_any:
        lines.append("_No changes detected._")
        return "\n".join(lines)

    # Summary verdict
    parts = []
    if total_critical:
        parts.append(f"🔴 **{total_critical} critical**")
    if n_warning:
        parts.append(f"🟡 {n_warning} warning{'s' if n_warning != 1 else ''}")
    if n_new:
        parts.append(f"🟢 {n_new} new endpoint{'s' if n_new != 1 else ''} (not in CLI)")
    if n_info or n_removed_uncov:
        i = n_info + n_removed_uncov
        parts.append(f"ℹ️ {i} info")

    lines.append("> " + " · ".join(parts))
    if not has_coverage:
        lines.append(">")
        lines.append("> _No coverage file provided — all changes shown as INFO._")
    lines.append("")

    def op_block(method: str, path: str, summary: str, changes: list[str]) -> list[str]:
        s = f" — {summary}" if summary else ""
        out = [f"**`{method} {path}`**{s}"]
        for c in changes:
            out.append(f"  - {c}")
        return out

    # Critical: removed covered endpoints
    removed_cov = [(m, p, s, c) for m, p, s, c in diff["removed"] if c]
    removed_uncov = [(m, p, s, c) for m, p, s, c in diff["removed"] if not c]

    if removed_cov or diff[CRITICAL]:
        lines.append("### 🔴 Critical — breaking changes to CLI-implemented endpoints")
        lines.append("")
        for method, path, summary, cov in removed_cov:
            lines.append(f"**`{method} {path}`** — {summary}")
            lines.append("  - ❌ **Endpoint removed from spec**")
            lines.append("")
        for method, path, summary, changes in diff[CRITICAL]:
            lines.extend(op_block(method, path, summary, changes))
            lines.append("")

    if diff[WARNING]:
        lines.append("### 🟡 Warnings — CLI-implemented endpoints changed")
        lines.append("")
        for method, path, summary, changes in diff[WARNING]:
            lines.extend(op_block(method, path, summary, changes))
            lines.append("")

    if diff[NEW]:
        lines.append("### 🟢 New spec endpoints not yet in CLI")
        lines.append("")
        for method, path, summary in diff[NEW]:
            s = f" — {summary}" if summary else ""
            lines.append(f"- `{method} {path}`{s}")
        lines.append("")

    info_items = diff[INFO] + (
        [("—", s, "Schema removed", []) for s in diff["removed_schemas"]] +
        [("—", s, "Schema added",   []) for s in diff["added_schemas"]]
    )
    if info_items or removed_uncov:
        lines.append("### ℹ️ Informational — spec changes with no current CLI impact")
        lines.append("")
        for method, path, summary, cov in removed_uncov:
            lines.append(f"- ~~`{method} {path}`~~ removed from spec")
        for method, path, summary, changes in diff[INFO]:
            if changes:
                lines.extend(op_block(method, path, summary, changes))
                lines.append("")
            else:
                s = f" — {summary}" if summary else ""
                lines.append(f"- `{method} {path}`{s}")
        for name in diff["added_schemas"]:
            lines.append(f"- New schema: `{name}`")
        for name in diff["removed_schemas"]:
            lines.append(f"- Removed schema: `{name}`")
        lines.append("")

    return "\n".join(lines).rstrip()


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main() -> int:
    parser = argparse.ArgumentParser(description="Deep-diff two OpenAPI YAML snapshots")
    parser.add_argument("old",  help="Path to the old snapshot")
    parser.add_argument("new",  help="Path to the new snapshot")
    parser.add_argument("--title",    default="API changes",
                        help="Section heading in the Markdown output")
    parser.add_argument("--coverage", metavar="FILE",
                        help="YAML file mapping CLI-implemented endpoints (see docs/specs/cli-coverage.yaml)")
    parser.add_argument("--no-exit-on-diff", action="store_true",
                        help="Always exit 0, even when differences are found")
    args = parser.parse_args()

    for p in (args.old, args.new):
        if not Path(p).exists():
            print(f"ERROR: {p} not found", file=sys.stderr)
            return 2

    if args.coverage and not Path(args.coverage).exists():
        print(f"ERROR: coverage file {args.coverage} not found", file=sys.stderr)
        return 2

    old = load_yaml(args.old)
    new = load_yaml(args.new)
    covered = load_coverage(args.coverage)

    diff = diff_specs(old, new, covered)
    md = render_markdown(diff, args.title, has_coverage=bool(args.coverage))
    print(md)

    has_changes = any([
        diff[CRITICAL], diff[WARNING], diff[NEW],
        diff[INFO], diff["removed"],
        diff["added_schemas"], diff["removed_schemas"],
    ])
    if has_changes and not args.no_exit_on_diff:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
