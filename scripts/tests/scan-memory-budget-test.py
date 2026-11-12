#!/usr/bin/env python3
"""Исполнение настоящего wrapper и Kustomize с синтетическими входами без сети."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "deploy/k8s/base/image-supply-chain/scan-memory-budget.sh"


class ScanMemoryBudgetTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.budget, self.limit = self.root / "budget", self.root / "limit"
        self.limit.write_text("2147483648\n")
        self.calls = self.root / "calls"
        self.env = {"PATH": str(self.root) + ":/usr/bin:/bin", "CALLS": str(self.calls)}
        for tool in ("syft", "grype"):
            p = self.root / tool
            p.write_text("#!/usr/bin/python3\nimport json,os,sys\n"
                         "with open(os.environ['CALLS'],'a') as f: "
                         "f.write(json.dumps({'memory':os.getenv('GOMEMLIMIT'),"
                         "'tool':os.path.basename(sys.argv[0]),'args':sys.argv[1:]})+'\\n')\n"
                         "sys.exit(int(os.getenv('TOOL_EXIT','0')))\n")
            p.chmod(0o700)

    def run_tool(self, tool="syft", args=()):
        return subprocess.run(["/bin/sh", str(SCRIPT), str(self.budget), str(self.limit), tool, *args],
                              env=self.env, capture_output=True, text=True, timeout=5)

    def test_absent_profile_does_not_require_cgroup_or_override_environment(self):
        self.limit.unlink()
        self.env["GOMEMLIMIT"] = "768MiB"
        self.assertEqual(self.run_tool().returncode, 0)
        self.assertEqual(json.loads(self.calls.read_text())["memory"], "768MiB")

    def test_both_tools_receive_budget_and_literal_arguments(self):
        self.budget.write_text("1536\n")
        args = ["--config", "/tmp/example config.json", "$(do-not-execute)", "a;b"]
        for tool in ("syft", "grype"):
            with self.subTest(tool=tool):
                self.assertEqual(self.run_tool(tool, args).returncode, 0)
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()]
        self.assertEqual([v["tool"] for v in calls], ["syft", "grype"])
        self.assertTrue(all(v["memory"] == "1536MiB" and v["args"] == args for v in calls))

    def test_tool_failure_is_propagated_without_retry(self):
        self.budget.write_text("1536")
        self.env["TOOL_EXIT"] = "23"
        self.assertEqual(self.run_tool().returncode, 23)
        self.assertEqual(len(self.calls.read_text().splitlines()), 1)

    def test_invalid_budgets_fail_before_tool_and_do_not_echo_contents(self):
        for value in ("", "0", "0128", "127", "65537", "1537", "-1", "1GiB", "1536\n\n",
                      "1536\r\n", " 1536", "9" * 10000, "$(SYNTHETIC_SECRET)"):
            with self.subTest(value=value[:20]):
                self.budget.write_text(value)
                result = self.run_tool()
                self.assertEqual(result.returncode, 78)
                self.assertEqual(result.stderr, "scan memory budget is invalid\n")
                self.assertFalse(self.calls.exists())

    def test_invalid_cgroup_and_dangling_profile_are_closed(self):
        self.budget.write_text("1536")
        for value in ("max", "", "0", "01073741824", "1073741824", "2147483648\n\n",
                      "9" * 10000, "999999999999999", "2147483648\r\n"):
            with self.subTest(limit=value[:20]):
                self.limit.write_text(value)
                self.assertEqual(self.run_tool().returncode, 78)
                self.assertFalse(self.calls.exists())
        self.budget.unlink()
        self.budget.symlink_to(self.root / "absent")
        self.assertEqual(self.run_tool().returncode, 78)
        self.assertFalse(self.calls.exists())

    def test_unknown_tool_cannot_execute(self):
        self.assertEqual(self.run_tool("sh", ["-c", "exit 0"]).returncode, 64)
        self.assertFalse(self.calls.exists())

    def test_component_changes_only_optional_configmap_key(self):
        # Настоящий Kustomize проверяет базу и оба полных профиля поставки.
        for relative in ("base/image-supply-chain", "profiles/web-only", "profiles/web-with-mattermost"):
            with self.subTest(profile=relative):
                self.assert_component_delta(ROOT / "deploy/k8s" / relative)

    def assert_component_delta(self, base):
        k = self.root / "kustomization.yaml"
        component = ROOT / "deploy/k8s/components/image-admission-memory-budget"
        header = "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"
        k.write_text(header + "resources:\n- " + os.path.relpath(base, self.root) + "\n")
        def render():
            r = subprocess.run(["kubectl", "kustomize", str(self.root), "--load-restrictor=LoadRestrictionsNone"],
                               env={"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": str(self.root)},
                               capture_output=True, text=True, timeout=45, check=True)
            d = subprocess.run(["yq", "-o=json", "-I=0", ".", "-"], input=r.stdout,
                               capture_output=True, text=True, timeout=10, check=True)
            return [json.loads(line) for line in d.stdout.splitlines() if line.strip()]
        before = render()
        k.write_text(k.read_text() + "components:\n- " + os.path.relpath(component, self.root) + "\n")
        after = render()
        cm = next(v for v in after if v["kind"] == "ConfigMap" and v["metadata"]["name"] == "kodex-image-admission")
        self.assertEqual(cm["data"].pop("scan-go-memory-limit-mib"), "1536")
        self.assertEqual(cm["data"]["scan-memory-budget.sh"], SCRIPT.read_text())
        self.assertEqual(before, after)


if __name__ == "__main__":
    unittest.main()
