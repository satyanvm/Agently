"""Reasoner entrypoint.

Runs two things side by side on one Temporal connection:
  1. A Temporal Worker that executes ReasoningWorkflow + the LangGraph node activities
     (registered via LangGraphPlugin).
  2. A dispatcher loop that turns engine='temporal' queued runs into workflow starts.

  python -m reasoner.worker
"""
from __future__ import annotations

import asyncio
import logging
import signal

from temporalio.client import Client
from temporalio.contrib.langgraph import LangGraphPlugin
from temporalio.worker import Worker

from .config import CONFIG
from .dispatcher import run_dispatcher
from .graph import GRAPH_NAME, build_graph
from .workflow import ReasoningWorkflow

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
log = logging.getLogger("reasoner")


async def main() -> None:
    log.info("connecting to Temporal at %s (ns=%s, queue=%s)",
             CONFIG.temporal_hostport, CONFIG.temporal_namespace, CONFIG.temporal_task_queue)

    plugin = LangGraphPlugin(graphs={GRAPH_NAME: build_graph()})
    client = await Client.connect(
        CONFIG.temporal_hostport,
        namespace=CONFIG.temporal_namespace,
    )

    stop = asyncio.Event()
    _install_signal_handlers(stop)

    worker = Worker(
        client,
        task_queue=CONFIG.temporal_task_queue,
        workflows=[ReasoningWorkflow],
        plugins=[plugin],
    )

    log.info("reasoner up: langfuse=%s browserbase=%s model=%s",
             CONFIG.langfuse_enabled, CONFIG.browserbase_enabled, CONFIG.model)

    async with worker:
        dispatcher = asyncio.create_task(run_dispatcher(client, stop))
        await stop.wait()
        dispatcher.cancel()
        try:
            await dispatcher
        except asyncio.CancelledError:
            pass


def _install_signal_handlers(stop: asyncio.Event) -> None:
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, stop.set)
        except NotImplementedError:  # pragma: no cover — e.g. Windows
            pass


if __name__ == "__main__":
    asyncio.run(main())
