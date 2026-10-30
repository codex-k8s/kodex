"""Отрицательная матрица и сохранение security boundary для опционального render."""
import copy
import json
from pathlib import Path
import subprocess
import unittest

import yaml
from deployment_profile import Rejected, transform, validate, verify_proxy_controller, read_render_profile

ROOT = Path(__file__).resolve().parents[2]
BUNDLED = {"version": 1, "identity": {"mode": "bundled"}, "public_tls": {"mode": "managed"}}
EXTERNAL = {"version": 1, "identity": {
    "mode": "external", "issuer": "https://id.example.invalid/auth/realms/test",
    "jwks_url": "https://id.example.invalid/auth/realms/test/protocol/openid-connect/certs",
    "connect_host": "id.example.invalid", "endpoint_cidrs": ["192.0.2.17/32"],
    "authorization_url": "https://id.example.invalid/authorize", "token_url": "https://id.example.invalid/token",
    "client_id": "test-console", "audience": "kodex-control-api", "scope": "openid profile"},
    "public_tls": {"mode": "external", "trusted_proxy_cidrs": ["192.0.2.18/32", "2001:db8::18/128"]}}


class ProfileTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.renders = {}
        for profile in ("web-only", "web-with-mattermost"):
            result = subprocess.run(["kubectl", "kustomize", str(ROOT / "deploy/k8s/profiles" / profile)],
                                    capture_output=True, text=True, timeout=60, check=True)
            raw = result.stdout.replace("__KODEX_PUBLIC_HOST__", "app.example.invalid")
            cls.renders[profile] = list(filter(None, yaml.safe_load_all(raw)))

    def test_bundled_is_unchanged_in_both_profiles(self):
        for resources in self.renders.values():
            before = copy.deepcopy(resources)
            self.assertEqual(transform(resources, BUNDLED, "app.example.invalid")[:-1], before)
            self.assertEqual(resources, before)

    def test_modes_are_independent(self):
        for identity in (BUNDLED["identity"], EXTERNAL["identity"]):
            for tls in (BUNDLED["public_tls"], EXTERNAL["public_tls"]):
                self.assertEqual(validate({"version": 1, "identity": identity, "public_tls": tls})["identity"], identity)

    def test_external_preserves_internal_tls_and_other_resources(self):
        for resources in self.renders.values():
            before = copy.deepcopy(resources)
            rendered = transform(resources, EXTERNAL, "app.example.invalid")
            self.assertEqual(resources, before)
            by_name = {(r["kind"], r["metadata"]["name"]): r for r in rendered if r["metadata"].get("namespace") in (None, "kodex-system")}
            self.assertNotIn(("Certificate", "staff-control-center-public"), by_name)
            touched = {("Ingress", n) for n in ("staff-control-center", "staff-control-center-api", "staff-control-center-public-assets")}
            touched |= {("NetworkPolicy", n) for n in ("control-plane-exact-runtime-paths", "control-api-gateway-exact-runtime-paths")}
            touched |= {("Deployment", "staff-control-center"), ("Deployment", "control-api-gateway"), ("Certificate", "staff-control-center-public")}
            for resource in resources:
                key = (resource["kind"], resource["metadata"]["name"])
                if resource["metadata"].get("namespace") not in (None, "kodex-system"):
                    self.assertIn(resource, rendered)
                    continue
                if key not in touched and not (key[0] == "ConfigMap" and key[1].startswith("staff-control-center-runtime")):
                    self.assertEqual(by_name[key], resource)
            for name in ("control-plane-exact-runtime-paths", "control-api-gateway-exact-runtime-paths"):
                edges = by_name[("NetworkPolicy", name)]["spec"]["egress"]
                self.assertIn({"to": [{"ipBlock": {"cidr": "192.0.2.17/32"}}], "ports": [{"protocol": "TCP", "port": 443}]}, edges)
            allow = by_name[("Middleware", "external-proxy-source")]["spec"]
            self.assertEqual(allow, {"ipAllowList": {"sourceRange": EXTERNAL["public_tls"]["trusted_proxy_cidrs"]}})
            for kind, name in touched:
                if kind != "Ingress": continue
                route = by_name[(kind, name)]
                self.assertNotIn("tls", route["spec"])
                annotations = route["metadata"]["annotations"]
                self.assertEqual(annotations["traefik.ingress.kubernetes.io/router.entrypoints"], "web")
                self.assertNotIn("oauth2", annotations["traefik.ingress.kubernetes.io/router.middlewares"])
            cm = next(r for r in rendered if r["kind"] == "ConfigMap" and r["metadata"]["name"].startswith("staff-control-center-runtime-profile"))
            runtime = json.loads(cm["data"]["runtime-config.json"])
            self.assertEqual(runtime["oidc"]["clientId"], "test-console")
            self.assertEqual(json.loads(cm["data"]["readiness.json"])["runtimeConfigRevision"], runtime["revision"])
            self.assertEqual(transform(resources, EXTERNAL, "app.example.invalid"), rendered)

    def test_invalid_or_ambiguous_profiles_fail_closed(self):
        cases = []
        for key, value in [("issuer", "http://id.example.invalid"), ("issuer", "https://user:fixture@id.example.invalid/realm"),
                           ("issuer", "https://id.example.invalid/realm?x"), ("issuer", "https://id.example.invalid/a/../b"),
                           ("jwks_url", "https://elsewhere.example.invalid/jwks"), ("client_id", "x\nsecret"),
                           ("audience", "other"), ("scope", "profile"), ("endpoint_cidrs", ["0.0.0.0/0"]),
                           ("endpoint_cidrs", ["192.0.2.0/24"]), ("endpoint_cidrs", ["127.0.0.1/32"]),
                           ("connect_host", "192.0.2.17")]:
            candidate = copy.deepcopy(EXTERNAL); candidate["identity"][key] = value; cases.append(candidate)
        for cidrs in ([], ["::/0"], ["192.0.2.18/32"] * 2, ["::1/128"]):
            candidate = copy.deepcopy(EXTERNAL); candidate["public_tls"]["trusted_proxy_cidrs"] = cidrs; cases.append(candidate)
        candidate = copy.deepcopy(BUNDLED); candidate["identity"]["admin_password"] = "fixture"; cases.append(candidate)
        candidate = copy.deepcopy(BUNDLED); candidate["public_tls"]["mode"] = "disabled"; cases.append(candidate)
        for candidate in cases:
            with self.subTest(candidate=candidate), self.assertRaises(Rejected): validate(candidate)

    def test_proxy_controller_must_match_exact_static_trust(self):
        deployment = {"kind": "Deployment", "metadata": {"generation": 2},
            "status": {"observedGeneration": 2, "availableReplicas": 1},
            "spec": {"template": {"spec": {"containers": [{"name": "traefik", "args": [
                "--entryPoints.web.address=:80",
                "--entryPoints.web.forwardedHeaders.trustedIPs=192.0.2.18/32,2001:db8::18/128"]}]}}}}
        verify_proxy_controller(deployment, EXTERNAL)
        for flag in ("--entryPoints.web.forwardedHeaders.insecure=true", "--configFile=/tmp/fixture",
                     "--entryPoints.web.http.redirections.entryPoint.to=websecure",
                     "--entryPoints.web.proxyProtocol.insecure=true"):
            candidate = copy.deepcopy(deployment)
            candidate["spec"]["template"]["spec"]["containers"][0]["args"].append(flag)
            with self.assertRaises(Rejected): verify_proxy_controller(candidate, EXTERNAL)
        candidate = copy.deepcopy(deployment); candidate["status"]["availableReplicas"] = 0
        with self.assertRaises(Rejected): verify_proxy_controller(candidate, EXTERNAL)

    def test_profile_is_sealed_in_render(self):
        result = transform(self.renders["web-only"], EXTERNAL, "app.example.invalid")
        self.assertEqual(read_render_profile(result), EXTERNAL)
        result[-1]["data"]["profile.sha256"] = "a" * 64
        with self.assertRaises(Rejected): read_render_profile(result)

    def test_missing_edges_routes_and_host_are_rejected(self):
        resources = self.renders["web-only"]
        for kind, name in [("NetworkPolicy", "control-plane-exact-runtime-paths"), ("Ingress", "staff-control-center-api")]:
            candidate = [r for r in resources if (r["kind"], r["metadata"]["name"]) != (kind, name)]
            with self.assertRaises(Rejected): transform(candidate, EXTERNAL, "app.example.invalid")
        with self.assertRaises(Rejected): transform(resources, EXTERNAL, "foreign.example.invalid")


if __name__ == "__main__":
    unittest.main()
