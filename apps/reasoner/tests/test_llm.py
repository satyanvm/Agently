"""llm._content_text: Gemini 3 returns content blocks, not a string."""
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

from dataclasses import replace  # noqa: E402

from reasoner import config, llm  # noqa: E402


class ContentTextTest(unittest.TestCase):
    def test_string_passes_through(self):
        self.assertEqual(llm._content_text("hi"), "hi")

    def test_blocks_keep_only_text_in_order(self):
        content = [
            {"type": "thinking", "thinking": "hmm"},
            {"type": "text", "text": "Hello, "},
            {"type": "text", "text": "world", "extras": {"signature": "x"}},
        ]
        self.assertEqual(llm._content_text(content), "Hello, world")


class ProviderTest(unittest.TestCase):
    def test_ollama_model_builds_chat_ollama(self):
        m = llm._chat_model("ollama/qwen3:8b")
        self.assertEqual(type(m).__name__, "ChatOllama")
        self.assertEqual(m.model, "qwen3:8b")
        self.assertEqual(m.num_ctx, llm.CONFIG.ollama_num_ctx)
        self.assertFalse(m.reasoning)

    def test_local_models_are_free(self):
        self.assertEqual(llm._estimate_cost("ollama/qwen3:8b", 10_000, 10_000), 0.0)

    def test_gemini_model_without_key_names_the_key(self):
        orig = llm.CONFIG
        llm.CONFIG = replace(orig, gemini_api_key="")
        try:
            with self.assertRaisesRegex(RuntimeError, "GEMINI_API_KEY"):
                llm._chat_model("gemini-3.5-flash-lite")
        finally:
            llm.CONFIG = orig

    def test_gemini_key_optional_when_all_models_local(self):
        saved = os.environ.pop("GEMINI_API_KEY", None)
        try:
            self.assertEqual(config._gemini_key("ollama/a", "ollama/b"), "")
            with self.assertRaises(SystemExit):
                config._gemini_key("ollama/a", "gemini-3.5-flash")
        finally:
            if saved is not None:
                os.environ["GEMINI_API_KEY"] = saved


if __name__ == "__main__":
    unittest.main()
