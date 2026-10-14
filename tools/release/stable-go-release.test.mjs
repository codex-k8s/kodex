import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync, readFileSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { fingerprint } from "./stable-inventory.mjs";

const cli = fileURLToPath(new URL("./stable-go-release.mjs", import.meta.url));
const mock = `#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),a=process.argv.slice(2),p=path.join(__dirname,'..','fixture.json');
const s=JSON.parse(fs.readFileSync(p,'utf8')),d=s.deployment;
fs.appendFileSync(path.join(__dirname,'..','trace'),JSON.stringify(a)+'\\n');
const i=a.indexOf('get');
if(i>=0){
  if(a[i+1]==='namespace')console.log(JSON.stringify({metadata:{uid:'cluster'}}));
  else if(a[i+1]==='deployment')console.log(JSON.stringify(d));
  else if(a[i+1]==='replicasets,pods'){
    const digest=d.spec.template.spec.containers[0].image.split('@').pop();
    const items=[{kind:'ReplicaSet',metadata:{uid:'rs',ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}},
      ...['pod-a','pod-b'].map(uid=>({kind:'Pod',metadata:{uid,ownerReferences:[{kind:'ReplicaSet',uid:'rs',controller:true}]},
        spec:d.spec.template.spec,status:{conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:d.metadata.name,ready:true,imageID:'fixture@'+digest}]}}))];
    console.log(JSON.stringify({items}));
  }else process.exit(4);
}else if(a.includes('exec')){
  const sql=fs.readFileSync(0,'utf8');if(!sql.includes('BEGIN TRANSACTION READ ONLY'))process.exit(5);
  console.log(JSON.stringify({at:new Date().toISOString(),openBuilds:s.busy?1:0,pendingAdmissions:0,pendingPromotions:0,
    activeRuntimeRuns:0,claimedRuntimeLeases:0,promotedArtifactCount:3,promotedPinsSHA256:'d'.repeat(64)}));
}else if(a.includes('patch')){
  const patch=JSON.parse(fs.readFileSync(0,'utf8'));
  if(patch[0].value!==d.metadata.uid||patch[1].value!==d.metadata.resourceVersion)process.exit(6);
  d.spec=patch.at(-1).value;d.metadata.labels=patch.at(-2).value;
  d.metadata.generation=2;d.status={observedGeneration:2,replicas:2,readyReplicas:2,availableReplicas:2,updatedReplicas:2};
  fs.writeFileSync(p,JSON.stringify(s));console.log('deployment.apps/session-archive');
}else if(a.includes('rollout'))console.log('complete');else process.exit(7);
`;

function setup() {
  const directory = mkdtempSync(join(tmpdir(), "kodex-stable-cli-test-"));
  mkdirSync(join(directory, "bin"));
  writeFileSync(join(directory, "bin", "kubectl"), mock, { mode: 0o700 });
  const deployment = { apiVersion: "apps/v1", kind: "Deployment",
    metadata: { name: "session-archive", namespace: "kodex-system", uid: "deployment", resourceVersion: "1", generation: 1 },
    spec: { replicas: 1, template: { metadata: {}, spec: {
      containers: [{ name: "session-archive", image: "dev", command: ["/workspace/tools/dev/run-go-hot-reload.sh"],
        args: ["services/jobs/session-archive", "./cmd/session-archive", "session-archive", "controller"],
        env: [{ name: "PRIVATE", value: "CLI_PRIVATE_SENTINEL" }], volumeMounts: [] }], volumes: [] } } },
    status: { observedGeneration: 1, replicas: 1, readyReplicas: 1 } };
  writeFileSync(join(directory, "fixture.json"), JSON.stringify({ deployment }), { mode: 0o600 });
  const release = { component: "session-archive", sourceSHA: "a".repeat(40), provenanceSHA256: "b".repeat(64),
    image: "example.invalid/kodex/session-archive@sha256:" + "c".repeat(64) };
  writeFileSync(join(directory, "manifest.json"), JSON.stringify({ schema: "kodex.stable-go-manifest/v1",
    targets: [{ name: "session-archive", release }] }), { mode: 0o600 });
  return { directory, run: args => {
    try { return { status: 0, output: execFileSync(process.execPath, [cli, ...args], {
      env: { PATH: join(directory, "bin") + ":/usr/bin:/bin", FIXTURE: join(directory, "fixture.json"), TRACE: join(directory, "trace") },
      timeout: 10000, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }) }; }
    catch (error) { return { status: error.status, output: (error.stdout ?? "") + (error.stderr ?? "") }; }
  } };
}

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
    const readback = run(["readback", "--context", "fixture", "--plan", planPath, "--output", join(directory, "readback.json")]);
    assert.equal(readback.status, 0, readback.output);
  } finally { rmSync(directory, { recursive: true, force: true }); }
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
    assert.equal(readFileSync(join(directory, "trace"), "utf8").includes('"patch"'), false);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
