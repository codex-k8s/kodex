"""Герметичные проверки: registry bytes не подменяются node image cache."""

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
SPEC = importlib.util.spec_from_file_location("archive_publication", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def encode(value):
    return json.dumps(value).encode()


class FixtureReader:
    def __init__(self):
        self.data, self.calls = {}, []

    def blob(self, raw, media):
        digest = "sha256:"+hashlib.sha256(raw).hexdigest()
        self.data[digest] = raw
        return {"mediaType": media, "digest": digest, "size": len(raw)}

    def manifest(self):
        config = self.blob(encode({"os": "linux", "architecture": "amd64", "config": {
            "Entrypoint": ["/usr/local/bin/session-archive"]}}), MODULE.CONFIG)
        layer = self.blob(b"actual fixture image bytes", next(iter(MODULE.LAYERS)))
        manifest = self.blob(encode({"schemaVersion": 2, "mediaType": MODULE.MANIFEST,
                                    "config": config, "layers": [layer]}), MODULE.MANIFEST)
        return manifest

    def read(self, kind, digest, size=None):
        self.calls.append((kind, digest))
        raw = self.data[digest]
        MODULE.require(hashlib.sha256(raw).hexdigest() == digest[7:] and
                       (size is None or len(raw) == size), "DIGEST_MISMATCH")
        return raw


class PublicationTests(unittest.TestCase):
    def test_manifest_and_all_image_blobs_without_node_cache(self):
        reader = FixtureReader()
        manifest = reader.manifest()
        self.assertEqual(MODULE.verify_graph(reader, manifest["digest"]), 1)
        self.assertEqual(len(reader.calls), 3)
        index = reader.blob(encode({"schemaVersion": 2, "mediaType": MODULE.INDEX,
            "manifests": [{**manifest, "platform": {"os": "linux", "architecture": "amd64"}}]}), MODULE.INDEX)
        self.assertEqual(MODULE.verify_graph(reader, index["digest"]), 2)

    def test_corruption_wrong_size_foreign_platform_and_external_urls_fail(self):
        for invalid in ("corrupt", "size", "platform", "url"):
            reader = FixtureReader()
            manifest = reader.manifest()
            value = json.loads(reader.data[manifest["digest"]])
            if invalid == "corrupt":
                reader.data[value["layers"][0]["digest"]] = b"SENTINEL_PRIVATE_BODY"
            elif invalid == "size":
                value["layers"][0]["size"] += 1
            elif invalid == "url":
                value["layers"][0]["urls"] = ["http://private.invalid/SENTINEL"]
            elif invalid == "platform":
                config = reader.blob(encode({"os": "linux", "architecture": "arm64"}), MODULE.CONFIG)
                value["config"] = config
            if invalid != "corrupt":
                manifest = reader.blob(encode(value), MODULE.MANIFEST)
            with self.assertRaises(MODULE.Failure) as failure:
                MODULE.verify_graph(reader, manifest["digest"])
            self.assertNotIn("SENTINEL", str(failure.exception))

    def test_unknown_graph_duplicates_and_bounds_fail_closed(self):
        for value in ({"schemaVersion": 2, "mediaType": "arbitrary"},
                      {"schemaVersion": 2, "mediaType": MODULE.INDEX, "manifests": []}):
            reader = FixtureReader()
            digest = reader.blob(encode(value), MODULE.MANIFEST)["digest"]
            with self.assertRaises(MODULE.Failure):
                MODULE.verify_graph(reader, digest)
        with self.assertRaises(MODULE.Failure):
            MODULE.document(b'{"password":"SENTINEL","password":"other"}')
        reader = FixtureReader()
        manifest = reader.manifest()
        with patch.object(MODULE, "MAX_TOTAL", 1), self.assertRaises(MODULE.Failure):
            MODULE.verify_graph(reader, manifest["digest"])

    def test_existing_identity_route_closed_and_secrets_never_in_argv(self):
        config = {"mirrors": {"pull.fixture.invalid": {"endpoint": ["https://pull.fixture.invalid"]}},
                  "configs": {"pull.fixture.invalid": {"tls": MODULE.TLS_PATHS,
                    "auth": {"username": "node", "password": "SENTINEL_PRIVATE_CREDENTIAL"}}}}
        auth = MODULE.credentials(config, "pull.fixture.invalid")
        for bad in ({**MODULE.TLS_PATHS, "insecure_skip_verify": True},
                    {**MODULE.TLS_PATHS, "key_file": "/private/foreign"}):
            config["configs"]["pull.fixture.invalid"]["tls"] = bad
            with self.assertRaises(MODULE.Failure):
                MODULE.credentials(config, "pull.fixture.invalid")
        with tempfile.TemporaryDirectory() as root:
            reader = MODULE.NodeReader("k3d-kodex-agent-0", "sha256:"+"a"*64,
                Path(root), "pull.fixture.invalid", "172.18.0.2", auth, time.monotonic()+5)
            payload = b"actual layer bytes"
            digest = "sha256:"+hashlib.sha256(payload).hexdigest()
            original = subprocess.Popen
            calls = []

            def factory(argv, **kwargs):
                calls.append(argv)
                return original([sys.executable, "-c", "import sys;sys.stdout.buffer.write("+repr(payload)+")"], **kwargs)

            with patch.object(MODULE.subprocess, "Popen", side_effect=factory):
                self.assertEqual(reader.read("blobs", digest, len(payload)), payload)
            argv = calls[0]
            self.assertIn("--network=container:k3d-kodex-agent-0", argv)
            self.assertIn("--pull=never", argv)
            self.assertIn("--read-only", argv)
            self.assertNotIn("SENTINEL", " ".join(argv))
            self.assertNotIn("--insecure", argv)
            self.assertEqual(Path(root, "curl.conf").stat().st_mode & 0o777, 0o600)

    def test_stream_digest_failure_private_output_redacted(self):
        with tempfile.TemporaryDirectory() as root:
            reader = MODULE.NodeReader("k3d-kodex-server-0", "sha256:"+"a"*64,
                Path(root), "pull.fixture.invalid", "172.18.0.2",
                {"username": "node", "password": "SECRET_SENTINEL"}, time.monotonic()+5)
            original = subprocess.Popen

            def factory(_argv, **kwargs):
                return original([sys.executable, "-c", "import sys;print('PRIVATE_SENTINEL');print('ERROR_SENTINEL',file=sys.stderr)"], **kwargs)

            with patch.object(MODULE.subprocess, "Popen", side_effect=factory), self.assertRaises(MODULE.Failure) as error:
                reader.read("blobs", "sha256:"+"b"*64, 1024)
            self.assertNotIn("SENTINEL", str(error.exception))

    def test_render_requires_one_bound_public_worker_pin(self):
        fixture = {"kind": "Deployment", "metadata": {"name": "session-archive", "namespace": "kodex-system"},
            "spec": {"template": {"metadata": {"labels": {"kodex.dev/local-profile": "hot-reload"}},
                "spec": {"containers": [{"name": "session-archive", "env": [{"name": "SESSION_ARCHIVE_WORKER_IMAGE",
                    "value": "pull.fixture.invalid/kodex/session-archive@sha256:"+"a"*64}]}]}}}}
        self.assertIn("pull.fixture.invalid/", MODULE.deployment_pin(fixture))
        fixture["spec"]["template"]["spec"]["containers"][0]["env"] *= 2
        with self.assertRaises(MODULE.Failure):
            MODULE.deployment_pin(fixture)

    def test_full_cli_two_nodes_reads_registry_bytes_without_cache_or_private_output(self):
        reader = FixtureReader()
        descriptor = reader.manifest()
        digest = descriptor["digest"]
        pin = "pull.fixture.invalid/kodex/session-archive@"+digest
        deployment = {"kind": "Deployment", "metadata": {"name": "session-archive", "namespace": "kodex-system"},
            "spec": {"template": {"metadata": {"labels": {"kodex.dev/local-profile": "hot-reload"}},
                "spec": {"containers": [{"name": "session-archive", "env": [{"name": "SESSION_ARCHIVE_WORKER_IMAGE", "value": pin}]}]}}}}
        config = {"mirrors": {"pull.fixture.invalid": {"endpoint": ["https://pull.fixture.invalid"]}},
            "configs": {"pull.fixture.invalid": {"tls": MODULE.TLS_PATHS,
                "auth": {"username": "node", "password": "PRIVATE_SENTINEL"}}}}
        with tempfile.TemporaryDirectory() as root:
            directory = Path(root)
            state, binary = directory/"state", directory/"bin"
            state.mkdir()
            binary.mkdir()
            (state/"session-archive-image").write_text("registry.local.kodex/kodex/session-archive@"+digest)
            (state/"image-supply-chain-tools-docker-tag").write_text("kodex-local/image-admission-tools:"+"a"*64)
            render = directory/"render.json"
            render.write_bytes(encode(deployment))
            (directory/"config.json").write_bytes(encode(config))
            (directory/"blobs.json").write_bytes(encode({key: value.hex() for key, value in reader.data.items()}))
            commands = {
                "kubectl": "print((root/'render.json').read_text())",
                "k3d": "print(json.dumps([{'name':'k3d-kodex-server-0','role':'server'},{'name':'k3d-kodex-agent-0','role':'agent'}]))",
                "docker": '''
with (root/'argv.jsonl').open('a') as output: output.write(json.dumps(args)+'\\n')
if args[:2] == ['image','inspect']: print('sha256:'+'a'*64)
elif args[0] == 'inspect': print(json.dumps([{'State':{'Running':True},'Config':{'Labels':{'k3d.cluster':'kodex','k3d.role':'server' if 'server' in args[1] else 'agent'}}}]))
elif args[0] == 'exec':
    if args[2] == 'stat': print('regular file:0:600')
    elif args[2] == 'getent': print('172.18.0.2 pull.fixture.invalid')
    elif args[-1] == '/etc/rancher/k3s/registries.yaml': print((root/'config.json').read_text())
    elif args[2] == 'cat': print('PRIVATE_CERT_SENTINEL')
    else: sys.exit(5)
elif args[0] == 'run':
    assert '--read-only' in args and '--pull=never' in args and '--cap-drop=ALL' in args
    assert any(value.startswith('--network=container:k3d-kodex-') for value in args)
    mount=args[args.index('--mount')+1]; source=mount.split('src=')[1].split(',dst=')[0]
    cfg=(Path(source)/'curl.conf').read_text()
    assert 'PRIVATE_SENTINEL' in cfg and 'PRIVATE_CERT_SENTINEL' in (Path(source)/'key_file').read_text()
    assert 'proto = "=https"' in cfg and 'resolve = "pull.fixture.invalid:443:172.18.0.2"' in cfg
    digest=args[-1].rsplit('/',1)[1]
    sys.stdout.buffer.write(bytes.fromhex(json.loads((root/'blobs.json').read_text())[digest]))
else: sys.exit(6)
''',
            }
            for name, body in commands.items():
                executable = binary/name
                executable.write_text("#!/usr/bin/python3\nimport json,sys,os\nfrom pathlib import Path\nroot=Path(os.environ['FIXTURE_ROOT']);args=sys.argv[1:]\n"+body+"\n")
                executable.chmod(0o700)
            result = subprocess.run([sys.executable, str(SOURCE), "--context", "k3d-kodex",
                "--state-directory", str(state), "--render", str(render), "--timeout", "10"],
                capture_output=True, text=True, timeout=15,
                env={**os.environ, "PATH": str(binary)+os.pathsep+os.environ["PATH"],
                     "FIXTURE_ROOT": root, "KUBECONFIG": "/home/s/.kube/config"})
            self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
            proof = [json.loads(line) for line in result.stdout.splitlines()]
            self.assertEqual(len(proof), 2)
            self.assertTrue(all(item["digest"] == digest and item["evidence"] == "NODE_HTTPS_GRAPH" and
                                item["criPull"] == "NOT_CHECKED" for item in proof))
            self.assertNotIn("SENTINEL", result.stdout+result.stderr+(directory/"argv.jsonl").read_text())
            self.assertFalse((state/"cache").exists())
            (directory/"config.json").write_text('{"configs":{"password":"PRIVATE_SENTINEL"}}')
            denied = subprocess.run([sys.executable, str(SOURCE), "--context", "k3d-kodex",
                "--state-directory", str(state), "--render", str(render), "--timeout", "10"],
                capture_output=True, text=True, timeout=15,
                env={**os.environ, "PATH": str(binary)+os.pathsep+os.environ["PATH"],
                     "FIXTURE_ROOT": root, "KUBECONFIG": "/home/s/.kube/config"})
            self.assertNotEqual(denied.returncode, 0)
            self.assertNotIn("SENTINEL", denied.stdout+denied.stderr)

    def test_capture_bounds_timeout_and_private_stderr(self):
        with self.assertRaises(MODULE.Failure) as error:
            MODULE.capture([sys.executable, "-c", "import sys;print('PRIVATE_SENTINEL',file=sys.stderr);sys.stdout.write('x'*5000000)"], time.monotonic()+5)
        self.assertEqual(str(error.exception), "COMMAND_BOUND_EXCEEDED")
        with self.assertRaises(MODULE.Failure) as error:
            MODULE.capture([sys.executable, "-c", "import time;time.sleep(2)"], time.monotonic()+.02)
        self.assertEqual(str(error.exception), "COMMAND_TIMEOUT")


if __name__ == "__main__":
    unittest.main()
