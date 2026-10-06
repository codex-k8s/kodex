"""Герметичные проверки узкой SSO policy; без сети и credentials."""
import copy
import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("session_policy", Path(__file__).with_name("keycloak-session-policy.py"))
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


def fixtures():
    return {
        **policy.POLICY, "ssoSessionIdleTimeout": 28800,
        "ssoSessionIdleTimeoutRememberMe": 0, "ssoSessionMaxLifespanRememberMe": 0,
        "clientSessionIdleTimeout": 0, "clientSessionMaxLifespan": 0,
        "accessTokenLifespan": 300, "revokeRefreshToken": True, "refreshTokenMaxReuse": 0,
    }, {"clientId": "kodex-control-center", "attributes": {"access.token.lifespan": "3600"}}


class FakeAPI(policy.API):
    def __init__(self):
        self.realm, self.client = fixtures()
        self.writes = []

    def read(self, realm_name):
        policy.validate_inherited_timeouts(self.realm, self.client)
        return copy.deepcopy(self.realm), copy.deepcopy(self.client)

    def request(self, path, data=None, method=None, authenticated=False):
        import json
        self.writes.append((path, method, authenticated, json.loads(data)))
        self.realm.update(json.loads(data))


class SessionPolicyTests(unittest.TestCase):
    def test_apply_changes_exact_five_fields(self):
        api = FakeAPI()
        result = api.execute("kodex", "apply")
        self.assertEqual(api.writes, [("/admin/realms/kodex", "PUT", True, policy.POLICY)])
        self.assertEqual(result["realm"]["accessTokenLifespan"], 300)
        self.assertTrue(result["realm"]["revokeRefreshToken"])
        self.assertEqual(result["clientTimeoutAttributes"]["access.token.lifespan"], "3600")

    def test_readback_never_writes(self):
        api = FakeAPI()
        api.realm.update(policy.POLICY)
        api.execute("kodex", "readback")
        self.assertEqual(api.writes, [])

    def test_old_readback_fails_closed(self):
        api = FakeAPI()
        with self.assertRaises(policy.PolicyError):
            api.execute("kodex", "readback")
        self.assertEqual(api.writes, [])

    def test_shorter_or_unknown_client_override_prevents_write(self):
        for value in ("1800", "invalid", "-1", "86400"):
            with self.subTest(value=value):
                api = FakeAPI()
                api.client["attributes"]["client.session.max.lifespan"] = value
                with self.assertRaises(policy.PolicyError):
                    api.execute("kodex", "apply")
                self.assertEqual(api.writes, [])

    def test_shorter_realm_client_override_prevents_write(self):
        api = FakeAPI()
        api.realm["clientSessionIdleTimeout"] = 1800
        with self.assertRaises(policy.PolicyError):
            api.execute("kodex", "apply")
        self.assertEqual(api.writes, [])

    def test_projection_excludes_other_fields(self):
        realm, client = fixtures()
        realm["smtpServer"] = {"password": "synthetic-not-exported"}
        client["secret"] = "synthetic-not-exported"
        output = str(policy.safe_projection(realm, client))
        self.assertNotIn("synthetic-not-exported", output)
        self.assertNotIn("smtpServer", output)

    def test_transport_keeps_credentials_out_of_argv(self):
        api = object.__new__(policy.API)
        api.origin, api.ca_file, api.token = "https://sso.example.invalid", "/public-ca.crt", "synthetic-bearer"
        with patch.object(policy.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "{}\n200", "")) as run:
            api.request("/admin/realms/kodex", authenticated=True)
        argv = run.call_args.args[0]
        self.assertNotIn("synthetic-bearer", str(argv))
        self.assertIn("synthetic-bearer", run.call_args.kwargs["input"])
        self.assertIn("--cacert", argv)
        self.assertIn("--proto", argv)
        self.assertNotIn("--insecure", argv)
        self.assertNotIn("-k", argv)

    def test_transport_error_does_not_export_response(self):
        api = object.__new__(policy.API)
        api.origin, api.ca_file, api.token = "https://sso.example.invalid", None, "synthetic-bearer"
        with patch.object(policy.subprocess, "run", return_value=subprocess.CompletedProcess([], 60, "synthetic-secret", "synthetic-secret")):
            with self.assertRaisesRegex(policy.PolicyError, "curl exit 60") as error:
                api.request("/admin/realms/kodex", authenticated=True)
        self.assertNotIn("synthetic-secret", str(error.exception))

    def test_origin_requires_exact_https_realm_without_userinfo(self):
        for issuer in ("http://sso.invalid/realms/kodex", "https://user:password@sso.invalid/realms/kodex",
                       "https://sso.invalid/realms/other", "https://sso.invalid/realms/kodex?token=value"):
            with self.subTest(issuer=issuer), patch.object(policy.subprocess, "check_output", side_effect=["exact-context", issuer]):
                with self.assertRaises(policy.PolicyError):
                    policy.trusted_origin("exact-context", "kodex-system", "endpoints", "kodex")

    def test_context_mismatch_fails_before_read(self):
        with patch.object(policy.subprocess, "check_output", return_value="wrong-context") as read:
            with self.assertRaises(policy.PolicyError):
                policy.trusted_origin("exact-context", "kodex-system", "endpoints", "kodex")
        self.assertEqual(read.call_count, 1)


if __name__ == "__main__":
    unittest.main()

