#!/usr/bin/env python3
"""Preprocess the RevenueCat OpenAPI spec for oapi-codegen compatibility.

Fixes fields that use the broken pattern:
    field:
      allOf:
        - { nullable: true, type: object, description: "..." }
        - { $ref: '#/components/schemas/SomeSchema' }

Rewrites them as:
    field:
      nullable: true
      description: "..."
      allOf:
        - { $ref: '#/components/schemas/SomeSchema' }

Usage: python3 scripts/preprocess-spec.py INPUT OUTPUT
"""

import sys
import yaml


def fix_nullable_allof(obj):
    """Recursively fix nullable+allOf patterns that break oapi-codegen."""
    if isinstance(obj, dict):
        if "allOf" in obj and isinstance(obj["allOf"], list):
            new_allof = []
            hoisted = {}
            for item in obj["allOf"]:
                if isinstance(item, dict) and "$ref" not in item and item.get("nullable"):
                    # This is a nullable descriptor without a $ref — hoist its fields
                    for k, v in item.items():
                        if k != "type":  # drop bare 'type: object' — it's implied by $ref
                            hoisted[k] = v
                else:
                    new_allof.append(fix_nullable_allof(item))

            if hoisted:
                # Merge hoisted fields into the parent object, keep allOf with only $refs
                result = {k: v for k, v in obj.items() if k != "allOf"}
                result.update(hoisted)
                if new_allof:
                    result["allOf"] = new_allof
                return result

        return {k: fix_nullable_allof(v) for k, v in obj.items()}
    elif isinstance(obj, list):
        return [fix_nullable_allof(item) for item in obj]
    return obj


def main():
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} INPUT OUTPUT", file=sys.stderr)
        sys.exit(1)

    input_path, output_path = sys.argv[1], sys.argv[2]

    with open(input_path) as f:
        spec = yaml.safe_load(f)

    spec = fix_nullable_allof(spec)

    with open(output_path, "w") as f:
        yaml.dump(spec, f, allow_unicode=True, sort_keys=False, default_flow_style=False)

    print(f"Wrote preprocessed spec to {output_path}")


if __name__ == "__main__":
    main()
