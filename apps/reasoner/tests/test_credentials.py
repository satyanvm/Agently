import os
import unittest

os.environ.setdefault("DATABASE_URL", "postgresql://fake/fake")
os.environ.setdefault("GEMINI_API_KEY", "test-key")
os.environ.setdefault("LANGFUSE_PUBLIC_KEY", "test-public")
os.environ.setdefault("LANGFUSE_SECRET_KEY", "test-secret")
os.environ.setdefault("BROWSERBASE_API_KEY", "test-browserbase")
os.environ.setdefault("BROWSERBASE_PROJECT_ID", "test-project")
os.environ.setdefault("SMTP_HOST", "smtp.test")
os.environ.setdefault("SMTP_USER", "test")
os.environ.setdefault("SMTP_PASS", "test")

from unittest import mock  # noqa: E402

from reasoner.db import open_credential_data  # noqa: E402

KEY = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
# Sealed by the API's platform.sealCredentialData under KEY.
SEALED = {"$enc": "v1:6wvhxAZvcovFUtWBtR7EGXe9usisTno4XQzaftondT0Vu8bfhaKqXjTqqbgWcG06"}


class OpenCredentialDataTest(unittest.TestCase):
    def test_opens_api_sealed_value(self):
        with mock.patch.dict(os.environ, {"AGENTLY_CREDENTIALS_KEY": KEY}):
            self.assertEqual(open_credential_data(SEALED), {"token": "xoxb-123"})

    def test_passes_through_legacy_plaintext(self):
        self.assertEqual(open_credential_data({"token": "plain"}), {"token": "plain"})

    def test_sealed_without_key_fails(self):
        with mock.patch.dict(os.environ, {"AGENTLY_CREDENTIALS_KEY": ""}):
            with self.assertRaises(RuntimeError):
                open_credential_data(SEALED)

    def test_wrong_key_fails(self):
        with mock.patch.dict(os.environ, {"AGENTLY_CREDENTIALS_KEY": "ff" * 32}):
            with self.assertRaises(RuntimeError):
                open_credential_data(SEALED)


if __name__ == "__main__":
    unittest.main()
