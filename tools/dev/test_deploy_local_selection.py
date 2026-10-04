"""Проверки закрытого выбора локального workload без доступа к кластеру."""

from pathlib import Path
import subprocess
import json
import re
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

    def test_supply_chain_seed_precedes_full_registry_readiness(self):
        source = SCRIPT.read_text()
        stage = source[source.index('  if [[ "$stage" == supply-chain ]]'):]
        node_registry = stage.index('configure-local-node-registry.sh" --mode apply')
        pause = stage.index('pause_local_image_admission_controller')
        reconcile = stage.index('reconcile_local_immutable_image_admission_policy')
        owner_intent = stage.index('apply_render image-admission-owner-intent')
        registries = stage.index('apply_render image-registry-workloads')
        seed = stage.index('seed-local-image-supply-chain.sh')
        full_readiness = stage.index(
            'for workload in kodex-image-registry-pull kodex-image-registry-push'
        )
        self.assertLess(node_registry, pause)
        self.assertLess(
            pause, reconcile, 'controller must stop before immutable policy reconciliation'
        )
        self.assertLess(reconcile, owner_intent)
        self.assertLess(owner_intent, registries)
        self.assertLess(registries, seed)
        self.assertLess(seed, full_readiness)

    def test_supply_chain_materialization_admission_is_exact_and_precedes_controller(self):
        source = SCRIPT.read_text()
        stage = source[source.index('  if [[ "$stage" == supply-chain ]]'):]
        match = re.search(r"apply_render runtime-materialization-admission\s+'([^']*)'", stage)
        self.assertIsNotNone(match)
        self.assertLess(match.start(), stage.index('apply_render image-supply-chain-controllers'))
        names = [
            "runtime-execution-ticket-exact-projection", "runtime-execution-service-account",
            "runtime-execution-rbac", "runtime-execution-network-policy",
            "runtime-revision-exact-configmap-projection", "runtime-role-pod-exact-secret-projection",
        ]
        accepted = [
            {"kind": kind, "metadata": {"name": name}}
            for name in names
            for kind in ("ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding")
        ]
        rejected = [
            {"kind": kind, "metadata": {"name": name}}
            for name in names + ["other-project", "runtime-role-pod-exact-secret-projection-shadow"]
            for kind in ("Secret", "ConfigMap", "Deployment")
        ] + [
            {"kind": kind, "metadata": {"name": name}}
            for name in ("other-project", "runtime-role-pod-exact-secret-projection-shadow")
            for kind in ("ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding")
        ]
        result = subprocess.run(
            ["jq", "-c", match.group(1)], text=True, capture_output=True, timeout=5,
            input="\n".join(json.dumps(item) for item in accepted + rejected),
        )
        self.assertEqual(result.returncode, 0)
        self.assertEqual([json.loads(line) for line in result.stdout.splitlines()], accepted)
        self.assertIn("runtime materialization admission readback mismatch", stage)

    def test_materialization_readback_accepts_only_approved_api_defaults(self):
        source = SCRIPT.read_text()
        match = re.search(
            r"canonical_runtime_admission_specs\(\) \{.*?jq -scS '([^']*)'", source, re.DOTALL,
        )
        self.assertIsNotNone(match)

        def canonical(spec):
            result = subprocess.run(
                ["jq", "-scS", match.group(1)], input=json.dumps(spec),
                text=True, capture_output=True, timeout=5,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            return result.stdout

        for field in ("matchConstraints", "matchResources"):
            raw = {field: {"resourceRules": [{"resources": ["pods"]}]}, "failurePolicy": "Fail"}
            defaulted = json.loads(json.dumps(raw))
            defaulted[field].update({
                "matchPolicy": "Equivalent", "namespaceSelector": {}, "objectSelector": {},
            })
            defaulted[field]["resourceRules"][0]["scope"] = "*"
            self.assertEqual(canonical(raw), canonical(defaulted))
            for key, value in (("matchPolicy", "Exact"), ("namespaceSelector", {"matchLabels": {"foreign": "yes"}})):
                changed = json.loads(json.dumps(defaulted))
                changed[field][key] = value
                self.assertNotEqual(canonical(raw), canonical(changed))
            changed = json.loads(json.dumps(defaulted))
            changed[field]["resourceRules"][0]["scope"] = "Namespaced"
            self.assertNotEqual(canonical(raw), canonical(changed))
            changed = json.loads(json.dumps(defaulted))
            changed["failurePolicy"] = "Ignore"
            self.assertNotEqual(canonical(raw), canonical(changed))
            changed = json.loads(json.dumps(defaulted))
            changed["unexpected"] = True
            self.assertNotEqual(canonical(raw), canonical(changed))

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

    def test_full_core_applies_the_synthetic_integration_fixture(self):
        source = SCRIPT.read_text()
        core = source[source.index('  if [[ "$stage" == core ]]'):]
        full_selection = core[core.index("apply_render core-applications"):]
        full_selection = full_selection[:full_selection.index("      '")]
        self.assertIn("integration-synthetic", full_selection)


if __name__ == "__main__":
    unittest.main()
