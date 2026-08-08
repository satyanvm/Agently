"""Sandboxed code execution for tool.code and catalog runtime="code" nodes.

Isolation model — honest scope: this is PROCESS-level containment, not a hardened
VM. It raises the cost of an accident (runaway loop, memory bomb, secret exfil via
env), it does not defend against a determined kernel exploit. Accordingly the whole
feature is OFF unless the operator sets TOOL_CODE_ENABLED=1. Attempting to use it
while disabled fails the node explicitly.

What the child process gets:
  - a hard wall-clock timeout (killed on expiry),
  - RLIMIT_CPU / RLIMIT_AS / RLIMIT_FSIZE / RLIMIT_NPROC caps (POSIX),
  - a STRIPPED environment (PATH + HOME=tmp only — no DATABASE_URL, no API keys),
  - an isolated temp working directory, deleted afterwards,
  - the run payload as JSON on stdin: {"input": …, "outputs": …, "config": …}.

Contract with the program: print a JSON object on the LAST line of stdout to emit
structured fields (exposed as output "result"); all stdout is captured (clipped)
as "stdout".
"""
from __future__ import annotations

import asyncio
import json
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any

_TIMEOUT_S = 30
_CPU_S = 15
_MEM_BYTES = 512 * 1024 * 1024
_OUT_CLIP = 16_000


@dataclass
class CodeResult:
    ok: bool
    result: Any = None       # parsed JSON object from the last stdout line, if any
    stdout: str = ""
    error: str = ""


def _limit_resources() -> None:  # pragma: no cover — runs in the child pre-exec
    import resource

    resource.setrlimit(resource.RLIMIT_CPU, (_CPU_S, _CPU_S))
    resource.setrlimit(resource.RLIMIT_FSIZE, (8 * 1024 * 1024, 8 * 1024 * 1024))
    try:
        resource.setrlimit(resource.RLIMIT_NPROC, (32, 32))
    except (ValueError, OSError):
        pass  # not adjustable on all platforms
    try:
        resource.setrlimit(resource.RLIMIT_AS, (_MEM_BYTES, _MEM_BYTES))
    except (ValueError, OSError):
        pass  # macOS rejects RLIMIT_AS below current usage; the CPU cap still holds


async def run(language: str, source: str, payload: dict[str, Any]) -> CodeResult:
    """Execute `source` with the payload on stdin; never raises."""
    return await asyncio.to_thread(_run_blocking, language, source, payload)


def _run_blocking(language: str, source: str, payload: dict[str, Any]) -> CodeResult:
    lang = (language or "python").strip().lower()
    tmp = tempfile.mkdtemp(prefix="agently-code-")
    env = {"PATH": "/usr/bin:/bin:/usr/local/bin", "HOME": tmp}
    try:
        # The payload arrives on stdin, so the script itself goes to a file.
        if lang in ("python", "python3", "py"):
            script = Path(tmp) / "main.py"
            argv = [sys.executable, "-I", str(script)]  # -I: isolated mode
        elif lang in ("javascript", "js", "node"):
            node = shutil.which("node")
            if not node:
                return CodeResult(ok=False, error="node interpreter not available")
            script = Path(tmp) / "main.js"
            argv = [node, str(script)]
        else:
            return CodeResult(ok=False, error=f"no interpreter available for language '{language}'")

        script.write_text(source)

        try:
            proc = subprocess.run(
                argv,
                input=json.dumps(payload, default=str),
                capture_output=True,
                text=True,
                timeout=_TIMEOUT_S,
                cwd=tmp,
                env=env,
                preexec_fn=_limit_resources if sys.platform != "win32" else None,
            )
        except subprocess.TimeoutExpired:
            return CodeResult(ok=False, error=f"timed out after {_TIMEOUT_S}s")
        except OSError as exc:
            return CodeResult(ok=False, error=str(exc))

        stdout = (proc.stdout or "")[:_OUT_CLIP]
        if proc.returncode != 0:
            err = (proc.stderr or "")[:2000] or f"exit code {proc.returncode}"
            return CodeResult(ok=False, stdout=stdout, error=err)

        result: Any = None
        for line in reversed(stdout.strip().splitlines()):
            line = line.strip()
            if line.startswith("{") and line.endswith("}"):
                try:
                    result = json.loads(line)
                except ValueError:
                    pass
                break
        return CodeResult(ok=True, result=result, stdout=stdout)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
