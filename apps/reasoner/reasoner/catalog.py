"""Loader for the shared integration-node catalog (packages/nodes/catalog).

The catalog is the same data the Go compiler plans against and the web palette
renders. Here it powers the GENERIC integration executor (nodes.py `_integration`):
any GraphNode whose `type` matches a catalog id executes via the definition's
declared runtime (http / browser / code / llm) — hundreds of integrations, one
handler, zero per-service code.

Loaded once at import, like CONFIG. A missing catalog directory IS an error: it
used to silently reduce the platform to the built-in types, so every integration
node in an otherwise valid workflow became a pass-through nobody was told about.
"""
from __future__ import annotations

import json
import logging
import os
from pathlib import Path
from typing import Any

log = logging.getLogger("reasoner.catalog")

# apps/reasoner/reasoner/catalog.py → repo root is three levels up.
_REPO_ROOT = Path(__file__).resolve().parents[3]


def _catalog_dir() -> Path | None:
    if env := os.getenv("NODES_CATALOG_DIR"):
        p = Path(env)
        return p if p.is_dir() else None
    p = _REPO_ROOT / "packages" / "nodes" / "catalog"
    return p if p.is_dir() else None


def _load() -> dict[str, dict[str, Any]]:
    by_id: dict[str, dict[str, Any]] = {}
    directory = _catalog_dir()
    if directory is None:
        raise SystemExit(
            "Node catalog not found — every integration node depends on it. Expected "
            "packages/nodes/catalog (or set NODES_CATALOG_DIR)."
        )
    for path in sorted(directory.glob("*.json")):
        try:
            data = json.loads(path.read_text())
        except (OSError, ValueError) as exc:
            # Skipping a corrupt cluster file makes every node it defines vanish
            # from the executor while the compiler still happily plans against it.
            raise SystemExit(f"Node catalog file {path.name} is unreadable: {exc}") from exc
        cluster = data.get("cluster", path.stem)
        for node in data.get("nodes", []):
            node_id = node.get("id")
            if not node_id:
                continue
            node["cluster"] = cluster
            by_id[node_id] = node
    log.info("node catalog loaded: %d types", len(by_id))
    return by_id


_BY_ID = _load()


def spec_for(node_type: str) -> dict[str, Any] | None:
    """Full catalog definition for a node type, or None if unknown."""
    return _BY_ID.get(node_type)


def size() -> int:
    return len(_BY_ID)
