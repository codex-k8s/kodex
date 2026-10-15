import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync, readFileSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { fingerprint } from "./stable-inventory.mjs";
import { archiveTransition, sameArchiveBoundary, ownedDeploymentPods } from "./stable-archive-transition.mjs";

const cli = fileURLToPath(new URL("./stable-go-release.mjs", import.meta.url));
const mock = `#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),a=process.argv.slice(2),p=path.join(__dirname,'..','fixture.json');
const s=JSON.parse(fs.readFileSync(p,'utf8')),d=s.deployment;
fs.appendFileSync(path.join(__dirname,'..','trace'),JSON.stringify(a)+'\\n');
const i=a.indexOf('get');
if(i>=0){
  if(a[i+1]==='namespace')console.log(JSON.stringify({metadata:{uid:'cluster'}}));
  else if(a[i+1]==='deployment')console.log(JSON.stringify(d));
  else if(a[i+1]==='role')console.log(JSON.stringify(s.role));
  else if(a[i+1]==='rolebinding')console.log(JSON.stringify(s.binding));
  else if(a[i+1]==='jobs,pods')console.log(JSON.stringify({items:s.archiveWorkers?[{kind:'Job'}]:[]}));
  else if(a[i+1]==='replicasets,pods'){
    const digest=d.spec.template.spec.containers[0].image.split('@').pop();
    const draining=d.spec.replicas===0&&s.drainPolls>0;
    if(draining){s.drainPolls--;fs.writeFileSync(p,JSON.stringify(s));}
    const items=[{kind:'ReplicaSet',metadata:{uid:'rs',ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}},
      ...Array.from({length:draining?1:d.spec.replicas},(_,i)=>'pod-'+i).map(uid=>({kind:'Pod',metadata:{uid,ownerReferences:[{kind:'ReplicaSet',uid:'rs',controller:true}]},
        spec:d.spec.template.spec,status:{conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:d.metadata.name,ready:true,imageID:'fixture@'+digest}]}}))];
    console.log(JSON.stringify({items}));
  }else process.exit(4);
}else if(a.includes('exec')){
  const sql=fs.readFileSync(0,'utf8');if(!sql.includes('BEGIN TRANSACTION READ ONLY'))process.exit(5);
  if(sql.includes('stable_archive_idle')){console.log(JSON.stringify({openArchiveTasks:s.archiveBusy?1:0,transitionalStorage:0}));process.exit(0);}
  console.log(JSON.stringify({at:new Date().toISOString(),openBuilds:s.busy?1:0,pendingAdmissions:0,pendingPromotions:0,
    activeRuntimeRuns:0,claimedRuntimeLeases:0,promotedArtifactCount:3,promotedPinsSHA256:(s.pinsChanged?'e':'d').repeat(64)}));
}else if(a.includes('patch')){
  const patch=JSON.parse(fs.readFileSync(0,'utf8'));
  const kind=a[a.indexOf('patch')+1],target=kind==='role'?s.role:d;
  for(const op of patch){
    const keys=op.path.slice(1).split('/').map(x=>x.replaceAll('~1','/').replaceAll('~0','~'));
    const key=keys.pop();let parent=target;for(const k of keys)parent=parent[k];
    if(op.op==='test'){if(JSON.stringify(parent[key])!==JSON.stringify(op.value))process.exit(6);}
    else if(op.op==='replace'||op.op==='add')parent[key]=op.value;
    else if(op.op==='remove')delete parent[key];
    else process.exit(8);
  }
  if(a.includes('--dry-run=server')){console.log(JSON.stringify(d));process.exit(0);}
  target.metadata.resourceVersion=String(Number(target.metadata.resourceVersion)+1);
  if(kind==='deployment'){
    d.metadata.generation++;
    d.status={observedGeneration:d.metadata.generation,replicas:d.spec.replicas,readyReplicas:d.spec.replicas,
      availableReplicas:d.spec.replicas,updatedReplicas:d.spec.replicas};
    if(s.driftAfterQuiesce&&d.spec.replicas===0)s.binding.metadata.uid='replaced-binding';
  }
  fs.writeFileSync(p,JSON.stringify(s));
  if(s.lostQuiesceResponse&&kind==='deployment'&&d.spec.replicas===0)process.exit(9);
  if(s.lostRoleResponse&&kind==='role')process.exit(9);
  if(s.lostRestoreResponse&&kind==='deployment'&&d.spec.template.spec.containers[0].command[0].includes('run-go-hot-reload'))process.exit(9);
  console.log(kind+'/session-archive');
}else if(a.includes('rollout'))console.log('complete');else process.exit(7);
`;

function setup(name = "session-archive") {
  const directory = mkdtempSync(join(tmpdir(), "kodex-stable-cli-test-"));
  mkdirSync(join(directory, "bin"));
  writeFileSync(join(directory, "bin", "kubectl"), mock, { mode: 0o700 });
  writeFileSync(join(directory, "bin", "git"), `#!${process.execPath}\nprocess.stdout.write(process.argv[2]==='rev-parse'?'${"f".repeat(40)}':'');\n`, { mode: 0o700 });
  const deployment = { apiVersion: "apps/v1", kind: "Deployment",
    metadata: { name, namespace: "kodex-system", uid: "deployment", resourceVersion: "1", generation: 1 },
    spec: { replicas: 1, template: { metadata: {}, spec: {
      containers: [{ name, image: "dev", command: ["/workspace/tools/dev/run-go-hot-reload.sh"],
        args: [name === "session-archive" ? "services/jobs/session-archive" : "services/internal/control-plane", `./cmd/${name}`, name, "controller"],
        env: [{ name: "PRIVATE", value: "CLI_PRIVATE_SENTINEL" },
          { name: "SESSION_ARCHIVE_WORKER_IMAGE", value: "example.invalid/kodex/session-archive@sha256:" + "e".repeat(64) }], volumeMounts: [] }], volumes: [] } } },
    status: { observedGeneration: 1, replicas: 1, readyReplicas: 1, availableReplicas: 1, updatedReplicas: 1 } };
  const desiredRole = JSON.parse(execFileSync("yq", ["eval-all", "-o=json", "-I=0",
    'select(.kind == "Role" and .metadata.name == "session-archive-controller")',
    fileURLToPath(new URL("../../deploy/k8s/base/session-archive/runtime/rbac.yaml", import.meta.url))], { encoding: "utf8" }));
  const role = { ...desiredRole, metadata: { ...desiredRole.metadata, uid: "archive-role", resourceVersion: "1" } };
  role.rules.find(r => r.resources[0] === "configmaps").verbs = ["create", "delete", "deletecollection"];
  role.rules.find(r => r.resources[0] === "jobs").verbs.push("deletecollection");
  const binding = { apiVersion: role.apiVersion, kind: "RoleBinding",
    metadata: { name: role.metadata.name, namespace: role.metadata.namespace, uid: "archive-binding", resourceVersion: "1" },
    roleRef: { apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: role.metadata.name },
    subjects: [{ kind: "ServiceAccount", name: "session-archive", namespace: "kodex-system" }] };
  writeFileSync(join(directory, "fixture.json"), JSON.stringify({ deployment, role, binding }), { mode: 0o600 });
  const release = { component: name, sourceSHA: "a".repeat(40), provenanceSHA256: "b".repeat(64),
    image: "example.invalid/kodex/session-archive@sha256:" + "c".repeat(64) };
  writeFileSync(join(directory, "manifest.json"), JSON.stringify({ schema: "kodex.stable-go-manifest/v1",
    targets: [{ name, release }] }), { mode: 0o600 });
  return { directory, run: args => {
    try { return { status: 0, output: execFileSync(process.execPath, [cli, ...args], {
      env: { PATH: join(directory, "bin") + ":" + process.env.PATH, FIXTURE: join(directory, "fixture.json"), TRACE: join(directory, "trace") },
      timeout: 10000, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }) }; }
    catch (error) { return { status: error.status, output: (error.stdout ?? "") + (error.stderr ?? "") }; }
  } };
}

const writes = directory => readFileSync(join(directory, "trace"), "utf8").trim().split("\n").map(JSON.parse)
  .filter(args => args.includes("patch") && !args.includes("--dry-run=server"));

test("private plan never persists raw spec and exact approved cutover proves two serving pods", () => {
  const { directory, run } = setup();
  try {
    const planPath = join(directory, "plan.json"), applyPath = join(directory, "apply.jsonl");
    const result = run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]);
    assert.equal(result.status, 0, result.output);
    const encoded = readFileSync(planPath, "utf8"), plan = JSON.parse(encoded);
    assert.equal(encoded.includes("CLI_PRIVATE_SENTINEL"), false);
    const applied = run(["apply", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan), "--output", applyPath]);
    assert.equal(applied.status, 0, applied.output);
    assert.equal(applied.output.includes("CLI_PRIVATE_SENTINEL"), false);
    assert.equal(JSON.parse(readFileSync(applyPath, "utf8").trim().split("\n").at(-1)).status, "PASS");
    const journal = readFileSync(applyPath, "utf8").trim().split("\n").map(line => JSON.parse(line).status);
    assert.deepEqual(journal, ["IN_PROGRESS", "QUIESCE_ATTEMPT", "QUIESCED", "ROLE_PATCH_ATTEMPT", "PATCH_ATTEMPT", "PASS", "PASS"]);
    const state = JSON.parse(readFileSync(join(directory, "fixture.json"), "utf8"));
    assert.equal(state.role.rules.some(r => r.verbs.includes("deletecollection")), false);
    assert.equal(state.deployment.spec.template.spec.containers[0].env.find(e => e.name === "SESSION_ARCHIVE_WORKER_IMAGE").value,
      state.deployment.spec.template.spec.containers[0].image);
    const readback = run(["readback", "--context", "fixture", "--plan", planPath, "--output", join(directory, "readback.json")]);
    assert.equal(readback.status, 0, readback.output);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("archive role changes only after all old controller pods disappear", () => {
  const { directory, run } = setup();
  try {
    const planPath = join(directory, "plan.json"), statePath = join(directory, "fixture.json");
    assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
    const plan = JSON.parse(readFileSync(planPath, "utf8")), state = JSON.parse(readFileSync(statePath, "utf8"));
    state.drainPolls = 2; writeFileSync(statePath, JSON.stringify(state));
    const applied = run(["apply", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan),
      "--output", join(directory, "apply.jsonl")]);
    assert.equal(applied.status, 0, applied.output);
    const trace = readFileSync(join(directory, "trace"), "utf8").trim().split("\n").map(JSON.parse);
    const index = trace.findIndex(args => args.includes("patch") && args[args.indexOf("patch") + 1] === "role");
    assert.ok(index > 0);
    assert.equal(trace.slice(0, index).filter(args => args.includes("replicasets,pods")).length, 3);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("archive binding drift stops before any patch or before role activation", () => {
  for (const scenario of ["before", "after quiesce"]) {
    const { directory, run } = setup();
    try {
      const planPath = join(directory, "plan.json");
      assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
      const plan = JSON.parse(readFileSync(planPath, "utf8"));
      const path = join(directory, "fixture.json"), state = JSON.parse(readFileSync(path, "utf8"));
      if (scenario === "before") state.binding.subjects[0].name = "foreign";
      else state.driftAfterQuiesce = true;
      writeFileSync(path, JSON.stringify(state));
      const result = run(["apply", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan),
        "--output", join(directory, "apply.jsonl")]);
      assert.notEqual(result.status, 0);
      const trace = readFileSync(join(directory, "trace"), "utf8").trim().split("\n").map(JSON.parse);
      const patches = writes(directory);
      assert.equal(patches.length, scenario === "before" ? 0 : 1);
      const current = JSON.parse(readFileSync(path, "utf8"));
      assert.equal(current.role.rules.some(r => r.verbs.includes("deletecollection")), true);
      if (scenario !== "before") assert.equal(current.deployment.spec.replicas, 0);
    } finally { rmSync(directory, { recursive: true, force: true }); }
  }
});

test("unknown quiesce or role response is never retried and never starts compiled pods", () => {
  for (const flag of ["lostQuiesceResponse", "lostRoleResponse"]) {
    const { directory, run } = setup();
    try {
      const planPath = join(directory, "plan.json");
      assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
      const plan = JSON.parse(readFileSync(planPath, "utf8"));
      const path = join(directory, "fixture.json"), state = JSON.parse(readFileSync(path, "utf8"));
      state[flag] = true; writeFileSync(path, JSON.stringify(state));
      const args = ["apply", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan)];
      assert.notEqual(run([...args, "--output", join(directory, "apply.jsonl")]).status, 0);
      assert.equal(JSON.parse(readFileSync(path, "utf8")).deployment.spec.replicas, 0);
      const observed = run(["inspect", "--context", "fixture", "--plan", planPath, "--output", join(directory, "inspect.json")]);
      assert.equal(observed.status, 0, observed.output);
      const observation = JSON.parse(observed.output);
      assert.equal(observation.status, "OBSERVED");
      assert.equal(observation.results[0].complete, false);
      assert.equal(observation.results[0].phase, flag === "lostQuiesceResponse" ? "QUIESCED" : "QUIESCED_ROLE_READY");
      const trace = () => readFileSync(join(directory, "trace"), "utf8").trim().split("\n").map(JSON.parse);
      const before = writes(directory).length;
      assert.equal(before, flag === "lostQuiesceResponse" ? 1 : 2);
      assert.notEqual(run([...args, "--output", join(directory, "retry.jsonl")]).status, 0);
      assert.equal(writes(directory).length, before);
      assert.match(readFileSync(join(directory, "apply.jsonl"), "utf8"), /FAIL_OR_UNKNOWN/);
    } finally { rmSync(directory, { recursive: true, force: true }); }
  }
});

test("archive transition rejects wildcard permissions foreign subjects and namespace drift", () => {
  const { directory } = setup();
  try {
    const { role, binding } = JSON.parse(readFileSync(join(directory, "fixture.json"), "utf8"));
    const desiredRole = structuredClone(role);
    desiredRole.rules.forEach(r => { r.verbs = r.verbs.filter(v => v !== "deletecollection"); });
    desiredRole.rules.find(r => r.resources[0] === "configmaps").verbs.unshift("get");
    const original = archiveTransition(role, binding, desiredRole);
    assert.equal(sameArchiveBoundary(original.proof, original.proof), true);
    const newRole = { ...role, rules: desiredRole.rules };
    assert.equal(sameArchiveBoundary(archiveTransition(newRole, binding, desiredRole).proof, original.proof, "after"), true);
    for (const mutation of [
      (r, b) => { b.subjects[0].namespace = "foreign"; },
      (r, b) => { b.roleRef.kind = "ClusterRole"; },
      r => { r.rules[0].verbs = ["*"]; },
      r => { r.rules[0].resources = ["secrets"]; },
      r => { r.metadata.namespace = "kodex-system"; },
    ]) {
      const source = structuredClone(role), target = structuredClone(binding);
      mutation(source, target);
      assert.throws(() => archiveTransition(source, target, desiredRole), /STABLE_ARCHIVE_/);
    }
    const foreign = structuredClone(original.proof); foreign.roleUID = "replacement-role";
    assert.equal(sameArchiveBoundary(foreign, original.proof), false);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("serving pod lineage does not hide foreign pods behind valid replicas", () => {
  const list = { items: [
    { kind: "ReplicaSet", metadata: { uid: "rs", ownerReferences: [{ kind: "Deployment", uid: "dep", controller: true }] } },
    { kind: "Pod", metadata: { uid: "pod", ownerReferences: [{ kind: "ReplicaSet", uid: "rs", controller: true }] } },
  ] };
  assert.equal(ownedDeploymentPods(list, "dep").length, 1);
  list.items.push({ kind: "Pod", metadata: { uid: "foreign", ownerReferences: [{ kind: "ReplicaSet", uid: "foreign-rs", controller: true }] } });
  assert.throws(() => ownedDeploymentPods(list, "dep"), /FOREIGN_SERVING_POD/);
});

test("old Go plan schema is rejected without compatibility decoder or writes", () => {
  const { directory, run } = setup();
  try {
    const planPath = join(directory, "plan.json");
    assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    plan.schema = "kodex.stable-go-plan/v1"; writeFileSync(planPath, JSON.stringify(plan));
    const result = run(["inspect", "--context", "fixture", "--plan", planPath, "--output", join(directory, "inspect.json")]);
    assert.notEqual(result.status, 0);
    assert.match(result.output, /STABLE_PLAN_IDENTITY_MISMATCH/);
    assert.equal(writes(directory).length, 0);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("archive tasks or surviving workers block cutover before any patch", () => {
  for (const flag of ["archiveBusy", "archiveWorkers"]) {
    const { directory, run } = setup();
    try {
      const planPath = join(directory, "plan.json");
      assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
      const plan = JSON.parse(readFileSync(planPath, "utf8"));
      const path = join(directory, "fixture.json"), state = JSON.parse(readFileSync(path, "utf8"));
      state[flag] = true; writeFileSync(path, JSON.stringify(state));
      const applied = run(["apply", "--context", "fixture", "--plan", planPath, "--output", join(directory, "apply.jsonl"),
        "--approved-plan-sha", fingerprint(plan)]);
      assert.notEqual(applied.status, 0);
      assert.match(applied.output, /STABLE_ARCHIVE_(NOT_IDLE|WORKERS_PRESENT)/);
      assert.equal(writes(directory).length, 0);
    } finally { rmSync(directory, { recursive: true, force: true }); }
  }
});

test("wrong confirmation and busy owner state never perform a patch", () => {
  const { directory, run } = setup();
  try {
    const planPath = join(directory, "plan.json");
    assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    const base = ["apply", "--context", "fixture", "--plan", planPath, "--output", join(directory, "apply.jsonl"), "--approved-plan-sha"];
    assert.notEqual(run([...base, "0".repeat(64)]).status, 0);
    const path = join(directory, "fixture.json"), state = JSON.parse(readFileSync(path, "utf8"));
    state.busy = true; writeFileSync(path, JSON.stringify(state));
    assert.notEqual(run([...base, fingerprint(plan)]).status, 0);
    assert.equal(writes(directory).length, 0);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("Go rollback restores exact source spec labels replicas and preserves private env", () => {
  const { directory, run } = setup("control-plane");
  try {
    const path = join(directory, "fixture.json"), original = JSON.parse(readFileSync(path, "utf8")).deployment;
    const planPath = join(directory, "plan.json"), applyPath = join(directory, "apply.jsonl");
    const planned = run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]);
    assert.equal(planned.status, 0, planned.output);
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    assert.equal(run(["apply", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan), "--output", applyPath]).status, 0);
    const result = run(["rollback", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan),
      "--journal", applyPath, "--output", join(directory, "rollback.jsonl")]);
    assert.equal(result.status, 0, result.output);
    const current = JSON.parse(readFileSync(path, "utf8")).deployment;
    assert.deepEqual(current.spec, original.spec);
    assert.equal(Object.hasOwn(current.metadata, "labels"), false);
    assert.equal(JSON.parse(result.output).releaseComplete, false);
    assert.equal(readFileSync(join(directory, "rollback.jsonl"), "utf8").includes("CLI_PRIVATE_SENTINEL"), false);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test("archive recovery from partial or complete cutover never regrants bulk deletion or starts unsafe source", () => {
  for (const flag of [null, "lostQuiesceResponse", "lostRoleResponse"]) {
    const { directory, run } = setup();
    try {
      const path = join(directory, "fixture.json"), planPath = join(directory, "plan.json"), applyPath = join(directory, "apply.jsonl");
      assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
      const plan = JSON.parse(readFileSync(planPath, "utf8"));
      const before = JSON.parse(readFileSync(path, "utf8"));
      if (flag) { before[flag] = true; writeFileSync(path, JSON.stringify(before)); }
      const applied = run(["apply", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan), "--output", applyPath]);
      assert.equal(applied.status === 0, !flag, applied.output);
      const state = JSON.parse(readFileSync(path, "utf8")); if (flag) state[flag] = false; writeFileSync(path, JSON.stringify(state));
      const recovered = run(["rollback", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan),
        "--journal", applyPath, "--output", join(directory, "rollback.jsonl")]);
      assert.equal(recovered.status, 0, recovered.output);
      const after = JSON.parse(readFileSync(path, "utf8"));
      assert.equal(after.deployment.spec.replicas, 0);
      assert.equal(after.role.rules.some(r => r.verbs.includes("deletecollection")), false);
      assert.equal(JSON.parse(recovered.output).archivePaused, true);
      const inspected = run(["inspect", "--context", "fixture", "--plan", planPath, "--output", join(directory, "inspect.json")]);
      assert.equal(inspected.status, 0, inspected.output);
      assert.equal(JSON.parse(inspected.output).results[0].phase, "QUIESCED_ROLE_READY");
      // Возврат в две реплики требует свежего reviewed плана из safe paused
      // source; прежний план не переотправляется как неизвестный apply.
      const nextPlan = join(directory, "forward.json");
      assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", nextPlan]).status, 0);
      const fresh = JSON.parse(readFileSync(nextPlan, "utf8"));
      const resumed = run(["apply", "--context", "fixture", "--plan", nextPlan, "--approved-plan-sha", fingerprint(fresh), "--output", join(directory, "forward.jsonl")]);
      assert.equal(resumed.status, 0, resumed.output);
      assert.equal(JSON.parse(readFileSync(path, "utf8")).deployment.spec.replicas, 2);
    } finally { rmSync(directory, { recursive: true, force: true }); }
  }
});

test("recovery refuses foreign deployment role journal or promoted pins before writes", () => {
  for (const scenario of ["uid", "binding", "journal", "pins"]) {
    const { directory, run } = setup();
    try {
      const path = join(directory, "fixture.json"), planPath = join(directory, "plan.json"), applyPath = join(directory, "apply.jsonl");
      assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
      const plan = JSON.parse(readFileSync(planPath, "utf8"));
      assert.equal(run(["apply", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan), "--output", applyPath]).status, 0);
      const state = JSON.parse(readFileSync(path, "utf8"));
      if (scenario === "uid") state.deployment.metadata.uid = "foreign";
      if (scenario === "binding") state.binding.metadata.uid = "foreign";
      if (scenario === "pins") state.pinsChanged = true;
      writeFileSync(path, JSON.stringify(state));
      if (scenario === "journal") {
        const rows = readFileSync(applyPath, "utf8").trim().split("\n").map(JSON.parse); rows[0].planSHA256 = "0".repeat(64);
        writeFileSync(applyPath, rows.map(JSON.stringify).join("\n") + "\n");
      }
      const patches = () => writes(directory).length;
      const count = patches();
      assert.notEqual(run(["rollback", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan),
        "--journal", applyPath, "--output", join(directory, "rollback.jsonl")]).status, 0);
      assert.equal(patches(), count);
    } finally { rmSync(directory, { recursive: true, force: true }); }
  }
});

test("lost restore response is resolved by observed source without repeating the patch", () => {
  const { directory, run } = setup("control-plane");
  try {
    const path = join(directory, "fixture.json"), planPath = join(directory, "plan.json"), applyPath = join(directory, "apply.jsonl");
    assert.equal(run(["plan", "--context", "fixture", "--manifest", join(directory, "manifest.json"), "--output", planPath]).status, 0);
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    assert.equal(run(["apply", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan), "--output", applyPath]).status, 0);
    const state = JSON.parse(readFileSync(path, "utf8")); state.lostRestoreResponse = true; writeFileSync(path, JSON.stringify(state));
    const args = ["rollback", "--context", "fixture", "--plan", planPath, "--approved-plan-sha", fingerprint(plan), "--journal", applyPath];
    assert.notEqual(run([...args, "--output", join(directory, "unknown.jsonl")]).status, 0);
    const patches = () => writes(directory).length;
    const count = patches(), observed = run([...args, "--output", join(directory, "observed.jsonl")]);
    assert.equal(observed.status, 0, observed.output); assert.equal(patches(), count);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
