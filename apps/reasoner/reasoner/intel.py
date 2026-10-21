"""Page snapshots and diffs for competitor watching (pure logic + fetching).

The intel.* node handlers (intel_nodes.py) build on three steps here:

  fetch      plain HTTP (robots.txt honoured) or Browserbase for JS-only pages
  normalize  HTML → the visible text a person would read, one block per line,
             with noise that changes on every load (timestamps, "5 minutes
             ago", copyright years) removed so it never looks like a change
  diff       line-level added / removed between two normalized versions

Everything here is deterministic except fetch. A page that can't be fetched,
or that robots.txt disallows, raises with the reason — it never becomes an
empty "no change" snapshot.
"""
from __future__ import annotations

import difflib
import hashlib
import re
import urllib.parse
import urllib.robotparser
from dataclasses import dataclass, field
from html.parser import HTMLParser

import httpx

# The text we keep per snapshot (normalized pricing/changelog pages are far
# smaller; this only bounds pathological pages).
MAX_CONTENT_CHARS = 200_000
# Lines reported per diff side; the summary still counts all of them.
MAX_DIFF_LINES = 200

_SKIP_TAGS = {"script", "style", "noscript", "template", "svg", "head", "iframe", "canvas"}
_BLOCK_TAGS = {
    "p", "div", "section", "article", "header", "footer", "main", "aside", "nav",
    "li", "ul", "ol", "tr", "td", "th", "table", "thead", "tbody", "h1", "h2", "h3",
    "h4", "h5", "h6", "br", "hr", "blockquote", "pre", "dt", "dd", "dl", "figure",
    "figcaption", "form", "label", "button", "option", "summary", "details",
}

# Whole lines that are pure noise: they change between loads without the page
# meaning anything different.
_NOISE_LINES = [
    re.compile(r"^(updated|posted|published|last updated)?\s*:?\s*\d+\s+(second|minute|hour|day|week|month|year)s?\s+ago\.?$", re.I),
    re.compile(r"^(©|\(c\)|copyright)\s*(\d{4}\s*[-–]\s*)?\d{4}\b.*$", re.I),
    # A bare date or time stamp on its own line.
    re.compile(r"^\d{1,4}[-/.]\d{1,2}[-/.]\d{1,4}(\s+\d{1,2}:\d{2}(:\d{2})?\s*(am|pm|utc|gmt)?)?$", re.I),
    re.compile(r"^\d{1,2}:\d{2}(:\d{2})?\s*(am|pm|utc|gmt)?$", re.I),
]


class _TextExtractor(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.parts: list[str] = []
        self.title = ""
        self._skip = 0
        self._in_title = False

    def handle_starttag(self, tag: str, attrs) -> None:  # noqa: ANN001
        if tag == "title":
            self._in_title = True
        if tag in _SKIP_TAGS:
            self._skip += 1
        elif tag in _BLOCK_TAGS:
            self.parts.append("\n")

    def handle_endtag(self, tag: str) -> None:
        if tag == "title":
            self._in_title = False
        if tag in _SKIP_TAGS:
            self._skip = max(0, self._skip - 1)
        elif tag in _BLOCK_TAGS:
            self.parts.append("\n")

    def handle_data(self, data: str) -> None:
        if self._in_title:
            self.title += data
            return
        if not self._skip:
            self.parts.append(data)


def html_to_text(html: str) -> tuple[str, str]:
    """Return (title, visible text) for an HTML document."""
    parser = _TextExtractor()
    parser.feed(html)
    parser.close()
    return " ".join(parser.title.split()), "".join(parser.parts)


def normalize_text(text: str, ignore: list[str] | None = None) -> str:
    """Visible text → stable lines: collapse whitespace, drop blank and noise
    lines (plus any matching a caller's `ignore` regex), and drop consecutive
    duplicates. Numbers inside lines are kept — prices ARE the signal."""
    extra = [re.compile(p, re.I) for p in (ignore or []) if p.strip()]
    out: list[str] = []
    for raw in text.splitlines():
        line = " ".join(raw.split())
        if not line:
            continue
        if any(p.search(line) for p in _NOISE_LINES) or any(p.search(line) for p in extra):
            continue
        if out and out[-1] == line:
            continue
        out.append(line)
    return "\n".join(out)[:MAX_CONTENT_CHARS]


def content_hash(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


@dataclass
class Diff:
    added: list[str] = field(default_factory=list)
    removed: list[str] = field(default_factory=list)
    added_count: int = 0
    removed_count: int = 0

    @property
    def changed(self) -> bool:
        return bool(self.added_count or self.removed_count)

    def summary(self) -> str:
        if not self.changed:
            return "No change"
        parts = []
        if self.added_count:
            parts.append(f"{self.added_count} line{'s' if self.added_count != 1 else ''} added")
        if self.removed_count:
            parts.append(f"{self.removed_count} line{'s' if self.removed_count != 1 else ''} removed")
        return ", ".join(parts)


def diff_text(old: str, new: str) -> Diff:
    """Line diff of two normalized texts (order-aware, like `diff`)."""
    d = Diff()
    for line in difflib.ndiff(old.splitlines(), new.splitlines()):
        tag, body = line[:2], line[2:]
        if tag == "+ ":
            d.added_count += 1
            if len(d.added) < MAX_DIFF_LINES:
                d.added.append(body)
        elif tag == "- ":
            d.removed_count += 1
            if len(d.removed) < MAX_DIFF_LINES:
                d.removed.append(body)
    return d


# ─────────────────────────── fetching ───────────────────────────

@dataclass
class Fetched:
    url: str           # final URL after redirects
    status: int
    title: str
    text: str          # normalized


class FetchError(RuntimeError):
    pass


def _robots_url(url: str) -> str:
    parts = urllib.parse.urlsplit(url)
    return urllib.parse.urlunsplit((parts.scheme, parts.netloc, "/robots.txt", "", ""))


async def robots_allows(client: httpx.AsyncClient, url: str, user_agent: str) -> bool:
    """True unless the site's robots.txt disallows `url` for our user agent.
    A missing robots.txt (4xx) allows everything; an unreachable one (5xx,
    network error) is treated as disallow, as RFC 9309 recommends."""
    try:
        resp = await client.get(_robots_url(url))
    except httpx.HTTPError as exc:
        raise FetchError(f"could not read robots.txt for {url}: {exc}") from exc
    if 400 <= resp.status_code < 500:
        return True
    if resp.status_code >= 500:
        raise FetchError(f"robots.txt for {url} returned {resp.status_code}; not fetching")
    rp = urllib.robotparser.RobotFileParser()
    rp.parse(resp.text.splitlines())
    return rp.can_fetch(user_agent, url)


async def fetch_http(url: str, user_agent: str, ignore: list[str] | None = None,
                     timeout_s: float = 30) -> Fetched:
    """GET a page over plain HTTP and normalize it. Raises FetchError with the
    reason on robots.txt disallow, non-2xx status, or a non-HTML/text body."""
    headers = {"User-Agent": user_agent, "Accept": "text/html,application/xhtml+xml,text/plain;q=0.9"}
    async with httpx.AsyncClient(timeout=timeout_s, follow_redirects=True, headers=headers) as client:
        if not await robots_allows(client, url, user_agent):
            raise FetchError(f"robots.txt disallows fetching {url}")
        try:
            resp = await client.get(url)
        except httpx.HTTPError as exc:
            raise FetchError(f"GET {url} failed: {exc}") from exc
    if resp.status_code >= 400:
        raise FetchError(f"GET {url} returned {resp.status_code}")
    ctype = resp.headers.get("content-type", "")
    if ctype and not any(t in ctype for t in ("html", "text/plain", "xml")):
        raise FetchError(f"GET {url} returned {ctype}, not a web page")
    if "html" in ctype or "<html" in resp.text[:2000].lower():
        title, text = html_to_text(resp.text)
    else:
        title, text = "", resp.text
    return Fetched(url=str(resp.url), status=resp.status_code, title=title,
                   text=normalize_text(text, ignore))
