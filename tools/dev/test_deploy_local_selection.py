"""Проверки закрытого выбора локального workload без доступа к кластеру."""

from pathlib import Path
import subprocess
import json
import re
import os
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

    def test_exact_broker_bootstrap_is_only_a_migration_selection(self):
        result = self.run_selection("control-plane-broker-bootstrap", "migrate")
        self.assertIn("local render is invalid", result.stderr)
        self.assertNotIn("workload selection", result.stderr)
        for stage in ("core", "data", "network", "supply-chain"):
            result = self.run_selection("control-plane-broker-bootstrap", stage)
            self.assertIn("workload selection requires", result.stderr)
        for workload in ("foreign-broker-bootstrap", "control-plane-broker-bootstrap-extra"):
            result = self.run_selection(workload, "migrate")
            self.assertIn("migration workload selection requires", result.stderr)

    def test_archive_is_an_explicit_core_selection_only(self):
        result = self.run_selection("session-archive")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("local render is invalid", result.stderr)
        source = SCRIPT.read_text()
        core = source[source.index('  if [[ "$stage" == core ]]'):]
        full_selection = core[core.index("apply_render core-applications"):]
        full_selection = full_selection[:full_selection.index("      '")]
        self.assertNotIn("session-archive", full_selection)
        self.assertIn('[[ "$workload" != session-archive || "$selected_workload" == session-archive ]]', core)
        renderer = SCRIPT.with_name("render-local.sh").read_text()
        self.assertIn("patch_go_container Deployment session-archive session-archive services/jobs/session-archive ./cmd/session-archive controller", renderer)
        for stage in ("data", "network", "migrate", "supply-chain"):
            with self.subTest(stage=stage):
                result = self.run_selection("session-archive", stage)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("workload selection requires", result.stderr)

    def test_supply_chain_reaches_render_guard_without_cluster_access(self):
        result = self.run_selection(stage="supply-chain")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("local render is invalid", result.stderr)
        self.assertNotIn("deployment stage", result.stderr)
        self.assertNotIn("not implemented", result.stderr)

    def test_archive_dns_is_owned_by_controller_without_issuer(self):
        policy = SCRIPT.parents[2] / "deploy/k8s/base/session-archive/networkpolicy.yaml"
        result = subprocess.run(
            ["yq", "-o=json", ".", str(policy)],
            capture_output=True, text=True, timeout=5,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        decoder = json.JSONDecoder()
        remaining = result.stdout.lstrip()
        documents = []
        while remaining:
            document, end = decoder.raw_decode(remaining)
            documents.append(document)
            remaining = remaining[end:].lstrip()
        controller = next(item for item in documents if item["metadata"]["name"] == "session-archive-exact-paths")
        self.assertEqual(controller["spec"]["podSelector"]["matchLabels"], {
            "app.kubernetes.io/name": "session-archive",
            "app.kubernetes.io/component": "archive-controller",
        })
        dns = [rule for rule in controller["spec"]["egress"] if any(port["port"] == 53 for port in rule["ports"])]
        self.assertEqual(dns, [{
            "to": [{
                "namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "kube-system"}},
                "podSelector": {"matchLabels": {"k8s-app": "kube-dns"}},
            }],
            "ports": [{"protocol": "UDP", "port": 53}, {"protocol": "TCP", "port": 53}],
        }])

    def test_archive_runtime_secret_precedes_controller_activation(self):
        source = SCRIPT.read_text()
        core = source[source.index('  if [[ "$stage" == core ]]'):]
        archive = core[core.index('if [[ "$selected_workload" == session-archive ]]'):]
        self.assertLess(archive.index("ensure_session_archive_worker_secret"), archive.index("apply_render core-application"))
        self.assertIn("apply_render session-archive-local-configuration", archive)
        readback = source[source.index("readback_session_archive_worker_secret() {"):source.index("readback_session_archive() {")]
        self.assertIn('--slurpfile source "$source_file"', readback)
        self.assertNotIn("--argjson source", readback)
        self.assertIn('chmod 0600 "$source_file"', readback)

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
            'for workload in control-plane control-api-gateway kodex-image-registry-pull kodex-image-registry-push'
        )
        self.assertLess(pause, node_registry)
        self.assertLess(
            pause, reconcile, 'controller must stop before immutable policy reconciliation'
        )
        self.assertLess(reconcile, owner_intent)
        self.assertLess(owner_intent, registries)
        self.assertLess(registries, seed)
        self.assertLess(seed, full_readiness)

    def test_supply_chain_refreshes_policy_owner_before_resuming_claims(self):
        source = SCRIPT.read_text()
        stage = source[source.index('  if [[ "$stage" == supply-chain ]]'):]
        pause = stage.index('pause_local_image_admission_controller')
        closed = stage.index('image_admission_policy_owner_coherent=false')
        policy = stage.index('apply_render image-admission-owner-intent')
        catalog = stage.index('apply_render role-environment-catalog')
        owner = stage.index('apply_render image-admission-control-plane-owner')
        ready = stage.index('rollout status deployment/control-plane')
        readback = stage.index('readback_local_control_plane_image_policy')
        coherent = stage.index('image_admission_policy_owner_coherent=true')
        resume = stage.index('apply_render image-supply-chain-controllers')
        self.assertEqual(sorted((closed, pause, policy, catalog, owner, ready, readback, resume, coherent)),
                         [closed, pause, policy, catalog, owner, ready, readback, resume, coherent])
        cleanup = re.search(r'(?ms)^cleanup_on_exit\(\) \{.*?^\}', source).group(0)
        self.assertIn('"$image_admission_policy_owner_coherent" == true', cleanup)
        renderer = SCRIPT.with_name('render-local.sh').read_text()
        self.assertIn('kodex.dev/image-policy-revision', renderer)
        match = re.search(r"apply_render image-admission-control-plane-owner\s+'([^']*)'", stage)
        accepted = {'kind': 'Deployment', 'metadata': {'namespace': 'kodex-system', 'name': 'control-plane'}}
        rejected = [dict(accepted, kind='ConfigMap'),
                    {'kind': 'Deployment', 'metadata': {'namespace': 'other', 'name': 'control-plane'}},
                    {'kind': 'Deployment', 'metadata': {'namespace': 'kodex-system', 'name': 'control-plane-other'}}]
        result = subprocess.run(['jq', '-c', match.group(1)], text=True, capture_output=True,
                                input='\n'.join(json.dumps(item) for item in [accepted] + rejected), timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([json.loads(line) for line in result.stdout.splitlines()], [accepted])

    def test_policy_owner_readback_requires_live_owner_pod_and_actual_process(self):
        source = SCRIPT.read_text()
        function = re.search(r'(?ms)^readback_local_control_plane_image_policy\(\) \{.*?^\}', source).group(0)
        policy = {'policySHA256': 'a' * 64, 'policyRevision': '4'}
        annotations = {'kodex.dev/runtime-admission-policy-sha256': policy['policySHA256'],
                       'kodex.dev/image-policy-revision': policy['policyRevision']}
        labels = {'app.kubernetes.io/part-of': 'kodex', 'app.kubernetes.io/name': 'control-plane',
                  'kodex.dev/local-profile': 'hot-reload', 'kodex.dev/security-profile': 'trusted-cluster'}
        deployment = {'metadata': {'namespace': 'kodex-system', 'name': 'control-plane',
                                  'uid': 'deployment-uid', 'generation': 2, 'labels': labels},
                      'spec': {'replicas': 1, 'template': {'metadata': {'annotations': annotations},
                              'spec': {'containers': [{'name': 'control-plane', 'env': [
                                  {'name': 'CONTROL_PLANE_IMAGE_POLICY_REVISION', 'value': '4'},
                                  {'name': 'CONTROL_PLANE_IMAGE_POLICY_SHA256', 'value': 'a' * 64}]}]}}},
                      'status': {'observedGeneration': 2, 'updatedReplicas': 1, 'availableReplicas': 1, 'replicas': 1}}
        replica_set = {'metadata': {'namespace': 'kodex-system', 'uid': 'replica-set-uid',
                                   'ownerReferences': [{'kind': 'Deployment', 'controller': True, 'uid': 'deployment-uid'}]},
                       'spec': {'replicas': 1, 'template': {'metadata': {'annotations': annotations}}}}
        pod = {'metadata': {'namespace': 'kodex-system', 'name': 'control-plane-new', 'labels': labels,
                            'annotations': annotations,
                            'ownerReferences': [{'kind': 'ReplicaSet', 'controller': True, 'uid': 'replica-set-uid'}]},
               'status': {'phase': 'Running', 'conditions': [{'type': 'Ready', 'status': 'True'}],
                          'containerStatuses': [{'name': 'control-plane', 'ready': True}]}}
        command = function + '''
fail() { printf '%s\\n' "$1" >&2; exit 1; }
yq() { printf '%s\\n' "$EXPECTED_POLICY"; }
kubectl() {
  case "$*" in
    *get\\ configmap/*) printf '{"data":%s}\\n' "$CURRENT_POLICY" ;;
    *get\\ deployment/*) printf '%s\\n' "$DEPLOYMENT" ;;
    *get\\ replicasets*) printf '{"items":[%s]}\\n' "$REPLICA_SET" ;;
    *get\\ pods*) printf '{"items":%s}\\n' "$PODS" ;;
    *exec*) sh -n -c "${!#}" || exit 98; printf '%s\\n' "$PROCESS_POLICY" ;;
    *) exit 99 ;;
  esac
}
namespace=kodex-system
render=synthetic
readback_local_control_plane_image_policy
'''
        base = dict(os.environ, EXPECTED_POLICY=json.dumps(policy), CURRENT_POLICY=json.dumps(policy),
                    DEPLOYMENT=json.dumps(deployment), REPLICA_SET=json.dumps(replica_set), PODS=json.dumps([pod]),
                    PROCESS_POLICY='4\n' + 'a' * 64)
        cases = [(base, None), (dict(base, PROCESS_POLICY='4\n' + 'b' * 64), 'process readback mismatch'),
                 (dict(base, PROCESS_POLICY='3\n' + 'a' * 64), 'process readback mismatch'),
                 (dict(base, PODS='[]'), 'Ready Pod readback mismatch'),
                 (dict(base, PODS=json.dumps([pod, pod])), 'Ready Pod readback mismatch'),
                 (dict(base, CURRENT_POLICY=json.dumps(dict(policy, policySHA256='b' * 64))), 'live image policy readback mismatch')]
        stale_deployment = json.loads(json.dumps(deployment))
        stale_deployment['spec']['template']['spec']['containers'][0]['env'][1]['value'] = 'b' * 64
        cases.append((dict(base, DEPLOYMENT=json.dumps(stale_deployment)), 'Deployment readback mismatch'))
        not_ready = json.loads(json.dumps(deployment))
        not_ready['status']['observedGeneration'] = 1
        cases.append((dict(base, DEPLOYMENT=json.dumps(not_ready)), 'Deployment readback mismatch'))
        foreign_replica_set = json.loads(json.dumps(replica_set))
        foreign_replica_set['metadata']['ownerReferences'][0]['uid'] = 'foreign-deployment'
        cases.append((dict(base, REPLICA_SET=json.dumps(foreign_replica_set)), 'ReplicaSet readback mismatch'))
        foreign_pod = json.loads(json.dumps(pod))
        foreign_pod['metadata']['ownerReferences'][0]['uid'] = 'foreign-replica-set'
        cases.append((dict(base, PODS=json.dumps([foreign_pod])), 'Ready Pod readback mismatch'))
        for env, error in cases:
            with self.subTest(error=error):
                result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command],
                                        env=env, text=True, capture_output=True, timeout=5)
                if error is None:
                    self.assertEqual(result.returncode, 0, result.stderr)
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(error, result.stderr)
        self.assertIn('/go/build-cache/runtime-control-plane/build/main', function)
        self.assertNotIn('-- printenv', function)

    def test_failed_policy_owner_gate_cannot_resume_controller_in_exit_cleanup(self):
        source = SCRIPT.read_text()
        cleanup = re.search(r'(?ms)^cleanup_on_exit\(\) \{.*?^\}', source).group(0)
        command = 'exec 3>&1\n' + cleanup + '''
kubectl() { printf 'resumed\\n'; }
rm() { return 0; }
namespace=kodex-system
temporary_directory=/synthetic-only
image_admission_controller_restore_replicas=1
image_admission_policy_owner_coherent=$COHERENT
cleanup_on_exit
'''
        # Scale stdout подавлен самим cleanup; записываем только synthetic marker.
        command = command.replace("kubectl() { printf 'resumed\\n'; }",
                                  "kubectl() { printf 'resumed\\n' >&3; }")
        for coherent, expected in [('true', 'resumed\n'), ('false', '')]:
            result = subprocess.run(['bash', '-euo', 'pipefail', '-c', 'exec 3>&1\n' + command],
                                    env=dict(os.environ, COHERENT=coherent), text=True, capture_output=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, expected)

    def test_policy_revision_rollout_annotation_is_exact_without_source_changes(self):
        renderer = SCRIPT.with_name('render-local.sh').read_text()
        scoped = renderer[renderer.index('# Policy задаёт authority процесса CP'):]
        expression = re.search(r"yq -i '([^']*)'", scoped).group(1)
        self.assertLess(scoped.index('if [[ "$security_profile" == trusted-cluster ]]'), scoped.index('yq -i'))
        for revision, digest in [('4', 'a' * 64), ('5', 'b' * 64)]:
            resources = [{'kind': kind, 'metadata': {'name': name, 'namespace': namespace},
                          'spec': {'template': {'metadata': {'annotations': {
                              'kodex.dev/source-revision': 'same-source-sha',
                              'kodex.dev/runtime-admission-policy-sha256': digest}}}}}
                         for kind, name, namespace in [('Deployment', 'control-plane', 'kodex-system'),
                                                       ('Deployment', 'control-plane', 'other'),
                                                       ('Deployment', 'other', 'kodex-system'),
                                                       ('StatefulSet', 'control-plane', 'kodex-system')]]
            result = subprocess.run(['yq', '-N', '-o=json', '-I=0', expression, '-'],
                                    input='\n---\n'.join(json.dumps(item) for item in resources),
                                    env=dict(os.environ, ADMISSION_POLICY_JSON=json.dumps({
                                        'policyRevision': revision, 'policySHA256': digest})),
                                    text=True, capture_output=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            rendered = [json.loads(line) for line in result.stdout.splitlines()]
            self.assertEqual(rendered[0]['spec']['template']['metadata']['annotations'], {
                'kodex.dev/source-revision': 'same-source-sha',
                'kodex.dev/runtime-admission-policy-sha256': digest,
                'kodex.dev/image-policy-revision': revision})
            self.assertEqual(rendered[1:], resources[1:])

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
        self.assertIn("readback_local_runtime_materialization_admission", stage)
        readback = re.search(r'(?ms)^readback_local_runtime_materialization_admission\(\) \{.*?^\}', source).group(0)
        self.assertIn("runtime materialization admission readback mismatch", readback)

    def test_supply_chain_admission_script_is_updated_while_controller_is_paused(self):
        source = SCRIPT.read_text()
        stage = source[source.index('  if [[ "$stage" == supply-chain ]]'):]
        match = re.search(r"apply_render image-admission-runtime-configuration\s+'([^']*)'", stage)
        self.assertIsNotNone(match)
        self.assertLess(stage.index('require_empty_local_image_admission_runs'), match.start())
        self.assertLess(match.start(), stage.index('apply_render image-supply-chain-controllers'))
        accepted = {"kind": "ConfigMap", "metadata": {"name": "kodex-image-admission", "namespace": "kodex-system"}}
        rejected = [
            {"kind": kind, "metadata": {"name": name, "namespace": namespace}}
            for kind, name, namespace in (
                ("Secret", "kodex-image-admission", "kodex-system"),
                ("ConfigMap", "kodex-image-admission", "other-project"),
                ("ConfigMap", "kodex-image-admission-shadow", "kodex-system"),
                ("ConfigMap", "kodex-image-admission-policy", "kodex-system"),
            )
        ]
        result = subprocess.run(
            ["jq", "-c", match.group(1)], text=True, capture_output=True, timeout=5,
            input="\n".join(json.dumps(item) for item in [accepted] + rejected),
        )
        self.assertEqual(result.returncode, 0)
        self.assertEqual([json.loads(line) for line in result.stdout.splitlines()], [accepted])
        self.assertIn("live image admission runtime configuration readback mismatch", source)

    def test_supply_chain_image_admission_registry_is_exact_and_gates_controller(self):
        source = SCRIPT.read_text()
        stage = source[source.index('  if [[ "$stage" == supply-chain ]]'):]
        names = ["kodex-image-admission-controller-jobs",
                 "kodex-image-admission-controller-workspaces",
                 "kodex-image-admission-proof-release"]
        previous = stage.index('apply_render image-admission-owner-intent')
        for step, kind in (("policies", "ValidatingAdmissionPolicy"),
                           ("bindings", "ValidatingAdmissionPolicyBinding")):
            match = re.search(r"apply_render image-admission-controller-" + step + r"\s+'([^']*)'", stage)
            self.assertIsNotNone(match)
            self.assertLess(previous, match.start())
            previous = match.start()
            accepted = [{"kind": kind, "metadata": {"name": name}} for name in names]
            rejected = [{"kind": other, "metadata": {"name": name}}
                        for other in ("Secret", "ConfigMap", "Deployment",
                                      "ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding")
                        for name in names + [names[0] + "-shadow", "other-project"]
                        if other != kind or name not in names]
            result = subprocess.run(["jq", "-c", match.group(1)], text=True,
                                    capture_output=True, timeout=5,
                                    input="\n".join(json.dumps(item) for item in accepted + rejected))
            self.assertEqual(result.returncode, 0)
            self.assertEqual([json.loads(line) for line in result.stdout.splitlines()], accepted)
        gate = stage.index('      readback_local_image_admission_policies')
        self.assertLess(previous, gate)
        self.assertLess(gate, stage.index('apply_render image-supply-chain-controllers'))
        self.assertLess(stage.index('pause_local_image_admission_controller'), previous)
        readback = source[source.index('readback_local_image_supply_chain() {'):]
        self.assertIn('  readback_local_image_admission_policies', readback)

    def test_image_admission_readback_rejects_live_scan_drift_and_incomplete_registry(self):
        source = SCRIPT.read_text()
        functions = "\n".join(re.search(r"(?ms)^" + name + r"\(\) \{.*?^\}", source).group(0)
                              for name in ("canonical_runtime_admission_specs",
                                           "readback_local_image_admission_policies"))
        command = functions + '''
fail() { printf '%s\\n' "$1" >&2; exit 1; }
yq() { printf '%s\\n' "$EXPECTED_SPEC"; }
kubectl() { printf '{"metadata":{"generation":1},"status":{"observedGeneration":1,"typeChecking":{"expressionWarnings":[]}},"spec":%s}\\n' "$ACTUAL_SPEC"; }
render=synthetic-render
readback_local_image_admission_policies
'''
        expected = {"failurePolicy": "Fail", "matchConstraints": {
            "resourceRules": [{"resources": ["jobs"]}]},
            "validations": [{"expression": "scan tmp == 32Gi"}]}
        defaulted = json.loads(json.dumps(expected))
        defaulted["matchConstraints"].update({"matchPolicy": "Equivalent",
                                            "namespaceSelector": {}, "objectSelector": {}})
        defaulted["matchConstraints"]["resourceRules"][0]["scope"] = "*"
        drift = json.loads(json.dumps(defaulted))
        drift["validations"][0]["expression"] = "scan tmp == 1Gi"
        for rendered, actual, error in (
            (json.dumps(expected), defaulted, None),
            (json.dumps(expected), drift, "readback mismatch"),
            ("", defaulted, "registry is incomplete"),
            (json.dumps(expected) + "\n" + json.dumps(expected), defaulted, "registry is incomplete"),
        ):
            with self.subTest(error=error):
                env = dict(os.environ, EXPECTED_SPEC=rendered, ACTUAL_SPEC=json.dumps(actual))
                result = subprocess.run(["bash", "-euo", "pipefail", "-c", command],
                                        env=env, capture_output=True, text=True, timeout=5)
                if error is None:
                    self.assertEqual(result.returncode, 0, result.stderr)
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(error, result.stderr)

    def test_control_plane_catalog_precedes_start_and_pins_pod_template(self):
        source = SCRIPT.read_text()
        stage = source[source.index('  if [[ "$stage" == core ]]'):]
        match = re.search(r"apply_render core-role-environment-catalog\s+'([^']*)'", stage)
        self.assertIsNotNone(match)
        self.assertLess(match.start(), stage.index('apply_render core-application'))
        accepted = {"kind": "ConfigMap", "metadata": {"name": "kodex-role-environments", "namespace": "kodex-system"}}
        rejected = [
            {"kind": "Secret", "metadata": accepted["metadata"]},
            {"kind": "ConfigMap", "metadata": {"name": "kodex-role-environments", "namespace": "other-project"}},
        ]
        result = subprocess.run(
            ["jq", "-c", match.group(1)], text=True, capture_output=True, timeout=5,
            input="\n".join(json.dumps(item) for item in [accepted] + rejected),
        )
        self.assertEqual(result.returncode, 0)
        self.assertEqual([json.loads(line) for line in result.stdout.splitlines()], [accepted])
        renderer = SCRIPT.with_name("render-local.sh").read_text()
        self.assertIn('kodex.dev/role-environment-catalog-sha256', renderer)
        self.assertIn('ROLE_ENVIRONMENT_CATALOG_DIGEST', renderer)

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

    def test_supply_chain_upgrade_requires_forward_owner_and_exact_rbac_before_resume(self):
        source = SCRIPT.read_text()
        stage = source[source.index('  if [[ "$stage" == supply-chain ]]'):]
        steps = ['image_admission_policy_owner_coherent=false',
                 'pause_local_image_admission_controller', 'require_empty_local_image_admission_runs',
                 'service-identity-policy.mjs" check', 'apply_job control-plane-migrate',
                 'apply_render image-admission-controller-policies',
                 'readback_local_image_admission_policies',
                 'apply_render image-admission-controller-rbac',
                 'readback_local_image_admission_controller_rbac',
                 'apply_render image-admission-control-plane-owner',
                 'readback_local_control_plane_image_policy',
                 'apply_render image-supply-chain-controllers']
        positions = [stage.index(step) for step in steps]
        self.assertEqual(positions, sorted(positions))
        upgrade = stage[:stage.index('  if [[ "$stage" == builder-runtime ]]')]
        self.assertNotIn('cleanup_local_image_admission_runs', upgrade)
        self.assertNotIn('apply_render authority-publisher', upgrade)
        expression = re.search(r"apply_render image-admission-controller-rbac\s+'([^']*)'", stage).group(1)
        accepted = [{'kind': kind, 'metadata': {'namespace': 'kodex-system', 'name': 'image-admission-controller'}}
                    for kind in ('Role', 'RoleBinding')]
        rejected = [{'kind': kind, 'metadata': {'namespace': namespace, 'name': name}}
                    for kind in ('Role', 'RoleBinding', 'ClusterRole', 'Secret')
                    for namespace in ('kodex-system', 'foreign')
                    for name in ('image-admission-controller', 'image-admission-controller-shadow')
                    if not (kind in ('Role', 'RoleBinding') and namespace == 'kodex-system'
                            and name == 'image-admission-controller')]
        result = subprocess.run(['jq', '-c', expression], capture_output=True, text=True, timeout=5,
                                input='\n'.join(json.dumps(item) for item in accepted + rejected))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([json.loads(line) for line in result.stdout.splitlines()], accepted)

    def test_upgrade_empty_inventory_is_non_destructive_and_fails_closed(self):
        source = SCRIPT.read_text()
        function = re.search(r'(?ms)^require_empty_local_image_admission_runs\(\) \{.*?^\}', source).group(0)
        command = function + '''
fail() { printf '%s\\n' "$1" >&2; exit 1; }
kubectl() { printf '%s\\n' "$INVENTORY"; return "$KUBE_EXIT"; }
namespace=kodex-system
require_empty_local_image_admission_runs
'''
        for inventory, kube_exit, error in (({'items': []}, '0', None),
                ({'items': [{'kind': 'Job'}]}, '0', 'requires an empty'),
                ({'items': [{'kind': 'PersistentVolumeClaim'}]}, '0', 'requires an empty'),
                ({}, '0', 'requires an empty'), ({'items': []}, '1', 'unavailable')):
            with self.subTest(inventory=inventory, kube_exit=kube_exit):
                result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command],
                    env=dict(os.environ, INVENTORY=json.dumps(inventory), KUBE_EXIT=kube_exit),
                    text=True, capture_output=True, timeout=5)
                self.assertEqual(result.returncode == 0, error is None)
                if error:
                    self.assertIn(error, result.stderr)
        self.assertNotIn('delete', function)

    def test_immutable_policy_change_is_guarded_before_each_delete(self):
        source = SCRIPT.read_text()
        self.assertNotIn('cleanup_local_image_admission_runs() {', source)
        self.assertNotIn('delete jobs,persistentvolumeclaims', source)
        functions = '\n'.join(re.search(r'(?ms)^' + name + r'\(\) \{.*?^\}', source).group(0)
                              for name in ('require_empty_local_image_admission_runs',
                                           'require_local_image_admission_policy_upgrade',
                                           'reconcile_local_immutable_image_admission_policy'))
        labels = {'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload'}
        desired_config = {'immutable': True, 'metadata': {'labels': labels}, 'data': {'revision': 'new'}}
        desired_parameters = {'metadata': {'labels': labels}, 'spec': {'revision': 'new'}}
        changed_config = dict(desired_config, data={'revision': 'old'})
        changed_parameters = dict(desired_parameters, spec={'revision': 'old'})
        command = functions + '''
exec 3>&1
fail() { printf '%s\\n' "$1" >&2; exit 1; }
yq() {
  if [[ "$*" == *ImageAdmissionPolicyParameters* ]]; then printf '%s\\n' "$DESIRED_PARAMETERS";
  else printf '%s\\n' "$DESIRED_CONFIG"; fi
}
kubectl() {
  case "$*" in
    *get\\ jobs,persistentvolumeclaims*) printf '%s\\n' "$INVENTORY" ;;
    *get\\ configmap/*) printf '%s\\n' "$CURRENT_CONFIG" ;;
    *get\\ imageadmissionpolicyparameters/*) printf '%s\\n' "$CURRENT_PARAMETERS" ;;
    *delete*) printf 'deleted\\n' >&3 ;;
    *) return 99 ;;
  esac
}
namespace=kodex-system
render=synthetic
stage=$TEST_STAGE
reconcile_local_immutable_image_admission_policy
'''
        cases = [
            ('data', '', '', [], 0, None),
            ('data', desired_config, desired_parameters, [], 0, None),
            ('data', changed_config, desired_parameters, [], 0, 'requires supply-chain or full'),
            ('data', desired_config, changed_parameters, [], 0, 'requires supply-chain or full'),
            ('supply-chain', changed_config, desired_parameters, [{'kind': 'PersistentVolumeClaim'}], 0, 'requires an empty'),
            ('full', desired_config, changed_parameters, [{'kind': 'Job'}], 0, 'requires an empty'),
            ('supply-chain', changed_config, changed_parameters, [], 2, None),
            ('full', desired_config, changed_parameters, [], 1, None),
        ]
        for stage, config, parameters, items, deletes, error in cases:
            with self.subTest(stage=stage, deletes=deletes, error=error):
                result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command],
                    env=dict(os.environ, DESIRED_CONFIG=json.dumps(desired_config),
                             DESIRED_PARAMETERS=json.dumps(desired_parameters),
                             CURRENT_CONFIG=json.dumps(config) if config else '',
                             CURRENT_PARAMETERS=json.dumps(parameters) if parameters else '',
                             INVENTORY=json.dumps({'items': items}), TEST_STAGE=stage),
                    text=True, capture_output=True, timeout=5)
                self.assertEqual(result.returncode == 0, error is None, result.stderr)
                self.assertEqual(result.stdout.count('deleted'), deletes)
                if error: self.assertIn(error, result.stderr)

    def test_controller_rbac_projection_runs_with_actual_yq(self):
        source = SCRIPT.read_text()
        function = re.search(r'(?ms)^readback_local_image_admission_controller_rbac\(\) \{.*?^\}', source).group(0)
        expression = re.search(r"yq -o=json -I=0 '([^']+)'", function).group(1)
        metadata = {'namespace': 'kodex-system', 'name': 'image-admission-controller'}
        resources = [{'kind': 'Role', 'metadata': metadata, 'rules': []},
                     {'kind': 'RoleBinding', 'metadata': metadata, 'subjects': [], 'roleRef': {}},
                     {'kind': 'Role', 'metadata': dict(metadata, namespace='foreign'), 'rules': []}]
        result = subprocess.run(['yq', '-o=json', '-I=0', expression],
                                input='\n---\n'.join(json.dumps(item) for item in resources),
                                capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        projected = [json.loads(line) for line in result.stdout.splitlines()]
        self.assertEqual([item['kind'] for item in projected], ['Role', 'RoleBinding'])
        self.assertTrue(all(set(item) == {'kind', 'rules', 'roleRef', 'subjects'} for item in projected))

    def test_exact_controller_rbac_readback_rejects_missing_update_and_extra_rights(self):
        source = SCRIPT.read_text()
        function = re.search(r'(?ms)^readback_local_image_admission_controller_rbac\(\) \{.*?^\}', source).group(0)
        expected = [{'kind': 'Role', 'rules': [{'apiGroups': [''], 'resources': ['persistentvolumeclaims'],
                     'verbs': ['get', 'list', 'create', 'update', 'delete']}], 'subjects': None, 'roleRef': None},
                    {'kind': 'RoleBinding', 'rules': None,
                     'subjects': [{'kind': 'ServiceAccount', 'name': 'image-admission-controller', 'namespace': 'kodex-system'}],
                     'roleRef': {'kind': 'Role', 'name': 'image-admission-controller', 'apiGroup': 'rbac.authorization.k8s.io'}}]
        command = function + '''
fail() { printf '%s\\n' "$1" >&2; exit 1; }
yq() { jq -c '.[]' <<<"$EXPECTED"; }
kubectl() { printf '{"items":%s}\\n' "$ACTUAL"; }
namespace=kodex-system
render=synthetic
readback_local_image_admission_controller_rbac
'''
        cases = [(expected, None), (expected[:1], 'mismatch')]
        for mutate in ('missing-update', 'extra-secret', 'foreign-actor'):
            actual = json.loads(json.dumps(expected))
            if mutate == 'missing-update': actual[0]['rules'][0]['verbs'].remove('update')
            elif mutate == 'extra-secret': actual[0]['rules'].append({'apiGroups': [''], 'resources': ['secrets'], 'verbs': ['get']})
            else: actual[1]['subjects'][0]['name'] = 'other'
            cases.append((actual, 'mismatch'))
        for actual, error in cases:
            with self.subTest(error=error, actual=actual):
                result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command],
                    env=dict(os.environ, EXPECTED=json.dumps(expected), ACTUAL=json.dumps(actual)),
                    text=True, capture_output=True, timeout=5)
                self.assertEqual(result.returncode == 0, error is None, result.stderr)
                if error: self.assertIn(error, result.stderr)

    def test_protected_publisher_and_owner_are_ready_before_image_controller(self):
        source = SCRIPT.read_text()
        full = source[source.index('if [[ "$mode" == apply ]]; then\n  verify_email_projection_generation'):]
        steps = ['image_admission_policy_owner_coherent=false', 'pause_local_image_admission_controller',
                 'require_empty_local_image_admission_runs', 'apply_job control-plane-migrate', 'apply_render authority-publisher',
                 'apply_render application-workloads',
                 'rollout status deployment/internal-rpc-authority-publisher', 'rollout status deployment/control-plane',
                 'apply_render image-admission-workloads']
        positions = [full.index(step) for step in steps]
        self.assertEqual(positions, sorted(positions))
        self.assertNotIn('cleanup_local_image_admission_runs', full)

    def test_supply_chain_failed_upgrade_never_resumes_controller(self):
        source = SCRIPT.read_text()
        stage = source[source.index('  if [[ "$stage" == supply-chain ]]'):]
        stage = stage[:stage.index('\n    readback_local_runtime_materialization_admission\n')] + '\n  fi\n'
        cleanup = re.search(r'(?ms)^cleanup_on_exit\(\) \{.*?^\}', source).group(0)
        command = cleanup + '''
fail() { printf 'closed\\n' >&2; exit 1; }
phase() { printf '%s\\n' "$1"; [[ "$1" != "$FAIL_PHASE" ]] || fail; }
apply_render() { phase "$1"; }
apply_job() { phase "$1"; }
pause_local_image_admission_controller() { phase pause; image_admission_controller_restore_replicas=1; }
require_empty_local_image_admission_runs() { phase preflight; }
readback_local_image_admission_policies() { phase vap-readback; }
readback_local_runtime_materialization_admission() { phase runtime-vap-readback; }
readback_local_image_admission_controller_rbac() { phase rbac-readback; }
readback_local_control_plane_image_policy() { phase owner-readback; }
readback_local_supply_chain_deployment_inputs() { phase source-readback; }
handover_local_image_admission_pause() { phase handover; }
apply_image_admission_crd() { phase crd-readback; }
readback_local_claim_evidence_network() { phase network-readback; }
readback_local_supply_chain_configuration() { phase configuration-readback; }
reconcile_local_immutable_image_admission_policy() { phase policy; }
ensure_seed_secrets() { :; }
yq() { printf 'synthetic-public-host\\n'; }
node() { phase identity-policy; }
kubectl() { if [[ "$*" == *scale* ]]; then printf 'unexpected-resume\\n' >&3; else phase kube-ready; fi; }
rm() { :; }
stage=supply-chain
mode=apply
namespace=kodex-system
context=synthetic-local
render=synthetic
state_directory=/synthetic
script_directory=$STUB_DIRECTORY
temporary_directory=/synthetic
image_admission_controller_restore_replicas=""
image_admission_policy_owner_coherent=true
trap cleanup_on_exit EXIT
''' + stage
        # Внешние repo entrypoints заменены inert fixtures; cluster/docker не вызываются.
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            for name in ('configure-local-node-registry.sh', 'seed-local-image-supply-chain.sh'):
                path = Path(directory) / name
                path.write_text('#!/bin/sh\nexit 0\n')
                path.chmod(0o700)
            for failed in ('pause', 'preflight', 'runtime-vap-readback', 'identity-policy', 'control-plane-migrate',
                           'crd-readback', 'network-readback', 'configuration-readback',
                           'vap-readback', 'rbac-readback', 'kube-ready', 'owner-readback', 'source-readback', 'handover', ''):
                with self.subTest(failed=failed):
                    result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command],
                        env=dict(os.environ, FAIL_PHASE=failed, STUB_DIRECTORY=directory),
                        text=True, capture_output=True, timeout=5)
                    if failed:
                        self.assertNotEqual(result.returncode, 0)
                        self.assertNotIn('image-supply-chain-controllers', result.stdout)
                        # EXIT cleanup не вызывает kubectl scale после owner gate failure.
                        self.assertNotIn('unexpected-resume', result.stdout)
                    else:
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertIn('image-supply-chain-controllers', result.stdout)

    def test_supply_chain_source_and_image_readback_rejects_stale_rollout(self):
        source = SCRIPT.read_text()
        function = re.search(r'(?ms)^readback_local_supply_chain_deployment_inputs\(\) \{.*?^\}', source).group(0)
        template = {'metadata': {'annotations': {'kodex.dev/source-revision': 'a' * 40,
                    'kodex.dev/source-content-sha256': 'b' * 64}},
                    'spec': {'containers': [{'name': 'image-admission-controller', 'image': 'repo@sha256:' + 'c' * 64}]}}
        deployment = {'metadata': {'generation': 3}, 'spec': {'replicas': 1, 'template': template},
                      'status': {'observedGeneration': 3, 'updatedReplicas': 1, 'availableReplicas': 1, 'replicas': 1}}
        command = function + '''
fail() { printf '%s\\n' "$1" >&2; exit 1; }
yq() { printf '%s\\n' "$EXPECTED_TEMPLATE"; }
kubectl() { printf '%s\\n' "$ACTUAL_DEPLOYMENT"; }
namespace=kodex-system
render=synthetic
readback_local_supply_chain_deployment_inputs image-admission-controller
'''
        cases = [(deployment, None)]
        for mutate in ('revision', 'content', 'image', 'generation', 'old-replica'):
            actual = json.loads(json.dumps(deployment))
            if mutate in ('revision', 'content'):
                key = 'kodex.dev/source-' + ('revision' if mutate == 'revision' else 'content-sha256')
                actual['spec']['template']['metadata']['annotations'][key] = 'd' * (40 if mutate == 'revision' else 64)
            elif mutate == 'image': actual['spec']['template']['spec']['containers'][0]['image'] = 'old'
            elif mutate == 'generation': actual['status']['observedGeneration'] = 2
            else: actual['status']['replicas'] = 2
            cases.append((actual, 'readback'))
        for actual, error in cases:
            with self.subTest(error=error, actual=actual):
                result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command],
                    env=dict(os.environ, EXPECTED_TEMPLATE=json.dumps(template), ACTUAL_DEPLOYMENT=json.dumps(actual)),
                    text=True, capture_output=True, timeout=5)
                self.assertEqual(result.returncode == 0, error is None, result.stderr)
                if error: self.assertIn(error, result.stderr)

    def test_full_core_applies_the_synthetic_integration_fixture(self):
        source = SCRIPT.read_text()
        core = source[source.index('  if [[ "$stage" == core ]]'):]
        full_selection = core[core.index("apply_render core-applications"):]
        full_selection = full_selection[:full_selection.index("      '")]
        self.assertIn("integration-synthetic", full_selection)

    def test_image_admission_resume_readback_requires_exact_false(self):
        source = SCRIPT.read_text()
        function = re.search(r'(?ms)^readback_local_image_admission_controller_resume\(\) \{.*?^\}', source)
        self.assertIsNotNone(function, 'canonical resume readback is absent')
        key = 'IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS'
        valid = [{'name': key, 'value': 'false'}]
        command = function.group(0) + '''
fail() { printf '%s\\n' "$1" >&2; exit 1; }
yq() { printf '%s\\n' "$EXPECTED_ENV"; }
kubectl() { printf '%s\\n' "$ACTUAL_DEPLOYMENT"; }
namespace=kodex-system
render=synthetic
readback_local_image_admission_controller_resume
'''
        bad = [[], [{'name': key, 'value': 'true'}], [{'name': key, 'value': False}],
               [{'name': key, 'valueFrom': {'configMapKeyRef': {'name': 'foreign', 'key': 'pause'}}}],
               valid * 2, [{'name': key, 'value': 'false', 'valueFrom': {}}]]
        cases = [(valid, valid, True)] + [(valid, value, False) for value in bad] + [(value, valid, False) for value in bad]
        for expected, actual, success in cases:
            with self.subTest(expected=expected, actual=actual):
                deployment = {'spec': {'template': {'spec': {'containers': [{'name': 'image-admission-controller', 'env': actual}]}}}}
                result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command],
                    env=dict(os.environ, EXPECTED_ENV='\n'.join(json.dumps(e) for e in expected), ACTUAL_DEPLOYMENT=json.dumps(deployment)),
                    text=True, capture_output=True, timeout=5)
                self.assertEqual(result.returncode == 0, success, result.stderr)
                if not success: self.assertIn('resume', result.stderr)
        readback = re.search(r'(?ms)^readback_local_image_supply_chain\(\) \{.*?^\}', source).group(0)
        self.assertIn('readback_local_image_admission_controller_resume', readback)

    def test_stopped_pause_handover_has_exact_single_field_cas(self):
        source = SCRIPT.read_text()
        function = re.search(r'(?ms)^handover_local_image_admission_pause\(\) \{.*?^\}', source)
        self.assertIsNotNone(function)
        key = 'IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS'
        owner = {'manager': 'kubectl-patch', 'operation': 'Update', 'apiVersion': 'apps/v1', 'fieldsType': 'FieldsV1',
                 'fieldsV1': {'f:spec': {'f:template': {'f:spec': {'f:containers': {'k:{"name":"image-admission-controller"}': {
                     'f:env': {'k:{"name":"IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS"}': {'f:value': {}}}}}}}}}}
        controller = {'metadata': {'name': 'image-admission-controller', 'namespace': 'kodex-system',
            'uid': 'b0d061c8-a12f-4366-9413-cee2a8e774dd', 'resourceVersion': '550324',
            'labels': {'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload'}, 'managedFields': [owner]},
            'spec': {'replicas': 0, 'template': {'spec': {'containers': [{'name': 'image-admission-controller',
            'image': 'repo@sha256:' + 'a' * 64, 'env': [{'name': key, 'value': 'true'}]}]}}}, 'status': {'replicas': 0, 'availableReplicas': 0}}
        desired = {'name': 'image-admission-controller', 'image': controller['spec']['template']['spec']['containers'][0]['image'], 'env': [{'name': key, 'value': 'false'}]}
        command = function.group(0) + '''
fail() { printf '%s\\n' "$1" >&2; exit 1; }
readback_local_control_plane_image_policy() { [[ "$CASE" != owner-failure ]]; }
readback_local_supply_chain_deployment_inputs() { [[ "$CASE" != source-failure ]]; }
require_empty_local_image_admission_runs() { [[ "$CASE" != inventory ]]; }
yq() { printf '%s\\n' "$DESIRED"; }
PATCH_JSON=''
kubectl() {
 if [[ " $* " == *" patch "* ]]; then
   [[ " $* " == *" --field-manager=kodex-local-dev "* && " $* " != *" --force-conflicts "* ]] || return 1
   [[ "$CASE" != cas-failure ]] || return 1
   local previous='' argument
   for argument in "$@"; do [[ "$previous" != -p ]] || PATCH_JSON=$argument; previous=$argument; done
   ACTUAL_DEPLOYMENT=$(jq -c --argjson patch "$PATCH_JSON" '.spec.template.spec.containers[0].env[0].value=$patch[-1].value' <<<"$ACTUAL_DEPLOYMENT")
 else printf '%s\\n' "$ACTUAL_DEPLOYMENT"; fi
}
namespace=kodex-system
render=synthetic
security_profile=${PROFILE:-trusted-cluster}
context=${CONTEXT:-k3d-kodex}
mode=${MODE:-apply}
stage=${STAGE:-supply-chain}
handover_local_image_admission_pause
printf '%s\\n' "${PATCH_JSON:-null}"
'''
        cases = [('valid', controller, {}, True)]
        for name in ('foreign-manager', 'missing-managed-fields', 'duplicate-owner', 'replicas', 'ready', 'uid', 'image', 'duplicate-pause', 'valuefrom'):
            bad = json.loads(json.dumps(controller))
            if name == 'foreign-manager': bad['metadata']['managedFields'][0]['manager'] = 'foreign'
            elif name == 'missing-managed-fields': del bad['metadata']['managedFields']
            elif name == 'duplicate-owner': bad['metadata']['managedFields'] *= 2
            elif name == 'replicas': bad['spec']['replicas'] = 1
            elif name == 'ready': bad['status']['readyReplicas'] = 1
            elif name == 'uid': bad['metadata']['uid'] = ''
            elif name == 'image': bad['spec']['template']['spec']['containers'][0]['image'] = 'foreign'
            elif name == 'duplicate-pause': bad['spec']['template']['spec']['containers'][0]['env'] *= 2
            elif name == 'valuefrom': bad['spec']['template']['spec']['containers'][0]['env'][0]['valueFrom'] = {}
            cases.append((name, bad, {}, False))
        for name in ('owner-failure', 'source-failure', 'inventory', 'cas-failure'):
            cases.append((name, controller, {}, False))
        for options in ({'CONTEXT': 'foreign'}, {'MODE': 'readback'}, {'STAGE': 'core'}):
            cases.append(('profile', controller, options, False))
        canonical = json.loads(json.dumps(controller)); canonical['metadata']['managedFields'][0]['manager'] = 'kodex-local-dev'
        cases.append(('canonical-manager', canonical, {}, True))
        for name, current, options, success in cases:
            with self.subTest(case=name):
                result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command],
                    env=dict(os.environ, CASE=name, ACTUAL_DEPLOYMENT=json.dumps(current), DESIRED=json.dumps(desired), **options),
                    text=True, capture_output=True, timeout=5)
                self.assertEqual(result.returncode == 0, success, result.stderr)
                if success:
                    patch = json.loads(result.stdout)
                    self.assertEqual([p['op'] for p in patch], ['test', 'test', 'test', 'replace'])
                    self.assertEqual(patch[0]['value'], current['metadata']['uid'])
                    self.assertEqual(patch[1]['value'], current['metadata']['resourceVersion'])
                    self.assertEqual(patch[2], {'op': 'test', 'path': '/spec', 'value': current['spec']})
                    self.assertEqual(patch[3], {'op': 'replace', 'path': '/spec/template/spec/containers/0/env/0/value', 'value': 'false'})
        self.assertIn('handover_local_image_admission_pause\n      apply_render image-supply-chain-controllers', source)
        self.assertIn('handover_local_image_admission_pause\n  apply_render image-admission-workloads', source)


if __name__ == "__main__":
    unittest.main()
