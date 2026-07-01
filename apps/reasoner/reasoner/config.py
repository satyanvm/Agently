"""Configuration, loaded from the repo-root .env (shared with the Go services)."""
from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from dotenv import load_dotenv

# The reasoner lives at apps/reasoner; the shared .env is two levels up at the repo
# root. Load it best-effort so DATABASE_URL / keys are present without re-exporting.
_REPO_ROOT = Path(__file__).resolve().parents[2]
load_dotenv(_REPO_ROOT / ".env")


def _clean(url: str | None) -> str | None:
    """Strip a Supabase-style pooler suffix is not needed; just return as-is."""
    return url


@dataclass(frozen=True)
class Config:
    # Shared Agently Postgres — the single source of truth the Go API serves.
    database_url: str
    # Temporal.
    temporal_hostport: str
    temporal_namespace: str
    temporal_task_queue: str
    # Langfuse (optional — tracing degrades gracefully if unset).
    langfuse_host: str
    langfuse_public_key: str
    langfuse_secret_key: str
    # Models.
    model: str
    synthesis_model: str
    anthropic_api_key: str
    # Browserbase (optional — falls back to a simulated browse if unset).
    browserbase_api_key: str
    browserbase_project_id: str
    # SMTP (optional — output.email falls back to record-intent if unset). Env var
    # names mirror the Go worker's notifier (apps/worker/internal/notifier).
    smtp_host: str
    smtp_port: str
    smtp_user: str
    smtp_pass: str
    smtp_from: str
    # How often the dispatcher polls Postgres for queued temporal runs (seconds).
    dispatch_interval_s: float

    @property
    def langfuse_enabled(self) -> bool:
        return bool(self.langfuse_public_key and self.langfuse_secret_key)

    @property
    def browserbase_enabled(self) -> bool:
        return bool(self.browserbase_api_key and self.browserbase_project_id)

    @property
    def smtp_enabled(self) -> bool:
        return bool(self.smtp_host)


def load() -> Config:
    db = _clean(os.getenv("DATABASE_URL"))
    if not db:
        raise SystemExit(
            "DATABASE_URL is required — the reasoner writes run state into the "
            "shared Agently Postgres. Set it in .env."
        )
    return Config(
        database_url=db,
        temporal_hostport=os.getenv("TEMPORAL_HOSTPORT", "localhost:7233"),
        temporal_namespace=os.getenv("TEMPORAL_NAMESPACE", "default"),
        temporal_task_queue=os.getenv("TEMPORAL_TASK_QUEUE", "agently-reasoner"),
        langfuse_host=os.getenv("LANGFUSE_HOST", "http://localhost:3001"),
        langfuse_public_key=os.getenv("LANGFUSE_PUBLIC_KEY", ""),
        langfuse_secret_key=os.getenv("LANGFUSE_SECRET_KEY", ""),
        model=os.getenv("REASONER_MODEL", "claude-sonnet-4-6"),
        synthesis_model=os.getenv("REASONER_SYNTHESIS_MODEL", "claude-opus-4-8"),
        anthropic_api_key=os.getenv("ANTHROPIC_API_KEY", ""),
        browserbase_api_key=os.getenv("BROWSERBASE_API_KEY", ""),
        browserbase_project_id=os.getenv("BROWSERBASE_PROJECT_ID", ""),
        smtp_host=os.getenv("SMTP_HOST", ""),
        smtp_port=os.getenv("SMTP_PORT", "587"),
        smtp_user=os.getenv("SMTP_USER", ""),
        smtp_pass=os.getenv("SMTP_PASS", ""),
        # from = SMTP_FROM, then SMTP_USER (mirrors the Go notifier's fallback).
        smtp_from=os.getenv("SMTP_FROM", "") or os.getenv("SMTP_USER", ""),
        dispatch_interval_s=float(os.getenv("REASONER_DISPATCH_INTERVAL_S", "1.5")),
    )


CONFIG = load()
