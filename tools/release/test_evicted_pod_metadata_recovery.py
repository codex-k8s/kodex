"""Герметичный полный recovery без настоящих Kubernetes/Docker вызовов."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

SCRIPT = Path(__file__).with_name("evicted-pod-metadata-recovery.py")
SPEC = importlib.util.spec_from_file_location("recovery", SCRIPT)
RECOVERY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RECOVERY)


def uid(number):
    return "10000000-0000-4000-8000-" + str(number).zfill(12)


def fixture():
    metadata = lambda name, number: {"name": name, "namespace": "kodex-system", "uid": uid(number), "resourceVersion": "7"}
    deployment = {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": metadata("control-plane", 3),
                  "spec": {"replicas": 3, "template": {"spec": {"containers": [{"name": "control-plane", "image": "unchanged"}]}}},
                  "status": {"observedGeneration": 1, "replicas": 3, "updatedReplicas": 3, "readyReplicas": 3, "availableReplicas": 3}}
    deployment["metadata"].update({"generation": 1, "labels": {"app.kubernetes.io/part-of": "kodex", "kodex.dev/profile": "web-only", "kodex.dev/environment": "production"}})
    replica = {"apiVersion": "apps/v1", "kind": "ReplicaSet", "metadata": metadata("old-cp-rs", 4)}
    replica["metadata"]["ownerReferences"] = [{"apiVersion": "apps/v1", "kind": "Deployment", "name": "control-plane", "uid": uid(3), "controller": True}]
    pods = {}
    for index in range(3):
        pod = {"apiVersion": "v1", "kind": "Pod", "metadata": metadata("old-cp-" + str(index), 10 + index),
               "spec": {"nodeName": "k3d-kodex-server-0", "containers": [{"name": "control-plane", "image": "unchanged"}]},
               "status": {"phase": "Failed", "reason": "Evicted"}}
        pod["metadata"]["ownerReferences"] = [{"apiVersion": "apps/v1", "kind": "ReplicaSet", "name": "old-cp-rs", "uid": uid(4), "controller": True}]
        if index == 2:
            pod["status"]["containerStatuses"] = [{"name": "control-plane", "state": {"terminated": {"exitCode": 0}}, "restartCount": 0}]
        pods[pod["metadata"]["name"]] = pod
    installation = {"apiVersion": "v1", "kind": "ConfigMap", "metadata": metadata("kodex-installation-profile", 5), "data": {"profile": "synthetic"}}
    target = {"schema": "kodex.portable-runner-target/v1", "context": "k3d-kodex", "clusterUID": uid(1), "namespaceUID": uid(2),
              "installationUID": uid(5), "installationSHA256": RECOVERY.fingerprint(installation["data"]),
              "resources": [{"apiVersion": "apps/v1", "kind": "Deployment", "name": "control-plane", "uid": uid(3)}]}
    nodes = []
    for index, name in enumerate(sorted(RECOVERY.ABSENCE.NODES)):
        nodes.append({"metadata": {"name": name, "uid": uid(20 + index)}, "status": {"nodeInfo": {"operatingSystem": "linux", "architecture": "amd64"},
                      "conditions": [{"type": "Ready", "status": "True"}], "addresses": [{"type": "InternalIP", "address": "172.18.0." + str(index + 2)}]}})
    return {"source": "a" * 40, "deployment": deployment, "replica": replica, "pods": pods, "installation": installation, "target": target, "nodes": nodes, "calls": [], "effects": []}


BACKEND = r'''#!PYTHON
import json,sys
from pathlib import Path
base=Path(__file__).resolve().parent.parent
path=base/'state.json';s=json.loads(path.read_text());a=sys.argv[1:];program=Path(sys.argv[0]).name
s['calls'].append({'program':program,'args':a})
def save():path.write_text(json.dumps(s))
def out(value):save();print(json.dumps(value));raise SystemExit(0)
def fail():save();raise SystemExit(9)
if program=='git':
 save()
 if a[0]=='rev-parse':print(s['source'])
 elif a[0]=='status':
  if s.get('dirty'):print(' M synthetic')
 else:raise SystemExit(8)
 raise SystemExit(0)
if program=='kubectl':
 if '--kubeconfig' not in a or a[a.index('--kubeconfig')+1]!=str(base/'kubeconfig') or '--context=k3d-kodex' not in a:fail()
 if 'get' in a:
  i=a.index('get');kind=a[i+1]
  if kind=='nodes':out({'items':s['nodes']})
  if kind=='replicasets,pods':
   rows=[s['replica'],*s['pods'].values(),*s.get('standalone',[])];save()
   print('\n'.join('\t'.join([r['kind'],r['metadata']['name'],r['metadata']['uid'],json.dumps(r['metadata']['ownerReferences']) if r['metadata'].get('ownerReferences') else '']) for r in rows));raise SystemExit(0)
  name=a[i+2]
  if kind=='namespace':out({'kind':'Namespace','metadata':{'name':name,'uid':s['target']['clusterUID'] if name=='kube-system' else s['target']['namespaceUID']}})
  if kind=='configmap':out(s['installation'])
  if kind=='deployment':out(s['deployment'])
  if kind=='replicaset':out(s['replica'])
  if kind=='pod':
   pod=s['pods'].get(name);save()
   if pod:print(json.dumps(pod))
   raise SystemExit(0)
  fail()
 if 'delete' in a:
  name=a[a.index('--raw')+1].split('/')[-1];body=json.loads(sys.stdin.read());pod=s['pods'].get(name)
  if not pod or a[a.index('--raw')+1]!='/api/v1/namespaces/kodex-system/pods/'+name:fail()
  if body!={'apiVersion':'v1','kind':'DeleteOptions','propagationPolicy':'Orphan','preconditions':{'uid':pod['metadata']['uid'],'resourceVersion':pod['metadata']['resourceVersion']}}:fail()
  if s.get('failBefore'):fail()
  s['effects'].append(name);del s['pods'][name];save()
  if s.get('lostACK'):raise SystemExit(9)
  out({'kind':'Status','status':'Success'})
 fail()
if program=='k3d':out([{'name':n['metadata']['name'],'role':'agent' if 'agent' in n['metadata']['name'] else 'server','runtimeLabels':{'k3d.cluster':'kodex'}} for n in s['nodes']])
if program=='docker':
 if a[0]=='context':
  save();print(s.get('dockerEndpoint','unix:///var/run/docker.sock'));raise SystemExit(0)
 if a[0]=='inspect':
  name=a[-1];index=sorted(n['metadata']['name'] for n in s['nodes']).index(name)
  out([str(index+1)*64,'/'+name,True,name,'kodex','172.18.0.'+str(index+2)])
 if a[0]=='exec':
  if a[1] not in ['1'*64,'2'*64]:fail()
  if a[2]=='ctr':
   save()
   if s.get('orphan') or s.get('orphanCount',0)>0:
    print('f'*64)
    if s.get('orphanCount',0)>0:s['orphanCount']-=1;save()
   raise SystemExit(0)
  if a[2]!='crictl':fail()
  if a[-3:]==['pods','-o','json']:
   rows=[]
   if s.get('targetPresent'):
    rows=[{'id':'c'*64,'metadata':{'uid':s['targetPresent'],'name':'old-cp-0','namespace':'kodex-system'},'state':'SANDBOX_NOTREADY'}]
   out({'items':rows})
  if a[-4:]==['ps','-a','-o','json']:out({'containers':[]})
 fail()
fail()
'''


class RecoveryTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="kodex-evicted-recovery-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.directory.chmod(0o700)
        self.state = fixture()
        self.write("state.json", self.state)
        self.write("target.json", self.state["target"])
        (self.directory / "kubeconfig").write_text("synthetic")
        (self.directory / "kubeconfig").chmod(0o600)
        binary = self.directory / "bin"
        binary.mkdir()
        for name in ("git", "kubectl", "docker", "k3d"):
            path = binary / name
            path.write_text(BACKEND.replace("#!PYTHON", "#!" + sys.executable))
            path.chmod(0o700)

    def write(self, name, value):
        path = self.directory / name
        path.write_text(json.dumps(value))
        path.chmod(0o600)

    def read(self, name="state.json"):
        return json.loads((self.directory / name).read_text())

    def edit(self, fn):
        state = self.read()
        fn(state)
        self.write("state.json", state)

    def run_cli(self, phase, extra=()):
        args = [sys.executable, str(SCRIPT), phase, "--context", "k3d-kodex", "--kubeconfig", str(self.directory / "kubeconfig"),
                "--plan", str(self.directory / "plan.json"), "--evidence", str(self.directory / "events.jsonl")]
        if phase == "plan":
            args += ["--target", str(self.directory / "target.json"), "--expected-count", "2"]
        if phase == "apply":
            args += ["--approved-plan-sha", RECOVERY.fingerprint(self.read("plan.json"))]
        env = {**os.environ, "PATH": str(self.directory / "bin") + ":" + os.environ["PATH"]}
        return subprocess.run([*args, *extra], encoding="utf8", capture_output=True, timeout=30, env=env)

    def passed(self, phase):
        result = self.run_cli(phase)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_real_cli_keeps_preimage_and_deletes_only_two_unknown_evicted_metadata(self):
        planned = self.passed("plan")
        self.assertEqual(planned["pods"], 2)
        plan = (self.directory / "plan.json").read_bytes()
        self.assertEqual(self.passed("apply")["status"], "COMPLETE")
        self.assertEqual(self.passed("status")["status"], "COMPLETE")
        self.assertEqual(self.passed("apply")["status"], "COMPLETE")
        state = self.read()
        self.assertEqual(set(state["effects"]), {"old-cp-0", "old-cp-1"})
        self.assertEqual(list(state["pods"]), ["old-cp-2"])
        self.assertEqual((self.directory / "plan.json").read_bytes(), plan)
        self.assertEqual(self.read("plan.json")["preimage"]["pods"][0]["metadata"]["resourceVersion"], "7")

    def test_fresh_intent_proof_is_durable_and_verifiable(self):
        self.passed("plan")
        self.passed("apply")
        events = [json.loads(line) for line in (self.directory / "events.jsonl").read_text().splitlines()]
        intents = [event for event in events if event["type"] == "INTENT"]
        self.assertEqual(len(intents), 2)
        for event in intents:
            self.assertEqual(RECOVERY.fingerprint(event["proof"]), event["proofSHA256"])
            self.assertEqual(len(event["proof"]["snapshots"]), 4)
            self.assertIn(event["uid"], event["proof"]["targetUIDs"])
            self.assertLessEqual(event["at"] - event["proof"]["at"], 10)

    def test_standalone_foreign_pod_with_empty_owner_projection_is_not_a_candidate(self):
        self.edit(lambda state: state.update(standalone=[{"kind": "Pod", "metadata": {"name": "foreign", "uid": uid(91)}}]))
        self.assertEqual(self.passed("plan")["pods"], 2)
        self.assertEqual(self.read()["effects"], [])

    def test_retry_rejects_incomplete_snapshot_then_requires_two_complete_snapshots(self):
        self.edit(lambda state: state.update(orphanCount=1))
        result = self.run_cli("plan", ["--absence-attempts", "2"])
        self.assertEqual(result.returncode, 0, result.stderr)
        proof = self.read("plan.json")["absence"]
        self.assertEqual(proof["attempt"], 2)
        self.assertEqual(len(proof["snapshots"]), 4)
        self.assertEqual(self.read()["effects"], [])

    def test_nonreading_child_stdin_cannot_block_bounded_runner(self):
        client = RECOVERY.ABSENCE.BoundedCommands(self.directory / "stdin-cache", kubeconfig=str(self.directory / "kubeconfig"), budget=1)
        client.deadline = time.monotonic() + 0.2
        started = time.monotonic()
        with self.assertRaises(ValueError):
            client([sys.executable, "-c", "import time;time.sleep(2)"], "x" * 65536)
        self.assertLess(time.monotonic() - started, 1)

    def test_orphan_or_target_runtime_never_proves_absence(self):
        for mutation in ({"orphan": True}, {"targetPresent": uid(10)}):
            with self.subTest(mutation=mutation):
                self.edit(lambda state: state.update(mutation))
                result = self.run_cli("plan", ["--absence-attempts", "3"])
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("RECOVERY_RUNTIME_ABSENCE_NOT_PROVEN", result.stderr)
                self.assertFalse((self.directory / "plan.json").exists())
                self.assertEqual(self.read()["effects"], [])
                self.edit(lambda state: (state.pop("orphan", None), state.pop("targetPresent", None)))

    def test_unknown_requires_separate_status_and_never_replays_completed_delete(self):
        self.passed("plan")
        self.edit(lambda state: state.update(lostACK=True))
        result = self.run_cli("apply")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RECOVERY_DELETE_OUTCOME_UNKNOWN", result.stderr)
        result = self.run_cli("apply")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RECOVERY_UNKNOWN_STATUS_REQUIRED", result.stderr)
        self.assertEqual(len(self.read()["effects"]), 1)
        self.passed("status")
        self.edit(lambda state: state.pop("lostACK"))
        self.assertEqual(self.passed("apply")["status"], "COMPLETE")
        self.assertEqual(len(self.read()["effects"]), 2)

    def test_unknown_before_effect_needs_status_before_same_uid_retry(self):
        self.passed("plan")
        self.edit(lambda state: state.update(failBefore=True))
        self.assertNotEqual(self.run_cli("apply").returncode, 0)
        self.assertEqual(self.read()["effects"], [])
        self.edit(lambda state: state.pop("failBefore"))
        self.assertNotEqual(self.run_cli("apply").returncode, 0)
        self.passed("status")
        self.assertEqual(self.passed("apply")["status"], "COMPLETE")

    def test_scope_source_uid_rv_lineage_and_runtime_drift_close_apply(self):
        self.passed("plan")
        original = self.read()
        changes = [lambda state: state.update(source="b" * 40), lambda state: state.update(dirty=True),
                   lambda state: state["target"].update(namespaceUID=uid(99)),
                   lambda state: state["pods"]["old-cp-0"]["metadata"].update(uid=uid(99)),
                   lambda state: state["pods"]["old-cp-0"]["metadata"].update(resourceVersion="8"),
                   lambda state: state["replica"]["metadata"]["ownerReferences"][0].update(uid=uid(99)),
                   lambda state: state.update(orphan=True), lambda state: state.update(targetPresent=uid(10)),
                   lambda state: state.update(dockerEndpoint="tcp://remote.invalid:2375")]
        for change in changes:
            with self.subTest(change=changes.index(change)):
                self.write("state.json", copy.deepcopy(original))
                self.edit(change)
                self.assertNotEqual(self.run_cli("apply").returncode, 0)
                self.assertEqual(self.read()["effects"], [])

    def test_missing_uid_without_owned_intent_cannot_become_completed(self):
        self.passed("plan")
        self.edit(lambda state: state["pods"].pop("old-cp-0"))
        result = self.run_cli("status")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RECOVERY_UNOWNED_ABSENCE", result.stderr)
        self.assertEqual(self.read()["effects"], [])

    def test_forged_plan_cannot_turn_name_into_raw_endpoint_or_include_running_pod(self):
        self.passed("plan")
        original = self.read("plan.json")
        for change in [lambda p: p["preimage"]["pods"][0]["metadata"].update(name="--raw=/secrets"),
                       lambda p: p["preimage"]["pods"][0]["status"].update(phase="Running"),
                       lambda p: p["preimage"]["pods"][0]["metadata"].update(namespace="foreign")]:
            plan = copy.deepcopy(original)
            change(plan)
            self.write("plan.json", plan)
            self.assertNotEqual(self.run_cli("apply").returncode, 0)
            self.assertEqual(self.read()["effects"], [])


if __name__ == "__main__":
    unittest.main()
