#!/usr/bin/env python3
"""Проверка приватного временного каталога публичного MCP wire-контура."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().with_name("runtime-mcp-catalog-test.sh")


class RuntimeMCPCatalogEntrypointTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="mcp-entrypoint-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.temp = self.root / "private temp"
        self.temp.mkdir(mode=0o700)
        self.bin = self.root / "bin"
        self.bin.mkdir(mode=0o700)
        self.log = self.root / "calls.jsonl"
        go = self.bin / "go"
        go.write_text("""#!/usr/bin/env python3
import json, os, pathlib, stat, sys
fixture = pathlib.Path(os.environ['KODEX_RUNTIME_MCP_CATALOG_FIXTURE'])
phase = 'producer' if 'TestRuntimeMCPCatalogWireProducer' in sys.argv[-1] else 'consumer'
with open(os.environ['MCP_TEST_CALLS'], 'a') as stream:
    stream.write(json.dumps({'phase': phase, 'args': sys.argv[1:],
        'fixture': str(fixture), 'tmpdir': os.environ['TMPDIR'],
        'mode': stat.S_IMODE(fixture.parent.stat().st_mode),
        'gomaxprocs': os.environ['GOMAXPROCS'], 'gowork': os.environ['GOWORK']}) + '\\n')
if os.environ.get('MCP_TEST_FAIL') == phase:
    sys.exit(9)
if phase == 'producer':
    fixture.write_text('{}')
elif not fixture.is_file():
    sys.exit(10)
""", encoding="utf-8")
        go.chmod(0o700)

    def run_script(self, temp=None, fail=""):
        return subprocess.run(
            ["bash", str(SCRIPT)], check=False, capture_output=True, timeout=10,
            env={"PATH": f"{self.bin}:/usr/local/bin:/usr/bin:/bin",
                 "TMPDIR": str(self.temp if temp is None else temp),
                 "GOMAXPROCS": "2", "MCP_TEST_CALLS": str(self.log),
                 "MCP_TEST_FAIL": fail},
        )

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def assert_cleaned(self, calls):
        self.assertEqual(list(self.temp.iterdir()), [])
        for call in calls:
            fixture = Path(call["fixture"])
            self.assertEqual(fixture.parent.parent, self.temp.resolve())
            self.assertFalse(fixture.parent.exists())
            self.assertEqual(call["tmpdir"], str(self.temp.resolve()))
            self.assertEqual(call["mode"], 0o700)
            self.assertEqual(call["gomaxprocs"], "2")
            self.assertEqual(call["gowork"], "off")

    def test_custom_tmpdir_private_fixture_and_cleanup(self):
        self.assertEqual(self.run_script().returncode, 0)
        calls = self.calls()
        self.assertEqual([call["phase"] for call in calls], ["producer", "consumer"])
        for call in calls:
            self.assertEqual(call["args"][2:8], ["test", "-p", "2", "-count=1", "-timeout=60s", "./internal/callback"]
                             if call["phase"] == "producer" else
                             ["test", "-p", "2", "-count=1", "-timeout=60s", "./internal/readiness"])
        self.assert_cleaned(calls)

    def test_producer_failure_cleans_without_consumer(self):
        self.assertEqual(self.run_script(fail="producer").returncode, 9)
        calls = self.calls()
        self.assertEqual([call["phase"] for call in calls], ["producer"])
        self.assert_cleaned(calls)

    def test_consumer_failure_preserves_failure_and_cleanup(self):
        self.assertEqual(self.run_script(fail="consumer").returncode, 9)
        self.assert_cleaned(self.calls())

    def test_symlink_tmpdir_is_resolved(self):
        alias = self.root / "alias"
        alias.symlink_to(self.temp, target_is_directory=True)
        self.assertEqual(self.run_script(temp=alias).returncode, 0)
        self.assert_cleaned(self.calls())

    def test_missing_or_relative_tmpdir_rejected_before_go(self):
        for temp in [self.root / "missing", "relative"]:
            result = self.run_script(temp=temp)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, b"Runtime MCP catalog test failed: temporary directory is invalid\n")
            self.assertFalse(self.log.exists())


if __name__ == "__main__":
    unittest.main()
