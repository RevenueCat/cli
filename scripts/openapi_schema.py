from urllib.parse import unquote


def resolve_ref(node: dict, spec: dict) -> dict:
    """Follow local JSON pointers without expanding recursive schemas."""
    seen = set()
    while (ref := node.get("$ref")) and ref.startswith("#/") and ref not in seen:
        seen.add(ref)
        target = spec
        for part in unquote(ref[2:]).split("/"):
            key = part.replace("~1", "/").replace("~0", "~")
            target = target.get(key) if isinstance(target, dict) else None
        if not isinstance(target, dict):
            break
        node = target
    return node
