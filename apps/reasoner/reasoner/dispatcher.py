"""Dispatcher: bridge from the Go control plane to Temporal.

The Go API creates a run row with engine='temporal', status='queued' (and never
hands it to the native worker, because claim_next_run is scoped to engine='native').
This loop polls those rows and starts a Temporal workflow for each, using the run id
as the workflow id so dispatch is idempotent — a duplicate start (e.g. two reasoner
instances, or a retry) is rejected by Temporal rather than double-executing. Once a
run's plan node flips status to 'running', it stops being selected.
"""
from __future__ import annotations

import asyncio
import logging

from temporalio.client import Client
from temporalio.service import RPCError

from . import db
from .config import CONFIG
from .workflow import ReasoningWorkflow

log = logging.getLogger("reasoner.dispatcher")


async def run_dispatcher(client: Client, stop: asyncio.Event) -> None:
    log.info("dispatcher polling every %.1fs", CONFIG.dispatch_interval_s)
    while not stop.is_set():
        try:
            await _dispatch_once(client)
        except Exception:  # noqa: BLE001 — keep the loop alive across transient errors
            log.exception("dispatch poll failed")
        try:
            await asyncio.wait_for(stop.wait(), timeout=CONFIG.dispatch_interval_s)
        except asyncio.TimeoutError:
            pass


async def _dispatch_once(client: Client) -> None:
    runs = await db.claim_queued_temporal_runs()
    for run in runs:
        run_id = run["id"]
        workflow_id = f"agently-run-{run_id}"
        params = {
            "run_id": run_id,
            "slug": run.get("slug", ""),
            "input": run.get("input") or {},
        }
        try:
            await client.start_workflow(
                ReasoningWorkflow.run,
                params,
                id=workflow_id,
                task_queue=CONFIG.temporal_task_queue,
            )
            log.info("dispatched run %s → workflow %s", run_id, workflow_id)
        except RPCError as exc:
            # ALREADY_EXISTS means it's already dispatched — idempotent, ignore.
            if "ALREADY_EXISTS" in str(exc) or "already started" in str(exc).lower():
                continue
            raise
