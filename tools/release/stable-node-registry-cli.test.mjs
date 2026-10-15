import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { fingerprint } from "./stable-inventory.mjs";
import { nodeRegistryTLSFiles } from "./stable-node-registry-model.mjs";

const cli = fileURLToPath(new URL("./stable-node-registry.mjs", import.meta.url));
const mock = `#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto');
const a=process.argv.slice(2),dir=path.join(__dirname,'..'),p=path.join(dir,'state.json'),s=JSON.parse(fs.readFileSync(p,'utf8'));
const program=path.basename(process.argv[1]),hash=v=>crypto.createHash('sha256').update(v).digest('hex');
const save=()=>fs.writeFileSync(p,JSON.stringify(s));
fs.appendFileSync(path.join(dir,'trace'),JSON.stringify({program,args:a})+'\\n');
if(program==='git'){if(a[0]==='rev-parse')console.log((s.executorSHA??'f').repeat(40));if(a[0]==='diff'&&s.workerDrift)process.exit(1);process.exit(0);}
if(program==='yq'){console.log(JSON.stringify(JSON.parse(fs.readFileSync(0,'utf8'))));process.exit(0);}
if(program==='kubectl'){
 if(a.includes('exec')){const sql=fs.readFileSync(0,'utf8');console.log(JSON.stringify(sql.includes('stable_archive_idle')?
  {openArchiveTasks:0,transitionalStorage:0}:{at:new Date().toISOString(),openBuilds:0,pendingAdmissions:0,pendingPromotions:0,
  activeRuntimeRuns:0,claimedRuntimeLeases:0,promotedArtifactCount:3,promotedPinsSHA256:'d'.repeat(64)}));process.exit(0);}
 const i=a.indexOf('get'),kind=a[i+1],name=a[i+2];
 if(kind==='namespace')console.log(JSON.stringify({metadata:{uid:name==='kube-system'?'cluster':'namespace'}}));
 else if(kind==='nodes')console.log(JSON.stringify({items:s.nodes.map(n=>n.node)}));
 else if(kind==='node')console.log(JSON.stringify(s.nodes.find(n=>n.node.metadata.name===name).node));
 else if(kind==='jobs,pods')console.log('{"items":[]}');
 else if(kind==='--raw=/readyz')console.log('ok');else process.exit(4);process.exit(0);
}
const n=s.nodes.find(n=>n.container.Id===a[1]||n.node.metadata.name===a.at(-1));
if(!n)process.exit(5);
if(a[0]==='inspect'){console.log(JSON.stringify(n.container));process.exit(0);}
if(a[0]==='restart'){
 n.container.State.StartedAt=new Date(Date.now()+1000).toISOString();
 n.node.status.conditions[0].lastHeartbeatTime=n.container.State.StartedAt;
 n.hosts=JSON.parse(n.files['/etc/rancher/k3s/registries.yaml'].raw).mirrors['registry.local.kodex']?
 'server = "https://pull.example.invalid/v2"\\n':null;
 save();if(s.lostRestart)process.exit(9);console.log(n.container.Id);process.exit(0);
}
if(a[0]!=='exec')process.exit(6);
let b=a.slice(2);if(b[0]==='-i')process.exit(7);
if(a[1]==='-i'){throw Error('fixture parsing');}
`;

// docker exec -i помещает флаг перед id; отдельная нормализация сохраняет
// фактический argv, но не записывает stdin с credential sentinel в trace.
const dockerMock = mock.replace("const n=s.nodes.find", "if(a[0]==='exec'&&a[1]==='-i')a.splice(1,1);\nconst n=s.nodes.find") + `
const meta=f=>[f.uid,f.gid??s.uid,600,1,1,f.inode,Buffer.byteLength(f.raw)].join(':');
if(b[0]==='cat'){if(b[1].endsWith('hosts.toml')){if(!n.hosts)process.exit(8);process.stdout.write(n.hosts);}
 else process.stdout.write(n.files[b[1]].raw);process.exit(0);}
if(b[0]==='stat'){console.log(meta(n.files[b.at(-1)]));process.exit(0);}
if(b[0]==='sha256sum'){for(const x of b.slice(1))console.log('a'.repeat(64)+'  '+x);process.exit(0);}
if(b.join(' ')==='crictl --runtime-endpoint unix:///run/k3s/containerd/containerd.sock info'){console.log(JSON.stringify({status:{conditions:[{type:'RuntimeReady',status:true},{type:'NetworkReady',status:true}]}}));process.exit(0);}
if(b.includes('kodex-node-file')){const f=n.files[b.at(-1)];console.log(f?meta(f):'ABSENT');process.exit(0);}
if(b.includes('kodex-node-intent')){console.log(n.backup?'PRESENT':'ABSENT');process.exit(0);}
if(b.includes('kodex-node-hosts')){process.exit(n.hosts?9:0);}
if(b.includes('kodex-node-registry')){
 const i=b.indexOf('kodex-node-registry'),v=b.slice(i+1),mode=v[0];
 const reg='/etc/rancher/k3s/registries.yaml',cfg='/etc/rancher/k3s/config.yaml';
 if(mode==='install'){
  if(n.backup||hash(n.files[reg].raw)!==v[2])process.exit(10);
  n.backup=structuredClone(n.files);const input=fs.readFileSync(0,'utf8').trimEnd().split('\\n');
  n.files[reg]={raw:input[0]+'\\n',uid:Number(v[6]),inode:3};n.files[cfg]={raw:input[1]+'\\n',uid:Number(v[7]),gid:0,inode:4};save();
 }else{
  if(!n.backup||![v[2],v[3]].includes(hash(n.files[reg].raw)))process.exit(11);
  if(mode==='rollback'){n.files=structuredClone(n.backup);save();}
 }
 console.log('fixture complete');process.exit(0);
}
// Exact lock check, с отсутствующим lock в этой synthetic orchestration.
if(b[0]==='sh')process.exit(0);
process.exit(12);
`;

function setup() {
  const directory = mkdtempSync(join(tmpdir(), "kodex-node-cli-fixture-")); mkdirSync(join(directory, "bin"));
  for (const program of ["git", "kubectl", "yq", "docker"])
    writeFileSync(join(directory, "bin", program), program === "docker" ? dockerMock : mock, { mode: 0o700 });
  const registry = JSON.stringify({ mirrors: { "pull.example.invalid": { endpoint: ["https://pull.example.invalid"] } },
    configs: { "pull.example.invalid": { auth: { username: "SENTINEL_USER", password: "SENTINEL_PASSWORD_".repeat(4) },
      tls: { ca_file: nodeRegistryTLSFiles[0], cert_file: nodeRegistryTLSFiles[1], key_file: nodeRegistryTLSFiles[2] } } } }) + "\n";
  const nodes = ["agent", "server"].map((role, index) => {
    const name = `k3d-fixture-${role}-0`;
    return { node: { metadata: { name, uid: role + "-uid" }, status: { conditions: [{ type: "Ready", status: "True", lastHeartbeatTime: "2026-01-01T00:00:00Z" }] } },
      container: { Id: String(index + 1).repeat(64), Name: "/" + name, Image: "sha256:" + "a".repeat(64),
        Config: { Labels: { "k3d.cluster": "fixture", "k3d.role": role }, Cmd: [role], Env: ["K3S_TOKEN=SENTINEL_PRIVATE"] },
        State: { Running: true, StartedAt: "2026-01-01T00:00:00Z" } },
      files: { "/etc/rancher/k3s/registries.yaml": { raw: registry, uid: process.getuid(), inode: 1 } } };
  });
  writeFileSync(join(directory, "state.json"), JSON.stringify({ nodes, uid: process.getuid() }), { mode: 0o600 });
  const run = args => {
    try { return { status: 0, output: execFileSync(process.execPath, [cli, ...args], { env: { PATH: join(directory, "bin") + ":/usr/bin:/bin" },
      encoding: "utf8", timeout: 15000, stdio: ["ignore", "pipe", "pipe"] }) }; }
    catch (error) { return { status: error.status, output: String(error.stdout ?? "") + String(error.stderr ?? "") }; }
  };
  const planPath = join(directory, "plan.json");
  const planned = run(["plan", "--context", "k3d-fixture", "--physical-host", "pull.example.invalid", "--output", planPath]);
  assert.equal(planned.status, 0, planned.output);
  assert.equal(readFileSync(planPath, "utf8").includes("SENTINEL"), false);
  const plan = JSON.parse(readFileSync(planPath, "utf8"));
  const action = (mode, output, approval = fingerprint(plan), executor) => run([mode, "--context", "k3d-fixture", "--plan", planPath,
    "--output", join(directory, output), ...(mode === "readback" ? [] : ["--approved-plan-sha", approval]),
    ...(executor ? ["--approved-executor-sha", executor] : [])]);
  return { directory, action, state: () => JSON.parse(readFileSync(join(directory, "state.json"), "utf8")),
    change: value => writeFileSync(join(directory, "state.json"), JSON.stringify(value)),
    trace: () => readFileSync(join(directory, "trace"), "utf8").trim().split("\n").map(JSON.parse) };
}

test("actual CLI sequences two nodes and roundtrips readback rollback without credential evidence", () => {
  const f = setup();
  try {
    const applied = f.action("apply", "apply.jsonl"); assert.equal(applied.status, 0, applied.output);
    assert.equal(f.action("readback", "readback.json").status, 0);
    assert.equal(f.action("rollback", "rollback.jsonl").status, 0);
    assert.equal(f.state().nodes.every(n => !n.files["/etc/rancher/k3s/config.yaml"]), true);
    const restarts = f.trace().filter(t => t.program === "docker" && t.args[0] === "restart");
    assert.deepEqual(restarts.map(t => t.args[1][0]), ["1", "2", "1", "2"]);
    for (const name of ["plan.json", "apply.jsonl", "readback.json", "rollback.jsonl", "trace"])
      assert.equal(readFileSync(join(f.directory, name), "utf8").includes("SENTINEL"), false);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});
test("wrong approval and private file drift never install files or restart a node", () => {
  const f = setup();
  try {
    assert.notEqual(f.action("apply", "bad.jsonl", "0".repeat(64)).status, 0);
    const value = f.state(); value.nodes[0].files["/etc/rancher/k3s/registries.yaml"].raw += "\n"; f.change(value);
    assert.notEqual(f.action("apply", "drift.jsonl").status, 0);
    assert.equal(f.trace().some(t => t.program === "docker" && (t.args[0] === "restart" || t.args.includes("kodex-node-registry"))), false);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});
test("lost first restart stops the next node and partial rollback leaves untouched nodes alone", () => {
  const f = setup();
  try {
    const value = f.state(); value.lostRestart = true; f.change(value);
    assert.notEqual(f.action("apply", "apply.jsonl").status, 0);
    assert.equal(f.state().nodes[1].backup, undefined);
    const state = f.state(); state.lostRestart = false; f.change(state);
    assert.equal(f.action("rollback", "rollback.jsonl").status, 0);
    assert.deepEqual(f.trace().filter(t => t.program === "docker" && t.args[0] === "restart").map(t => t.args[1][0]), ["1", "1"]);
    assert.match(readFileSync(join(f.directory, "apply.jsonl"), "utf8"), /FAIL_OR_UNKNOWN/);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("repaired executor needs its own approval and unchanged install bytes for partial recovery", () => {
  const f = setup();
  try {
    const s = f.state(); s.lostRestart = true; f.change(s);
    assert.notEqual(f.action("apply", "apply.jsonl").status, 0);
    const changed = f.state(); changed.lostRestart = false; changed.executorSHA = "e"; f.change(changed);
    const count = () => f.trace().filter(t => t.program === "docker" && t.args[0] === "restart").length;
    assert.equal(count(), 1);
    assert.notEqual(f.action("rollback", "unapproved.jsonl").status, 0); assert.equal(count(), 1);
    changed.workerDrift = true; f.change(changed);
    assert.notEqual(f.action("rollback", "changed-worker.jsonl", undefined, "e".repeat(40)).status, 0); assert.equal(count(), 1);
    changed.workerDrift = false; f.change(changed);
    const recovery = f.action("rollback", "recovery.jsonl", undefined, "e".repeat(40));
    assert.equal(recovery.status, 0, recovery.output); assert.equal(count(), 2);
    assert.equal(f.action("apply", "forward.jsonl", undefined, "e".repeat(40)).status === 0, false);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});
