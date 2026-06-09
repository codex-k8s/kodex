import pathlib
import sys
import unittest


SERVICE_DIR = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_DIR))

import app  # noqa: E402


class BotServiceTest(unittest.TestCase):
    def test_load_config_uses_safe_defaults(self):
        config = app.load_config({})

        self.assertEqual(config.port, 8080)
        self.assertEqual(config.default_team_name, "agents")
        self.assertEqual(
            config.default_channels,
            ("agents-control", "agents-runs", "agent-alerts", "agents-audit"),
        )
        self.assertFalse(config.bot_token_configured)
        self.assertFalse(config.slash_token_configured)

    def test_health_payload_does_not_expose_tokens(self):
        config = app.load_config(
            {
                "MATTERCODEX_MATTERMOST_BOT_TOKEN": "secret-bot-token",
                "MATTERCODEX_MATTERMOST_SLASH_TOKEN": "secret-slash-token",
            }
        )

        payload = app.health_payload(config)

        self.assertTrue(payload["bot_token_configured"])
        self.assertTrue(payload["slash_token_configured"])
        self.assertNotIn("secret-bot-token", repr(payload))
        self.assertNotIn("secret-slash-token", repr(payload))

    def test_slash_rejects_missing_configured_token(self):
        config = app.load_config({})

        status, payload = app.slash_response(config, {"text": "status"})

        self.assertEqual(status, 503)
        self.assertIn("slash token", payload["text"])

    def test_slash_rejects_wrong_token(self):
        config = app.load_config({"MATTERCODEX_MATTERMOST_SLASH_TOKEN": "expected"})

        status, payload = app.slash_response(config, {"token": "wrong", "text": "status"})

        self.assertEqual(status, 401)
        self.assertIn("token", payload["text"])

    def test_slash_status_response(self):
        config = app.load_config(
            {
                "MATTERCODEX_MATTERMOST_SITE_URL": "https://mattermost.example.test",
                "MATTERCODEX_MATTERMOST_BOT_TOKEN": "bot",
                "MATTERCODEX_MATTERMOST_SLASH_TOKEN": "slash",
            }
        )

        status, payload = app.slash_response(config, {"token": "slash", "text": "status"})

        self.assertEqual(status, 200)
        self.assertIn("matter-codex: online", payload["text"])
        self.assertIn("bot token: configured", payload["text"])


if __name__ == "__main__":
    unittest.main()
