"""Run-time half of "nothing publishes without human approval".

packages/nodes/publishing.json lists the node types that publish publicly.
The API refuses to plan or save a graph where one has no human.approval
upstream (apps/api services/publishing.go); this checks again when a run
loads its graph, so a graph saved before the rule existed can't publish
either. A missing list fails closed (no run can start) — the same file the
API reads, so the two can't disagree.
"""
from __future__ import annotations

import json
from functools import lru_cache
from typing import Any

from . import catalog

APPROVAL_TYPE = "human.approval"


@lru_cache(maxsize=1)
def publishing_types() -> frozenset[str]:
    directory = catalog._catalog_dir()
    if directory is None:
        raise RuntimeError("packages/nodes/publishing.json not found (no node catalog directory)")
    path = directory.parent / "publishing.json"
    try:
        data = json.loads(path.read_text())
    except (OSError, ValueError) as exc:
        raise RuntimeError(f"cannot read {path}: {exc}") from exc
    types = {f"pieces.{slug}.{a}" for slug, actions in (data.get("pieces") or {}).items() for a in actions}
    types |= set(data.get("types") or [])
    if not types:
        raise RuntimeError(f"{path} lists no publishing node types")
    return frozenset(types)


def unguarded(graph: list[dict[str, Any]]) -> list[str]:
    """Keys of publishing nodes with no human.approval among their ancestors."""
    types = publishing_types()
    by_key = {n["key"]: n for n in graph}

    def approved_above(key: str) -> bool:
        seen: set[str] = set()
        stack = list(by_key[key].get("dependsOn") or [])
        while stack:
            k = stack.pop()
            if k in seen or k not in by_key:
                continue
            seen.add(k)
            if by_key[k].get("type") == APPROVAL_TYPE:
                return True
            stack.extend(by_key[k].get("dependsOn") or [])
        return False

    return sorted(n["key"] for n in graph if n.get("type") in types and not approved_above(n["key"]))
