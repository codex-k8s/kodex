import assert from "node:assert/strict";
import { execFile, execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import test from "node:test";
import { renderPublicEdge } from "./edge-model.mjs";
import { protectedCommitment, publicCAProjection } from "./edge-plan.mjs";
import { matchesDesired, ownedResourceMatches, ownedResourcePath, resourceIntent } from "./edge-release.mjs";
import { fingerprint } from "../../tools/release/stable-inventory.mjs";

const execute = promisify(execFile);
const config = { platformHost: "platform.example.test", oidcHost: "sso.example.test", namespace: "kube-system",
  bridgeIP: "172.20.0.1", allowedPeers: ["172.20.0.2", "172.20.0.3"], backendIP: "127.0.0.2", relayPorts: [18443, 18444],
  upstreamServerName: "upstream.example.test", certResolver: "public" };

test("owned resource matching rejects extra routing authority and binds exact delete paths", () => {
  for (const resource of renderPublicEdge(config).resources) {
    const desired = resourceIntent(resource, "a".repeat(64)), current = structuredClone(desired);
    current.metadata.uid = "fixture"; current.metadata.resourceVersion = "1";
    assert.equal(ownedResourceMatches(current, desired), true);
    assert.equal(ownedResourcePath(resource).includes("/namespaces/kube-system/"), true);
    current.metadata.annotations["kodex.dev/public-edge-plan"] = "b".repeat(64);
    assert.equal(ownedResourceMatches(current, desired), false);
  }
  const desired = resourceIntent(renderPublicEdge(config).resources[0], "a".repeat(64));
  const defaults = structuredClone(desired);
  Object.assign(defaults.spec, { clusterIP: "10.43.0.10", clusterIPs: ["10.43.0.10"], ipFamilies: ["IPv4"],
    type: "ClusterIP", sessionAffinity: "None", internalTrafficPolicy: "Cluster", ipFamilyPolicy: "SingleStack" });
  assert.equal(ownedResourceMatches(defaults, desired), true);
  defaults.spec.selector = { app: "foreign" };
  assert.equal(ownedResourceMatches(defaults, desired), false);
  delete defaults.spec.selector; defaults.spec.type = "ExternalName";
  assert.equal(ownedResourceMatches(defaults, desired), false);
  const extra = structuredClone(desired); extra.metadata.finalizers = ["foreign"];
  assert.equal(ownedResourceMatches(extra, desired), false);
  assert.throws(() => ownedResourcePath({ ...desired, metadata: { ...desired.metadata, name: "../../foreign" } }));
  assert.equal(matchesDesired({ values: [1, 2] }, { values: [1] }), false);
});

const mock = `#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),a=process.argv.slice(2),base=path.join(__dirname,'..');
const fixture=path.join(base,'fixture.json'),s=JSON.parse(fs.readFileSync(fixture,'utf8'));
const save=()=>fs.writeFileSync(fixture,JSON.stringify(s));
fs.appendFileSync(path.join(base,'trace'),JSON.stringify({program:path.basename(process.argv[1]),args:a})+'\\n');
const unit=p=>{if(!/^\\/etc\\/systemd\\/system\\/kodex-public-edge-https-[01]\\.(socket|service)$/.test(p))process.exit(12);return path.join(base,'units',path.basename(p));};
const systemctl=argv=>{
 if(argv[0]==='show')console.log(fs.existsSync(unit('/etc/systemd/system/'+argv[1]))?'loaded':'not-found');
 else if(argv[0]==='enable'){s.enabled.push(argv.at(-1));save();}
 else if(argv[0]==='start'){s.stopped=(s.stopped||[]).filter(n=>n!==argv[1]);save();}
 else if(argv[0]==='disable'){s.enabled=s.enabled.filter(n=>n!==argv.at(-1));save();}
 else if(argv[0]==='is-active'||argv[0]==='is-enabled'){if(!s.enabled.includes(argv[1])||(argv[0]==='is-active'&&(s.stopped||[]).includes(argv[1])))process.exit(3);console.log(argv[0]==='is-active'?'active':'enabled');}
 else if(!['stop','daemon-reload'].includes(argv[0]))process.exit(13);
};
const program=path.basename(process.argv[1]);
if(program==='docker'){
 if(a[0]!=='inspect')process.exit(4);const index=Number(a[1].slice(-1));
 console.log(JSON.stringify([{Id:String(index+1).repeat(64),State:{Running:true},
 NetworkSettings:{Networks:{fixture:{IPAddress:'172.20.0.'+(index+2)}}}}]));
}else if(program==='systemctl')systemctl(a);
else if(program==='sudo'){
 if(a[0]!=='-n')process.exit(5);
 if(a[1]==='/bin/sh')fs.writeFileSync(unit(a.at(-1)),fs.readFileSync(0),{flag:'wx',mode:0o644});
 else if(a[1]==='systemctl')systemctl(a.slice(2));
 else if(a[1]==='rm')fs.unlinkSync(unit(a.at(-1)));else process.exit(6);
}else{
 const i=a.indexOf('get');
 if(i>=0){
  const kind=a[i+1],name=a[i+2];
  if(kind==='namespace')console.log(JSON.stringify({metadata:{uid:a.includes('--kubeconfig')?'source-cluster':'edge-cluster'}}));
  else if(kind==='nodes')console.log(JSON.stringify({items:[0,1].map(i=>({metadata:{name:'k3d-fixture-agent-'+i}}))}));
  else if(kind==='configmap')console.log(JSON.stringify(s.ca));
  else if(kind.includes('deployments,'))console.log(JSON.stringify({items:[...s.existing,...Object.values(s.objects).filter(r=>r.kind!=='Secret')]}));
  else {const current=Object.values(s.objects).find(r=>r.metadata.name===name);if(current)console.log(JSON.stringify(current));}
 }else if(a.includes('create')){
  const r=JSON.parse(fs.readFileSync(0,'utf8')),key=r.kind+'/'+r.metadata.name;
  if(s.objects[key])process.exit(7);
  r.metadata.uid='uid-'+Object.keys(s.objects).length;r.metadata.resourceVersion='1';s.objects[key]=r;save();
  if(s.loseCreateResponse)process.exit(8);console.log(JSON.stringify(r));
 }else if(a.includes('delete')&&a.includes('--raw')){
  const uri=a[a.indexOf('--raw')+1],name=uri.split('/').at(-1),entry=Object.entries(s.objects).find(([,r])=>r.metadata.name===name);
  const body=JSON.parse(fs.readFileSync(0,'utf8'));
  if(!entry||body.preconditions.uid!==entry[1].metadata.uid||body.preconditions.resourceVersion!==entry[1].metadata.resourceVersion)process.exit(9);
  s.deletes.push(body);delete s.objects[entry[0]];save();console.log('{}');
 }else process.exit(10);
}
`;

async function setup() {
  const directory = await mkdtemp(join(tmpdir(), "kodex-edge-release-test-"));
  await mkdir(join(directory, "bin")); await mkdir(join(directory, "units"));
  for (const program of ["kubectl", "docker", "sudo", "systemctl"])
    await writeFile(join(directory, "bin", program), mock, { mode: 0o700 });
  execFileSync("openssl", ["req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2", "-subj", "/CN=fixture",
    "-addext", "basicConstraints=critical,CA:TRUE", "-keyout", join(directory, "key"), "-out", join(directory, "ca")],
  { timeout: 15000, stdio: "ignore" });
  await chmod(join(directory, "key"), 0o600);
  const ca = { kind: "ConfigMap", metadata: { name: "kodex-oidc-ca", namespace: "kodex-system", uid: "source-ca" },
    data: { "ca.pem": await readFile(join(directory, "ca"), "utf8") } };
  const existing = [{ kind: "Deployment", metadata: { name: "traefik", namespace: "kube-system", uid: "traefik" },
    spec: { replicas: 1 } }];
  const fixture = { ca, existing, enabled: [], objects: {}, deletes: [] };
  await writeFile(join(directory, "fixture.json"), JSON.stringify(fixture), { mode: 0o600 });
  const kubeconfig = join(directory, "kubeconfig"); await writeFile(kubeconfig, "PRIVATE_SENTINEL", { mode: 0o600 });
  const rendered = renderPublicEdge(config), projection = publicCAProjection(ca, config.namespace);
  const plan = { schema: "kodex.public-edge-plan/v2", config, context: "edge-fixture", clusterUID: "edge-cluster",
    namespaceUID: "edge-cluster", contentSHA256: rendered.contentSHA256, protectedSHA256: protectedCommitment(existing),
    source: { clusterUID: "source-cluster", context: "source-fixture", kubeconfig, caUID: ca.metadata.uid,
      caProjectionSHA256: fingerprint(projection) }, peers: [0, 1].map(i => ({ name: `k3d-fixture-agent-${i}`,
      id: String(i + 1).repeat(64), peerIP: `172.20.0.${i + 2}` })) };
  await writeFile(join(directory, "plan.json"), JSON.stringify(plan), { mode: 0o600 });
  await writeFile(join(directory, "resources.json"), JSON.stringify({ apiVersion: "v1", kind: "List",
    items: [projection, ...rendered.resources] }), { mode: 0o600 });
  for (const [name, value] of Object.entries(rendered.units)) await writeFile(join(directory, name), value, { mode: 0o600 });
  // Только оснастка переводит закрытые host paths в временный каталог. CLI не
  // имеет флага/env для выбора иного каталога /etc/systemd/system.
  const moduleURL = new URL("./edge-release.mjs", import.meta.url).href;
  const wrapper = `import{releasePublicEdge}from${JSON.stringify(moduleURL)};import{lstat,open}from'node:fs/promises';
import{join,basename}from'node:path';const base=${JSON.stringify(directory)};
const map=p=>{if(!/^\\/etc\\/systemd\\/system\\/kodex-public-edge-https-[01]\\.(socket|service)$/.test(p))throw Error('TEST_PATH_REJECTED');return join(base,'units',basename(p));};
try{await releasePublicEdge(process.argv.slice(2),{lstat:async p=>{const s=await lstat(map(p));return Object.create(s,{uid:{value:0}});},open:(p,f)=>open(map(p),f)});}
catch(e){process.stderr.write(/^[A-Z_]+$/.test(e.message)?e.message:'TEST_RELEASE_FAILED');process.exitCode=1;}`;
  await writeFile(join(directory, "wrapper.mjs"), wrapper, { mode: 0o600 });
  return { directory, plan, fixture, run: (mode, output, extra = []) => {
    try { return { status: 0, output: execFileSync(process.execPath, [join(directory, "wrapper.mjs"), mode, "--context", plan.context,
      "--plan", join(directory, "plan.json"), "--output", join(directory, output), ...extra], {
      env: { PATH: join(directory, "bin") + ":/usr/bin:/bin" }, timeout: 25000, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }) }; }
    catch (error) { return { status: error.status, output: (error.stdout ?? "") + (error.stderr ?? "") }; }
  } };
}

test("synthetic apply/readback/rollback preserves foreign resources and uses UID fenced deletion", async () => {
  const { directory, plan, fixture, run } = await setup();
  try {
    const confirmation = ["--approved-plan-sha", fingerprint(plan)], journal = join(directory, "apply.jsonl");
    const applied = run("apply", "apply.jsonl", confirmation); assert.equal(applied.status, 0, applied.output);
    assert.equal(applied.output.includes("PRIVATE_SENTINEL"), false);
    let state = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    assert.equal(Object.keys(state.objects).length, 6); assert.equal(state.enabled.length, 2);
    const readback = run("readback", "readback.jsonl", ["--journal", journal]); assert.equal(readback.status, 0, readback.output);
    const rollback = run("rollback", "rollback.jsonl", ["--journal", journal, ...confirmation]);
    assert.equal(rollback.status, 0, rollback.output);
    state = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    assert.deepEqual(state.existing, fixture.existing); assert.deepEqual(state.objects, {}); assert.deepEqual(state.enabled, []);
    assert.equal(state.deletes.length, 6);
    assert.equal(state.deletes.every(body => body.preconditions.uid && body.preconditions.resourceVersion === "1"), true);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("wrong approval and foreign drift never install units or create resources", async () => {
  const { directory, plan, fixture, run } = await setup();
  try {
    const wrong = run("apply", "wrong.jsonl", ["--approved-plan-sha", "0".repeat(64)]);
    assert.notEqual(wrong.status, 0); assert.match(wrong.output, /EXACT_OWNER_CONFIRMATION_REQUIRED/);
    fixture.existing[0].spec.replicas = 2;
    await writeFile(join(directory, "fixture.json"), JSON.stringify(fixture));
    const drift = run("apply", "drift.jsonl", ["--approved-plan-sha", fingerprint(plan)]);
    assert.notEqual(drift.status, 0); assert.match(drift.output, /PROTECTED_STATE_DRIFT/);
    const trace = (await readFile(join(directory, "trace"), "utf8")).trim().split("\n").map(JSON.parse);
    assert.equal(trace.some(row => row.program === "sudo" || row.args.includes("create")), false);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("recovery starts exact installed sockets without rewriting resources or units", async () => {
  const { directory, plan, run } = await setup();
  try {
    const confirmation = ["--approved-plan-sha", fingerprint(plan)], journal = join(directory, "apply.jsonl");
    const applied = run("apply", "apply.jsonl", confirmation); assert.equal(applied.status, 0, applied.output);
    const state = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    state.stopped = [...state.enabled];
    await writeFile(join(directory, "fixture.json"), JSON.stringify(state));
    const countBefore = (await readFile(join(directory, "trace"), "utf8")).trim().split("\n").length;
    const recovered = run("recover", "recover.jsonl", ["--journal", journal, ...confirmation]);
    assert.equal(recovered.status, 0, recovered.output);
    const after = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    assert.deepEqual(after.objects, state.objects); assert.deepEqual(after.existing, state.existing);
    assert.deepEqual(after.stopped, []);
    const writes = (await readFile(join(directory, "trace"), "utf8")).trim().split("\n").slice(countBefore)
      .map(JSON.parse).filter(row => row.program === "sudo" || row.args.includes("create") || row.args.includes("delete"));
    assert.deepEqual(writes.map(row => row.args), state.enabled.map(name => ["-n", "systemctl", "start", name]));
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("recovery rejects replaced resources and modified units before socket start", async () => {
  for (const drift of ["resource", "unit"]) {
    const { directory, plan, run } = await setup();
    try {
      const confirmation = ["--approved-plan-sha", fingerprint(plan)], journal = join(directory, "apply.jsonl");
      const applied = run("apply", "apply.jsonl", confirmation); assert.equal(applied.status, 0, applied.output);
      if (drift === "resource") {
        const state = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
        state.objects["Service/kodex-public-edge"].metadata.uid = "replaced";
        await writeFile(join(directory, "fixture.json"), JSON.stringify(state));
      } else await writeFile(join(directory, "units", "kodex-public-edge-https-1.service"), "foreign unit\n");
      const recovered = run("recover", "recover.jsonl", ["--journal", journal, ...confirmation]);
      assert.notEqual(recovered.status, 0);
      assert.match(recovered.output, drift === "resource" ? /RECOVERY_RESOURCE_DRIFT/ : /INSTALLED_UNIT_DRIFT/);
      const trace = (await readFile(join(directory, "trace"), "utf8")).trim().split("\n").map(JSON.parse);
      assert.equal(trace.some(row => row.program === "sudo" && row.args.includes("start")), false);
    } finally { await rm(directory, { recursive: true, force: true }); }
  }
});

test("lost create response is not retried and rollback resolves only the exact intent", async () => {
  const { directory, plan, fixture, run } = await setup();
  try {
    fixture.loseCreateResponse = true;
    await writeFile(join(directory, "fixture.json"), JSON.stringify(fixture));
    const confirmation = ["--approved-plan-sha", fingerprint(plan)], journal = join(directory, "apply.jsonl");
    const failed = run("apply", "apply.jsonl", confirmation);
    assert.notEqual(failed.status, 0); assert.match(failed.output, /OPERATION_FAILED_OR_UNKNOWN/);
    const state = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    assert.equal(Object.keys(state.objects).length, 1);
    const rollback = run("rollback", "rollback.jsonl", ["--journal", journal, ...confirmation]);
    assert.equal(rollback.status, 0, rollback.output);
    assert.deepEqual(JSON.parse(await readFile(join(directory, "fixture.json"), "utf8")).objects, {});
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("rollback refuses replaced resources before deleting any other owned object", async () => {
  const { directory, plan, run } = await setup();
  try {
    const confirmation = ["--approved-plan-sha", fingerprint(plan)], journal = join(directory, "apply.jsonl");
    const applied = run("apply", "apply.jsonl", confirmation); assert.equal(applied.status, 0, applied.output);
    const state = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    state.objects["Secret/kodex-public-edge-upstream-ca"].metadata.uid = "foreign-replacement";
    await writeFile(join(directory, "fixture.json"), JSON.stringify(state));
    const rollback = run("rollback", "rollback.jsonl", ["--journal", journal, ...confirmation]);
    assert.notEqual(rollback.status, 0); assert.match(rollback.output, /ROLLBACK_RESOURCE_DRIFT/);
    const after = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    assert.equal(after.deletes.length, 0); assert.equal(Object.keys(after.objects).length, 6);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("real kubectl raw DELETE sends UID and resourceVersion preconditions as JSON body", async () => {
  const directory = await mkdtemp(join(tmpdir(), "kodex-edge-delete-test-"));
  let request;
  const server = createServer(async (incoming, response) => {
    let body = ""; for await (const chunk of incoming) body += chunk;
    request = { method: incoming.method, path: incoming.url, body: JSON.parse(body) };
    response.setHeader("content-type", "application/json");
    response.end(JSON.stringify({ apiVersion: "v1", kind: "Status", status: "Success" }));
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  try {
    const kubeconfig = join(directory, "kubeconfig"); await writeFile(kubeconfig, "{}", { mode: 0o600 });
    const body = { apiVersion: "v1", kind: "DeleteOptions", preconditions: { uid: "fixture-uid", resourceVersion: "42" } };
    const path = "/api/v1/namespaces/fixture/services/kodex-public-edge";
    const pending = execute("/bin/sh", ["-c", 'cat | kubectl "$@"', "edge-test-pipe", "--kubeconfig", kubeconfig,
      "--server", `http://127.0.0.1:${server.address().port}`, "--request-timeout=5s", "delete", "--raw", path,
      "-f", "/dev/stdin"], { env: { PATH: process.env.PATH, HOME: directory }, timeout: 10000 });
    pending.child.stdin.on("error", () => {}); pending.child.stdin.end(JSON.stringify(body));
    await pending;
    assert.equal(request.method, "DELETE");
    const target = new URL(request.path, "http://127.0.0.1");
    assert.equal(target.pathname, path); assert.equal(target.searchParams.get("timeout"), "5s");
    assert.deepEqual(request.body, body);
  } finally { await new Promise(resolve => server.close(resolve)); await rm(directory, { recursive: true, force: true }); }
});
