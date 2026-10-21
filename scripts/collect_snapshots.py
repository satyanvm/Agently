"""Collect raw competitor-page snapshots for measuring signal precision.

  apps/reasoner/.venv/bin/python scripts/collect_snapshots.py \
      --targets scripts/snapshot-targets.json --out ~/agently-snapshots

Fetches every page in the targets file once, the same way intel.snapshotPage
does (robots.txt honoured, Agently's user agent, plain HTTP), and keeps the
RAW HTML as well as the normalized text. The product only stores normalized
text, so a later change to the normalizer couldn't be re-measured on old
snapshots; this archive can be replayed through any version of it.

Run it on a schedule (cron or launchd, every few hours). Each run appends one
line per page to <out>/index.jsonl, and writes <page dir>/<UTC time>.html
plus .txt only when the raw HTML differs from the last one kept. A page that
can't be fetched is recorded with its reason; nothing is invented.

Nothing in the product imports this; it is a measuring tool for step 3 of the
real end-to-end phase.
"""
from __future__ import annotations

import argparse
import asyncio
import datetime as dt
import hashlib
import json
import os
import pathlib
import re
import sys
import urllib.parse

import httpx

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent / "apps" / "reasoner"))
from reasoner import intel  # noqa: E402  (pure module: no config, no database)

USER_AGENT = (os.environ.get("HTTP_USER_AGENT", "").strip()
              or "Agently/1.0 (+https://github.com/satyanvm/Agently)")


def page_dir(out: pathlib.Path, url: str) -> pathlib.Path:
    parts = urllib.parse.urlsplit(url)
    slug = re.sub(r"[^A-Za-z0-9]+", "-", (parts.path + ("?" + parts.query if parts.query else "")).strip("/"))
    return out / parts.netloc / (slug.strip("-") or "root")


def last_raw_hash(directory: pathlib.Path) -> str:
    kept = sorted(directory.glob("*.html"))
    return hashlib.sha256(kept[-1].read_bytes()).hexdigest() if kept else ""


async def snapshot(client: httpx.AsyncClient, out: pathlib.Path, stamp: str,
                   competitor: str, kind: str, url: str) -> dict:
    row = {"at": stamp, "competitor": competitor, "kind": kind, "url": url}
    try:
        if not await intel.robots_allows(client, url, USER_AGENT):
            raise intel.FetchError(f"robots.txt disallows fetching {url}")
        resp = await client.get(url)
        row.update(status=resp.status_code, finalUrl=str(resp.url))
        if resp.status_code >= 400:
            raise intel.FetchError(f"GET {url} returned {resp.status_code}")
        raw = resp.text
        _, text = intel.html_to_text(raw)
        norm = intel.normalize_text(text)
    except Exception as exc:  # noqa: BLE001 - recorded in the index, never hidden
        row["error"] = f"{type(exc).__name__}: {exc}"[:500]
        return row
    raw_hash = hashlib.sha256(raw.encode("utf-8")).hexdigest()
    row.update(rawHash=raw_hash, normHash=intel.content_hash(norm), normLines=len(norm.splitlines()))
    directory = page_dir(out, url)
    directory.mkdir(parents=True, exist_ok=True)
    if raw_hash != last_raw_hash(directory):
        (directory / f"{stamp}.html").write_text(raw, encoding="utf-8")
        (directory / f"{stamp}.txt").write_text(norm, encoding="utf-8")
        row["kept"] = str((directory / f"{stamp}.html").relative_to(out))
    return row


async def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--targets", required=True, help="JSON: [{competitor, pages: [{kind, url}]}]")
    ap.add_argument("--out", required=True, help="archive directory (created if missing)")
    args = ap.parse_args()

    targets = json.loads(pathlib.Path(args.targets).read_text())
    out = pathlib.Path(args.out).expanduser()
    out.mkdir(parents=True, exist_ok=True)
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    headers = {"User-Agent": USER_AGENT, "Accept": "text/html,application/xhtml+xml,text/plain;q=0.9"}
    async with httpx.AsyncClient(timeout=30, follow_redirects=True, headers=headers) as client:
        rows = await asyncio.gather(*(
            snapshot(client, out, stamp, t["competitor"], p["kind"], p["url"])
            for t in targets for p in t["pages"]
        ))
    with (out / "index.jsonl").open("a", encoding="utf-8") as fh:
        for row in rows:
            fh.write(json.dumps(row) + "\n")
    failed = [r for r in rows if "error" in r]
    for r in rows:
        state = "FAIL " + r["error"] if "error" in r else ("new version" if r.get("kept") else "unchanged")
        print(f"{r['competitor']:<10} {r['kind']:<10} {r['url']}  {state}")
    print(f"{len(rows) - len(failed)}/{len(rows)} pages fetched at {stamp}")
    return 1 if len(failed) == len(rows) else 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
