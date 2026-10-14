"""Герметичные проверки closed runner HTTPS и явно разрешённого CRI pull."""

import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

SOURCE = Path(__file__).with_name("verify-local-session-archive-publication.py")
SPEC = importlib.util.spec_from_file_location("runner_publication", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def encode(value):
    return json.dumps(value).encode()


def seal(policy):
    data = {k: v for k, v in sorted(policy["data"].items()) if k not in {"policySHA256", "orchestrationRevision"}}
    digest = hashlib.sha256((json.dumps(data, separators=(",", ":"))+"\n").encode()).hexdigest()
    policy["data"]["policySHA256"] = digest
    policy["metadata"]["name"] = "kodex-image-admission-policy-"+digest[:32]
    return policy


class RunnerTests(unittest.TestCase):
    def setUp(self):
        MODULE.cri_attempted = False
        self.blobs, self.events = {}, []
        def blob(value, media):
            raw = encode(value) if not isinstance(value, bytes) else value
            digest = "sha256:"+hashlib.sha256(raw).hexdigest()
            self.blobs[digest] = raw
            return {"mediaType": media, "digest": digest, "size": len(raw)}
        self.config = blob({"os": "linux", "architecture": "amd64", "config": {
            "Entrypoint": MODULE.RUNNER_ENTRYPOINT}}, MODULE.CONFIG)["digest"]
        self.manifest = blob({"schemaVersion": 2, "mediaType": MODULE.MANIFEST,
            "config": {"mediaType": MODULE.CONFIG, "digest": self.config, "size": len(self.blobs[self.config])},
            "layers": [blob(b"bounded actual runner fixture bytes", next(iter(MODULE.LAYERS)))]}, MODULE.MANIFEST)["digest"]
        self.host = "pull.kodex.127.0.0.2.nip.io"
        self.pin = self.host+"/kodex/agent-runner@"+self.manifest
        self.policy = seal({"apiVersion": "v1", "kind": "ConfigMap", "immutable": True,
            "metadata": {"namespace": "kodex-system", "uid": "12345678-1234-1234-1234-123456789012", "resourceVersion": "8", "labels": {
                "app.kubernetes.io/part-of": "kodex", "kodex.dev/owner-intent": "true"}},
            "data": {"pullRegistryHost": self.host, "pullCredentialGeneration": "1",
                "promotedPullRepository": self.host+"/kodex/roles", "trustedRoleBaseDigest": self.manifest,
                "nodeReadbackImage": self.pin, "policyRevision": "8"}})
        self.registry = {"mirrors": {self.host: {"endpoint": ["https://"+self.host]}},
            "configs": {self.host: {"tls": MODULE.TLS_PATHS,
                "auth": {"username": "node", "password": "PRIVATE_SENTINEL"}}}}
        self.nodes = ["k3d-kodex-agent-0", "k3d-kodex-server-0"]

    def tearDown(self):
        MODULE.enter_stage("UNKNOWN")
        MODULE.cri_attempted = False

    def capture(self, args, deadline, data=None, maximum_seconds=20):
        self.events.append(tuple(args))
        self.assertNotIn("SENTINEL", " ".join(args))
        if args[0] == "yq":
            return data
        if args[0] == "kubectl":
            if "deployment/image-admission-controller" in args:
                return encode({"kind": "Deployment", "metadata": {"namespace": "kodex-system", "name": "image-admission-controller"},
                    "spec": {"template": {"metadata": {"labels": {"kodex.dev/local-profile": "hot-reload"}}, "spec": {
                        "containers": [{"name": "image-admission-controller", "env": [{
                            "name": "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP", "value": self.policy["metadata"]["name"]}]}]}}}})
            return encode(self.policy)
        if args[0] == "k3d":
            return encode([{"name": n, "role": "server" if "-server-" in n else "agent"} for n in self.nodes])
        if args[:3] == ["docker", "image", "inspect"]:
            return ("sha256:"+"a"*64).encode()
        if args[:3] == ["docker", "inspect", "--format"]:
            return b"172.18.0.2"
        if args[:2] == ["docker", "inspect"]:
            return encode([{"Id": ("b" if "-server-" in args[2] else "a")*64, "State": {"Running": True}, "Config": {"Labels": {
                "k3d.cluster": "kodex", "k3d.role": "server" if "-server-" in args[2] else "agent"}}}])
        if args[:2] == ["docker", "exec"]:
            if args[3] == "/bin/crictl" and args[-1] == "--version":
                return b"crictl github.com/k3s-io/containerd/v2 v1.35.0-k3s2\n"
            if args[3] == "/bin/crictl" and args[-1] == "--help":
                if args[4:] == ["--help"]:
                    return b"NAME:\n   crictl - client for CRI\nOPTIONS:\n   --runtime-endpoint value\n   --image-endpoint value\n"
                if args[4] == "pull":
                    return b"NAME:\n   crictl pull - Pull an image\nOPTIONS:\n   --pull-timeout value\n"
                if args[4] == "inspecti":
                    return b"NAME:\n   crictl inspecti - Inspect image\nOPTIONS:\n   --output value\n"
            if args[3] == "stat":
                return b"regular file" if args[-1] == "/etc/hosts" else b"regular file:0:600"
            if args[-1] == "/etc/hosts":
                return ("172.18.0.2 "+self.host+"\n").encode()
            if args[-1] == "/etc/rancher/k3s/registries.yaml":
                return encode(self.registry)
            if args[3] == "cat":
                return b"PRIVATE_TLS_SENTINEL"
            if "pull" in args:
                self.assertEqual(args[-1], self.pin)
                self.assertIn(args[2], {"a"*64, "b"*64})
                self.assertEqual(args[3], "/bin/crictl")
                self.assertNotIn("k3s", args)
                self.assertIn("--image-endpoint=unix:///run/k3s/containerd/containerd.sock", args)
                self.assertIn("--runtime-endpoint=unix:///run/k3s/containerd/containerd.sock", args)
                self.assertTrue(any(a.startswith("--pull-timeout=") for a in args))
                return b"PRIVATE_COMMAND_OUTPUT_SENTINEL"
            if "inspecti" in args:
                self.assertEqual(args[-1], self.pin)
                return encode({"status": {"id": self.config, "repoDigests": [self.pin], "size": "12345"}})
        self.fail("unexpected fixture command")

    def run_main(self, cri=False, interceptor=None, graph_failure=False):
        with tempfile.TemporaryDirectory() as root:
            state = Path(root)
            (state/"agent-runner-image").write_text("registry.local.kodex/kodex/agent-runner@"+self.manifest)
            (state/"image-supply-chain-tools-docker-tag").write_text("kodex-local/image-admission-tools:"+"a"*64)
            args = [str(SOURCE), "--component", "runner", "--context", "k3d-kodex", "--state-directory", root, "--timeout", "30"]
            if cri:
                args.extend(["--cri-pull", "--confirm", "PULL-EXACT-LOCAL-RUNNER"])
            def read(reader, kind, digest, size=None):
                self.events.append(("HTTPS", reader.node, kind, digest))
                self.assertEqual(reader.repository, "agent-runner")
                if graph_failure and reader.node == self.nodes[-1]:
                    raise MODULE.Failure("HTTPS_READ_FAILED")
                raw = self.blobs[digest]
                MODULE.require(hashlib.sha256(raw).hexdigest() == digest[7:] and
                    (size is None or size == len(raw)), "DIGEST_MISMATCH")
                return raw
            def capture(*args, **kwargs):
                return interceptor(self.capture, *args, **kwargs) if interceptor else self.capture(*args, **kwargs)
            output = io.StringIO()
            with patch.object(sys, "argv", args), patch.dict(os.environ, {"KUBECONFIG": "/home/s/.kube/config"}), \
                    patch.object(MODULE, "capture", side_effect=capture), patch.object(MODULE.NodeReader, "read", new=read), \
                    patch("sys.stdout", output):
                MODULE.main()
            self.assertNotIn("SENTINEL", output.getvalue())
            return [json.loads(line) for line in output.getvalue().splitlines()]

    def test_actual_policy_target_not_alias_and_sealed_hash(self):
        self.assertEqual(MODULE.runner_policy_pin(self.policy), (self.pin, self.host, self.manifest))
        for mutate in (lambda p: p["data"].update(nodeReadbackImage="pull.kodex.works/kodex/agent-runner@"+self.manifest),
                       lambda p: p["data"].update(pullRegistryHost="https://"+self.host),
                       lambda p: p["data"].update(promotedPullRepository=self.host+"/foreign"),
                       lambda p: p["data"].update(pullCredentialGeneration="0"),
                       lambda p: p["metadata"].update(uid="unknown"),
                       lambda p: p["metadata"].update(namespace="foreign-owner"),
                       lambda p: p.update(immutable=False)):
            policy = copy.deepcopy(self.policy)
            mutate(policy)
            seal(policy)
            with self.assertRaises(MODULE.Failure):
                MODULE.runner_policy_pin(policy)
        self.policy["data"]["policySHA256"] = "0"*64
        with self.assertRaises(MODULE.Failure):
            MODULE.runner_policy_pin(self.policy)

    def test_default_https_profile_never_pulls_or_starts_pods(self):
        proof = self.run_main()
        self.assertEqual(len(proof), 2)
        self.assertTrue(all(p["evidence"] == "NODE_HTTPS_GRAPH" and p["criPull"] == "NOT_CHECKED" for p in proof))
        self.assertFalse(any("pull" in e or "inspecti" in e or "apply" in e or "patch" in e or "/bin/crictl" in e for e in self.events))
        self.assertFalse(MODULE.cri_attempted)

    def test_runner_url_and_tls_sni_remain_exact_without_secrets_in_argv(self):
        with tempfile.TemporaryDirectory() as root:
            reader = MODULE.NodeReader(self.nodes[0], "sha256:"+"a"*64, Path(root), self.host,
                "172.18.0.2", {"username": "node", "password": "PRIVATE_SENTINEL"}, time.monotonic()+5, "runner")
            config = Path(root, "curl.conf").read_text()
            self.assertIn('resolve = "'+self.host+':443:172.18.0.2"', config)
            self.assertIn('proto = "=https"', config)
            self.assertNotIn("insecure", config)
            calls = []
            original = subprocess.Popen
            raw = self.blobs[self.config]
            def factory(argv, **kwargs):
                calls.append(argv)
                return original([sys.executable, "-c", "import sys;sys.stdout.buffer.write("+repr(raw)+")"], **kwargs)
            with patch.object(MODULE.subprocess, "Popen", side_effect=factory):
                self.assertEqual(reader.read("blobs", self.config, len(raw)), raw)
            self.assertEqual(calls[0][-1], "https://"+self.host+"/v2/kodex/agent-runner/blobs/"+self.config)
            self.assertNotIn("SENTINEL", " ".join(calls[0]))
            self.assertNotIn("--insecure", calls[0])
            self.assertEqual(Path(root, "curl.conf").stat().st_mode & 0o777, 0o600)

    def test_all_https_graphs_before_opt_in_pull_and_exact_inspect(self):
        proof = self.run_main(cri=True)
        self.assertEqual([p["evidence"] for p in proof], ["NODE_HTTPS_GRAPH"]*2+["NODE_CRI_PULL"]*4)
        self.assertEqual(sum(p["status"] == "INTENT" for p in proof), 2)
        pulls = [i for i, e in enumerate(self.events) if "pull" in e and "--help" not in e]
        https = [i for i, e in enumerate(self.events) if e[0] == "HTTPS"]
        self.assertLess(max(https), min(pulls))
        self.assertTrue(all(p.get("runtimeELF") == "NOT_CHECKED" and p["cacheWrites"] is True for p in proof if p["evidence"] == "NODE_CRI_PULL" and p["status"] == "PASS"))
        self.assertFalse(any(e[0] == "docker" and any(a in e for a in ("rm", "start", "restart", "import")) for e in self.events))

    def test_unavailable_second_node_graph_never_begins_cri(self):
        with self.assertRaises(MODULE.Failure):
            self.run_main(cri=True, graph_failure=True)
        self.assertFalse(MODULE.cri_attempted)
        self.assertFalse(any("pull" in e for e in self.events))

    def test_source_digest_and_runner_entrypoint_mismatch_fail_closed(self):
        original_pin = self.policy["data"]["nodeReadbackImage"]
        self.policy["data"]["trustedRoleBaseDigest"] = "sha256:"+"c"*64
        self.policy["data"]["nodeReadbackImage"] = self.host+"/kodex/agent-runner@sha256:"+"c"*64
        seal(self.policy)
        with self.assertRaises(MODULE.Failure) as error:
            self.run_main(cri=True)
        self.assertEqual(str(error.exception), "PIN_MISMATCH")
        self.assertFalse(MODULE.cri_attempted)
        self.policy["data"]["trustedRoleBaseDigest"] = self.manifest
        self.policy["data"]["nodeReadbackImage"] = original_pin
        seal(self.policy)
        data = json.loads(self.blobs[self.config])
        data["config"]["Entrypoint"] = ["/usr/local/bin/session-archive"]
        raw = encode(data)
        changed_config = "sha256:"+hashlib.sha256(raw).hexdigest()
        self.blobs[changed_config] = raw
        manifest = json.loads(self.blobs[self.manifest])
        manifest["config"].update(digest=changed_config, size=len(raw))
        raw = encode(manifest)
        changed_manifest = "sha256:"+hashlib.sha256(raw).hexdigest()
        self.blobs[changed_manifest] = raw
        class Reader:
            def read(_self, kind, digest, size=None): return self.blobs[digest]
        with self.assertRaises(MODULE.Failure) as error:
            MODULE.verify_graph(Reader(), changed_manifest, "runner")
        self.assertEqual(str(error.exception), "PLATFORM_INVALID")

    def test_changed_policy_and_unknown_config_fail_before_cache_effect(self):
        for change in ("policy", "config"):
            self.events = []
            def intercept(original, args, *rest, **kwargs):
                raw = original(args, *rest, **kwargs)
                graphs = sum(e[0] == "HTTPS" for e in self.events)
                if graphs == 6 and change == "policy" and args[0] == "kubectl" and any(a.startswith("configmap/") for a in args):
                    policy = copy.deepcopy(self.policy)
                    policy["metadata"]["resourceVersion"] = "changed"
                    return encode(policy)
                if graphs == 6 and change == "config" and args[-1] == "/etc/rancher/k3s/registries.yaml":
                    return b"{}"
                return raw
            with self.assertRaises(MODULE.Failure):
                self.run_main(cri=True, interceptor=intercept)
            self.assertFalse(MODULE.cri_attempted)

    def test_pull_failure_or_wrong_repo_id_does_not_become_pass(self):
        for invalid in ("pull", "id", "repo", "size"):
            self.events = []
            MODULE.cri_attempted = False
            def intercept(original, args, *rest, **kwargs):
                if "pull" in args and "--help" not in args and invalid == "pull":
                    raise MODULE.Failure("COMMAND_TIMEOUT")
                raw = original(args, *rest, **kwargs)
                if "inspecti" in args and "--help" not in args:
                    value = json.loads(raw)
                    if invalid == "id": value["status"]["id"] = "sha256:"+"0"*64
                    if invalid == "repo": value["status"]["repoDigests"] = ["pull.foreign.invalid/kodex/agent-runner@"+self.manifest]
                    if invalid == "size": value["status"]["size"] = True
                    return encode(value)
                return raw
            with self.assertRaises(MODULE.Failure):
                self.run_main(cri=True, interceptor=intercept)
            self.assertTrue(MODULE.cri_attempted)

    def test_missing_or_unsupported_second_node_client_stops_before_any_intent(self):
        for invalid in ("missing", "version", "global", "pull", "inspecti"):
            self.events = []
            output = io.StringIO()
            def intercept(original, args, *rest, **kwargs):
                if args[:4] == ["docker", "exec", "b"*64, "/bin/crictl"]:
                    if invalid == "missing":
                        raise MODULE.Failure("COMMAND_FAILED")
                    if args[-1] == "--version" and invalid == "version":
                        return b"k3s PRIVATE_SENTINEL v2.0.0\n"
                    if args[-1] == "--help" and ((invalid == "global" and args[4:] == ["--help"]) or
                            invalid in args[4:-1]):
                        return b"NAME:\n   unsupported PRIVATE_SENTINEL\n"
                return original(args, *rest, **kwargs)
            with self.assertRaises(MODULE.Failure) as error, patch("sys.stdout", output):
                self.run_main(cri=True, interceptor=intercept)
            self.assertEqual(error.exception.stage, "CRI_CLIENT")
            self.assertNotIn("SENTINEL", str(error.exception))
            self.assertNotIn("INTENT", output.getvalue())
            self.assertFalse(MODULE.cri_attempted)
            self.assertFalse(any("pull" in e and "--help" not in e for e in self.events))

    def test_client_readonly_probe_all_nodes_before_pull_and_version_drift_denies(self):
        self.run_main(cri=True)
        first_pull = next(i for i, e in enumerate(self.events) if "pull" in e and "--help" not in e)
        for container in ("a"*64, "b"*64):
            for suffix in (("--version",), ("--help",), ("pull", "--help"), ("inspecti", "--help")):
                self.assertIn(("docker", "exec", container, "/bin/crictl", *suffix), self.events[:first_pull])
        self.events = []
        MODULE.cri_attempted = False
        version_probes = 0
        def intercept(original, args, *rest, **kwargs):
            nonlocal version_probes
            if args == ["docker", "exec", "a"*64, "/bin/crictl", "--version"]:
                version_probes += 1
                if version_probes == 2:
                    return b"crictl github.com/k3s-io/containerd/v2 v1.36.0-k3s2\n"
            return original(args, *rest, **kwargs)
        with self.assertRaises(MODULE.Failure) as error:
            self.run_main(cri=True, interceptor=intercept)
        self.assertEqual(str(error.exception), "CRI_CLIENT_CHANGED")
        self.assertFalse(MODULE.cri_attempted)

    def test_confirmation_closed_before_external_commands(self):
        for args in (["--component", "runner", "--cri-pull"], ["--component", "runner", "--confirm", "PULL-EXACT-LOCAL-RUNNER"],
                     ["--cri-pull", "--confirm", "PULL-EXACT-LOCAL-RUNNER"], ["--component", "runner", "--cri-pull", "--confirm", "YES"]):
            result = subprocess.run([sys.executable, str(SOURCE), "--context", "k3d-kodex", "--state-directory", "/nonexistent", *args],
                capture_output=True, text=True, timeout=3, env={**os.environ, "PATH": "/nonexistent"})
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(json.loads(result.stdout)["code"], "CRI_CONFIRMATION_INVALID")


if __name__ == "__main__":
    unittest.main()
