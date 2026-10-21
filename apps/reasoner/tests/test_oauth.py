"""OAuth token refresh for catalog nodes (reasoner/oauth.py)."""

import asyncio
import unittest

from reasoner import oauth


class RefreshTests(unittest.TestCase):
    def setUp(self):
        oauth._cache.clear()
        self.env = {"OAUTH_HUBSPOT_CLIENT_ID": "cid", "OAUTH_HUBSPOT_CLIENT_SECRET": "sec"}
        self.calls = []

    async def _post(self, url, form, headers):
        self.calls.append((url, form, headers))
        return {"access_token": "new", "expires_in": 1800, "refresh_token": "rt2"}

    def run_refresh(self, data, now=1000.0):
        return asyncio.run(
            oauth.refresh_if_expired("c1", "hubspot", data, post=self._post, now=lambda: now, env=self.env)
        )

    def test_fresh_token_untouched(self):
        data = {"HUBSPOT_ACCESS_TOKEN": "old", "expires_at": 5000, "refresh_token": "rt", "token_url": "https://t"}
        self.assertEqual(self.run_refresh(data), data)
        self.assertEqual(self.calls, [])

    def test_expired_token_refreshed_into_token_key(self):
        data = {
            "access_token": "old", "HUBSPOT_ACCESS_TOKEN": "old", "token_key": "HUBSPOT_ACCESS_TOKEN",
            "expires_at": 1010, "refresh_token": "rt", "token_url": "https://t",
        }
        out = self.run_refresh(data)
        self.assertEqual(out["HUBSPOT_ACCESS_TOKEN"], "new")
        self.assertEqual(out["access_token"], "new")
        self.assertEqual(out["refresh_token"], "rt2")
        url, form, _ = self.calls[0]
        self.assertEqual((url, form["grant_type"], form["client_id"]), ("https://t", "refresh_token", "cid"))
        # Cached: a second lookup makes no new call.
        self.run_refresh(data)
        self.assertEqual(len(self.calls), 1)

    def test_no_client_config_returns_stored(self):
        self.env = {}
        data = {"access_token": "old", "expires_at": 1, "refresh_token": "rt", "token_url": "https://t"}
        self.assertEqual(self.run_refresh(data), data)

    def test_failed_refresh_returns_stored(self):
        async def fail(*_):
            return None
        data = {"access_token": "old", "expires_at": 1, "refresh_token": "rt", "token_url": "https://t"}
        out = asyncio.run(oauth.refresh_if_expired("c2", "hubspot", data, post=fail, now=lambda: 1000.0, env=self.env))
        self.assertEqual(out, data)


if __name__ == "__main__":
    unittest.main()
