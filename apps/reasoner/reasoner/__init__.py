"""Agently reasoning plane.

Vertical Slice 1 of the unified platform: reasoning runs *inside* durability.

A run launched with engine='temporal' is executed by a LangGraph StateGraph whose
nodes run as Temporal activities — so every reasoning/tool step is durable and
resumable (Temporal's event history is the checkpoint). The graph drives a browser
session (Browserbase) and is traced in self-hosted Langfuse. Crucially, each node
writes its state back into the SAME Postgres tables the Go API already serves, so
the existing Agently UI renders these runs with no frontend rewrite.
"""

__all__ = ["config"]
