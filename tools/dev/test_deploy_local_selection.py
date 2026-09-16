"""Проверки закрытого выбора локального workload без доступа к кластеру."""

from pathlib import Path
import subprocess
import unittest


SCRIPT = Path(__file__).with_name("deploy-local.sh")


class DeployLocalSelectionTest(unittest.TestCase):
    def run_selection(self, workload, stage="core"):
        return subprocess.run(
            ["bash", str(SCRIPT), "--context", "synthetic-local",
             "--mode", "readback", "--security-profile", "trusted-cluster",
             "--stage", stage, "--workload", workload,
             "--render", "/nonexistent-kodex-render-fixture.yaml"],
            capture_output=True, text=True, timeout=5,
        )

    def test_stt_reaches_render_guard_without_cluster_access(self):
        result = self.run_selection("stt-tts-service")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("local render is invalid", result.stderr)
        self.assertNotIn("workload selection", result.stderr)

    def test_unknown_and_noncore_selection_are_rejected(self):
        for workload, stage in [("stt-provider-smoke", "core"),
                                ("stt-tts-service;echo invalid", "core"),
                                ("stt-tts-service", "data"),
                                ("stt-tts-service", "network")]:
            with self.subTest(workload=workload, stage=stage):
                result = self.run_selection(workload, stage)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("workload selection requires", result.stderr)


if __name__ == "__main__":
    unittest.main()
