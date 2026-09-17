"""Проверки закрытого выбора локального workload без доступа к кластеру."""

from pathlib import Path
import subprocess
import unittest


SCRIPT = Path(__file__).with_name("deploy-local.sh")


class DeployLocalSelectionTest(unittest.TestCase):
    def run_selection(self, workload=None, stage="core"):
        arguments = [
            "bash", str(SCRIPT), "--context", "synthetic-local",
            "--mode", "readback", "--security-profile", "trusted-cluster",
            "--stage", stage,
        ]
        if workload is not None:
            arguments.extend(["--workload", workload])
        arguments.extend([
            "--render", "/nonexistent-kodex-render-fixture.yaml",
        ])
        return subprocess.run(
            arguments,
            capture_output=True, text=True, timeout=5,
        )

    def test_stt_reaches_render_guard_without_cluster_access(self):
        result = self.run_selection("stt-tts-service")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("local render is invalid", result.stderr)
        self.assertNotIn("workload selection", result.stderr)

    def test_supply_chain_reaches_render_guard_without_cluster_access(self):
        result = self.run_selection(stage="supply-chain")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("local render is invalid", result.stderr)
        self.assertNotIn("deployment stage", result.stderr)
        self.assertNotIn("not implemented", result.stderr)

    def test_unknown_and_noncore_selection_are_rejected(self):
        for workload, stage in [("stt-provider-smoke", "core"),
                                ("stt-tts-service;echo invalid", "core"),
                                ("stt-tts-service", "data"),
                                ("stt-tts-service", "network"),
                                ("stt-tts-service", "supply-chain")]:
            with self.subTest(workload=workload, stage=stage):
                result = self.run_selection(workload, stage)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("workload selection requires", result.stderr)


if __name__ == "__main__":
    unittest.main()
