"""Stand-ins for the competitor-playbook demo (DEMO.md).

  python3 scripts/demo_competitor_site.py            # serve the "before" prices
  python3 scripts/demo_competitor_site.py --raise    # switch the page to the "after" prices

Serves a fake competitor, "Testco", at http://127.0.0.1:18701/pricing (robots.txt
allows it) and a fake Slack incoming webhook at http://127.0.0.1:18702/hook that
prints every message it receives. The pricing page is read from a file on each
request, so `--raise` (run in another terminal) changes it while the server runs.
Nothing here is used by the product; it only lets the demo show a real change
without waiting for a real competitor.
"""
import argparse
import json
import pathlib
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PAGE = pathlib.Path(tempfile.gettempdir()) / "agently-demo-pricing.html"

BEFORE = """<html><head><title>Testco Pricing</title></head><body><h1>Pricing</h1>
<div><h2>Starter</h2><p>$19 per seat / month</p><ul><li>Up to 10 seats</li><li>Email support</li></ul></div>
<div><h2>Business</h2><p>$49 per seat / month</p><ul><li>SSO</li><li>Priority support</li></ul></div>
</body></html>"""

AFTER = """<html><head><title>Testco Pricing</title></head><body><h1>Pricing</h1>
<div><h2>Starter</h2><p>$24 per seat / month</p><ul><li>Up to 10 seats</li><li>Email support</li></ul></div>
<div><h2>Business</h2><p>$59 per seat / month</p><ul><li>Priority support</li></ul></div>
<div><h2>Enterprise</h2><p>Contact sales</p><ul><li>SSO</li><li>Audit logs</li></ul></div>
</body></html>"""


class Site(BaseHTTPRequestHandler):
    def do_GET(self):  # noqa: N802
        if self.path == "/robots.txt":
            body, ctype = b"User-agent: *\nAllow: /\n", "text/plain"
        elif self.path == "/pricing":
            body, ctype = PAGE.read_bytes(), "text/html"
        else:
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class Hook(BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802
        body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
        try:
            text = json.loads(body).get("text", body.decode())
        except ValueError:
            text = body.decode(errors="replace")
        print("\n--- Slack message received ---\n" + text + "\n------------------------------", flush=True)
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

    def log_message(self, *args):
        pass


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--raise", dest="raise_prices", action="store_true", help="switch to the raised prices and exit")
    args = ap.parse_args()
    if args.raise_prices:
        PAGE.write_text(AFTER)
        print("Testco's pricing page now shows the raised prices.")
        return
    PAGE.write_text(BEFORE)
    threading.Thread(target=ThreadingHTTPServer(("127.0.0.1", 18701), Site).serve_forever, daemon=True).start()
    print("Testco pricing:  http://127.0.0.1:18701/pricing\nSlack webhook:   http://127.0.0.1:18702/hook", flush=True)
    ThreadingHTTPServer(("127.0.0.1", 18702), Hook).serve_forever()


if __name__ == "__main__":
    main()
