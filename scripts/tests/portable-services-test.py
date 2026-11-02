#!/usr/bin/env python3
"""Synthetic optional Helm/prefix/credential contract, без live cluster/credentials."""
import copy
import importlib.util
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
CHART = ROOT / "infra/portable-services"


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


PREPARE = module("portable_prepare", CHART / "files/prepare.py")
PLAN = module("portable_plan", CHART / "plan.py")


def profile(mode="shared_host", tls="external"):
    value = json.loads((CHART / "values.yaml").read_text())
    value["storage"].update(enabled=True, authSecret="synthetic-storage", trustedProxyCIDRs=["192.0.2.11/32"])
    value["storage"]["ui"]["enabled"] = True
    value["registry"].update(enabled=True, authSecret="synthetic-registry")
    value["pki"]["mode"] = "isolated"
    value["exposure"].update(mode=mode, tlsMode=tls, sharedHost="app.example.invalid",
                              s3Host="s3.example.invalid", storageHost="storage.example.invalid",
                              registryHost="registry.example.invalid", tlsSecret="synthetic-public-tls",
                              trustedProxyCIDRs=["192.0.2.10/32"])
    return value


def render(value, chart=CHART):
    with tempfile.TemporaryDirectory(prefix="kodex-services-render-") as directory:
        path = Path(directory) / "values.json"
        path.write_text(json.dumps(value))
        result = subprocess.run(["helm", "template", "synthetic", str(chart), "--namespace", "synthetic-infra",
                                 "--values", str(path)], capture_output=True, text=True, timeout=30)
    return result


class PortableServices(unittest.TestCase):
    def test_default_chart_has_no_effect(self):
        result = render({})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(list(filter(None, yaml.safe_load_all(result.stdout))), [])

    def test_shared_routes_preserve_auth_and_backend_tls(self):
        result = render(profile())
        self.assertEqual(result.returncode, 0, result.stderr)
        resources = list(filter(None, yaml.safe_load_all(result.stdout)))
        indexed = {(r["kind"], r["metadata"]["name"]): r for r in resources}
        routes = [r for r in resources if r["kind"] == "IngressRoute"]
        self.assertEqual(len(routes), 3)
        for route in routes:
            self.assertEqual(route["spec"]["entryPoints"], ["web"])
            self.assertNotIn("tls", route["spec"])
            self.assertIn("Host(`app.example.invalid`)", route["spec"]["routes"][0]["match"])
            self.assertEqual(route["spec"]["routes"][0]["middlewares"][0], {"name": "synthetic-source"})
            target = route["spec"]["routes"][0]["services"][0]
            self.assertEqual(target["scheme"], "https")
            transport = indexed[("ServersTransport", target["serversTransport"])]["spec"]
            self.assertNotIn("insecureSkipVerify", transport)
            self.assertTrue(transport["rootCAsSecrets"])
        s3 = indexed[("Middleware", "synthetic-s3-origin")]["spec"]["headers"]["customRequestHeaders"]
        self.assertEqual(s3["X-Forwarded-Prefix"], "/s3")
        self.assertEqual(s3["Cookie"], "")
        registry = indexed[("IngressRoute", "synthetic-registry")]["spec"]["routes"][0]
        self.assertIn("PathPrefix(`/v2/`)", registry["match"])
        self.assertFalse(any("strip" in r["name"] for r in registry["middlewares"]))
        console = indexed[("ConfigMap", "synthetic-console")]["data"]["nginx.conf"]
        for fragment in ("proxy_ssl_verify on", "samesite=strict", "proxy_set_header Authorization \"\"",
                         "admin-session", "proxy_cookie_path ~^/$ /storage/", "if ($origin_allowed = 0)"):
            self.assertIn(fragment, console)
        registry_config = indexed[("ConfigMap", "synthetic-registry")]["data"]["nginx.conf"]
        self.assertIn("~^release-reader: 0", registry_config)
        self.assertIn("auth_basic_user_file /prepared/htpasswd", registry_config)
        self.assertIn("location /v2/kodex/", registry_config)
        for item in resources:
            if item["kind"] == "StatefulSet":
                pod = item["spec"]["template"]["spec"]
                self.assertFalse(pod["automountServiceAccountToken"])
                for container in pod["containers"]:
                    self.assertIn("@sha256:", container["image"])
                    self.assertTrue(container["securityContext"]["readOnlyRootFilesystem"])
                    self.assertFalse(container["securityContext"]["allowPrivilegeEscalation"])
                    self.assertNotIn("envFrom", container)

    def test_separate_host_managed_tls_preserves_full_s3_path(self):
        result = render(profile("separate_hosts", "managed"))
        self.assertEqual(result.returncode, 0, result.stderr)
        resources = list(filter(None, yaml.safe_load_all(result.stdout)))
        self.assertFalse(any(r["kind"] == "Middleware" and "stripPrefix" in r["spec"] for r in resources))
        routes = [r for r in resources if r["kind"] == "IngressRoute"]
        self.assertEqual(len(routes), 3)
        for route in routes:
            self.assertEqual(route["spec"]["entryPoints"], ["websecure"])
            self.assertEqual(route["spec"]["tls"], {"secretName": "synthetic-public-tls"})
        console = next(r for r in resources if r["kind"] == "ConfigMap" and r["metadata"]["name"] == "synthetic-console")
        self.assertNotIn("proxy_cookie_path", console["data"]["nginx.conf"])

    def test_colliding_paths_unsafe_trust_and_missing_auth_are_rejected(self):
        cases = []
        for key, item in (("s3Prefix", "/api"), ("s3Prefix", "/s3/../api"), ("s3Prefix", "/%73%33"),
                          ("storagePrefix", "/s3/ui"), ("storagePrefix", "/s3"),
                          ("trustedProxyCIDRs", ["0.0.0.0/0"]), ("trustedProxyCIDRs", [])):
            candidate = profile(); candidate["exposure"][key] = item; cases.append(candidate)
        candidate = profile(); candidate["storage"]["authSecret"] = ""; cases.append(candidate)
        candidate = profile(); candidate["registry"]["readerUsername"] = "release-writer"; cases.append(candidate)
        candidate = profile(); candidate["storage"]["password"] = "synthetic-forbidden"; cases.append(candidate)
        for candidate in cases:
            with self.subTest(candidate=candidate):
                self.assertNotEqual(render(candidate).returncode, 0)

    def test_plan_validates_real_cidr_and_creates_private_render_once(self):
        for invalid in ("0.0.0.0/32", "127.0.0.1/32", "192.0.2.0/24", "999.0.0.1/32", "::1/128"):
            candidate = profile(); candidate["exposure"]["trustedProxyCIDRs"] = [invalid]
            with self.subTest(cidr=invalid), self.assertRaises(ValueError):
                PLAN.validate_values(candidate)
        with tempfile.TemporaryDirectory(prefix="kodex-services-plan-") as directory:
            root = Path(directory); root.chmod(0o700)
            values = root / "values.json"; values.write_text(json.dumps(profile()))
            args = [sys.executable, str(CHART / "plan.py"), "--values", str(values), "--release", "synthetic",
                    "--namespace", "synthetic-infra", "--output", str(root / "render.yaml")]
            result = subprocess.run(args, capture_output=True, text=True, timeout=35)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout)["phase"], "PLAN_ONLY")
            self.assertEqual((root / "render.yaml").stat().st_mode & 0o777, 0o600)
            self.assertNotEqual(subprocess.run(args, capture_output=True, timeout=35).returncode, 0)

    def test_empty_anonymous_and_duplicate_storage_credentials_fail_closed(self):
        credential = {"accessKey": "SYNTHETIC_ACCESS_1234", "secretKey": "s" * 64}
        valid = {"identities": [{"name": "synthetic-user", "credentials": [credential], "actions": ["Admin"]}]}
        self.assertEqual(json.loads(PREPARE.storage_config(json.dumps(valid))), valid)
        cases = [{"identities": []}, {"identities": None}]
        for field, item in (("name", "anonymous"), ("credentials", []), ("actions", ["*"])):
            candidate = copy.deepcopy(valid); candidate["identities"][0][field] = item; cases.append(candidate)
        candidate = copy.deepcopy(valid); candidate["identities"][0]["credentials"] *= 2; cases.append(candidate)
        for candidate in cases:
            with self.subTest(candidate=candidate), self.assertRaises(ValueError):
                PREPARE.storage_config(json.dumps(candidate))
        with self.assertRaises(ValueError):
            PREPARE.storage_config('{"identities":[],"identities":[]}')

    def test_preparer_is_idempotent_and_does_not_log_or_overwrite_secret(self):
        with tempfile.TemporaryDirectory(prefix="kodex-services-credentials-") as directory:
            root = Path(directory); root.chmod(0o700)
            source = root / "input"; source.mkdir(mode=0o700)
            secret = "SYNTHETIC_SECRET_" + "x" * 48
            data = {"identities": [{"name": "synthetic-user", "credentials": [{"accessKey": "SYNTHETIC_ACCESS_1234", "secretKey": secret}], "actions": ["Admin"]}]}
            for name, value in (("s3.json", json.dumps(data)), ("ui-user", "synthetic-user"), ("ui-password", secret)):
                file = source / name; file.write_text(value); file.chmod(0o600)
            args = [sys.executable, str(CHART / "files/prepare.py"), "storage", "--input", str(source), "--output", str(root / "prepared"), "--ui"]
            for _ in range(2):
                result = subprocess.run(args, capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn(secret, result.stdout + result.stderr)
            self.assertEqual((root / "prepared/s3.json").stat().st_mode & 0o777, 0o400)
            data["identities"][0]["credentials"][0]["secretKey"] = "y" * 64
            (source / "s3.json").write_text(json.dumps(data))
            result = subprocess.run(args, capture_output=True, text=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn(secret, result.stdout + result.stderr)
            self.assertEqual(json.loads((root / "prepared/s3.json").read_text())["identities"][0]["credentials"][0]["secretKey"], secret)

    def test_backend_prefix_trust_requires_exact_safe_peers(self):
        self.assertIn("192.0.2.11/32", PREPARE.proxy_config(["192.0.2.11/32"]))
        for peers in (["0.0.0.0/0"], ["192.0.2.0/24"], ["127.0.0.1/32"],
                      ["::ffff:127.0.0.1/128"], ["192.0.2.11/32"] * 2):
            with self.subTest(peers=peers), self.assertRaises(ValueError):
                PREPARE.proxy_config(peers)
        value = profile(); value["storage"]["trustedProxyCIDRs"] = []
        self.assertNotEqual(render(value).returncode, 0)

    def test_changed_serving_config_requests_kubernetes_rollout(self):
        def templates(value):
            result = render(value)
            self.assertEqual(result.returncode, 0, result.stderr)
            return {r["metadata"]["name"]: r["spec"]["template"]
                    for r in yaml.safe_load_all(result.stdout) if r and r["kind"] == "StatefulSet"}
        before = templates(profile())
        storage = profile(); storage["exposure"]["sharedHost"] = "changed.example.invalid"
        registry = profile(); registry["registry"]["repositoryPrefix"] = "changed"
        self.assertNotEqual(before["synthetic-storage"], templates(storage)["synthetic-storage"])
        self.assertNotEqual(before["synthetic-registry"], templates(registry)["synthetic-registry"])
        self.assertEqual(before, templates(profile()))

    def test_changed_bootstrap_validation_rolls_both_owned_pods(self):
        before = {r["metadata"]["name"]: r["spec"]["template"]
                  for r in yaml.safe_load_all(render(profile()).stdout) if r and r["kind"] == "StatefulSet"}
        with tempfile.TemporaryDirectory(prefix="kodex-bootstrap-revision-") as directory:
            chart = Path(directory) / "chart"
            shutil.copytree(CHART, chart, ignore=shutil.ignore_patterns("__pycache__"))
            script = chart / "files/prepare.py"
            script.write_text(script.read_text() + "\n# Синтетическая новая ревизия bootstrap.\n")
            result = render(profile(), chart)
            self.assertEqual(result.returncode, 0, result.stderr)
            after = {r["metadata"]["name"]: r["spec"]["template"]
                     for r in yaml.safe_load_all(result.stdout) if r and r["kind"] == "StatefulSet"}
        self.assertEqual(set(before), set(after))
        for name in before:
            self.assertNotEqual(before[name], after[name])

    def test_registry_requires_exact_distinct_bcrypt_identities(self):
        digest = "$2y$12$" + "a" * 53
        raw = "release-writer:" + digest + "\nrelease-reader:" + digest.replace("a", "b") + "\n"
        actual = dict(line.split(":", 1) for line in PREPARE.registry_config(raw, "release-writer", "release-reader").splitlines())
        self.assertEqual(actual, {"release-writer": digest, "release-reader": digest.replace("a", "b")})
        for invalid in (raw + "other:" + digest + "\n", "release-writer:plaintext\n", raw.replace("$12$", "$04$")):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                PREPARE.registry_config(invalid, "release-writer", "release-reader")


if __name__ == "__main__":
    unittest.main()
