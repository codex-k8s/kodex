import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmod, link, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { privateArchiveHash, validateStableReceipt } from "./stable-image-publication.mjs";
import { fingerprint } from "./stable-inventory.mjs";

const sha = value => createHash("sha256").update(value).digest("hex");
const cli = fileURLToPath(new URL("./stable-image-publication.mjs", import.meta.url));

test("publication receipt has a closed component registry and immutable source-specific tag", () => {
  const receipt = { schema: "kodex.stable-local-build/v1", component: "control-plane", sourceSHA: "a".repeat(40),
    contextSHA256: "b".repeat(64), archiveSHA256: "c".repeat(64), imageDigest: "sha256:" + "d".repeat(64),
    admission: "NOT_RUN", publication: "NOT_RUN", nodePull: "NOT_RUN" };
  assert.equal(validateStableReceipt(receipt).tag, `stable-${receipt.sourceSHA}-${"d".repeat(12)}`);
  for (const patch of [{ component: "agent-runner" }, { component: "role-image-inputs" }, { sourceSHA: "0".repeat(40) },
    { publication: "PASS" }, { imageDigest: "mutable:latest" }])
    assert.throws(() => validateStableReceipt({ ...receipt, ...patch }));
});

test("verified archive descriptor rejects writable files links and unexpected ownership shape", async () => {
  const directory = await mkdtemp(join(tmpdir(), "kodex-stable-archive-node-"));
  try {
    const path = join(directory, "archive"); await writeFile(path, "fixture", { mode: 0o400 });
    assert.equal(await privateArchiveHash(path), sha("fixture"));
    await chmod(path, 0o600); await assert.rejects(privateArchiveHash(path), /PRIVATE_ARCHIVE_REQUIRED/);
    await chmod(path, 0o400); await symlink(path, join(directory, "link"));
    await assert.rejects(privateArchiveHash(join(directory, "link")));
    await link(path, join(directory, "hardlink")); await assert.rejects(privateArchiveHash(path), /PRIVATE_ARCHIVE_REQUIRED/);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

const mock = `#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),a=process.argv.slice(2),base=path.join(__dirname,'..');
const statePath=path.join(base,'fixture.json'),s=JSON.parse(fs.readFileSync(statePath,'utf8'));
const save=()=>fs.writeFileSync(statePath,JSON.stringify(s));
fs.appendFileSync(path.join(base,'trace'),JSON.stringify({program:path.basename(process.argv[1]),args:a})+'\\n');
const program=path.basename(process.argv[1]);
if(program==='git'){
 if(a[0]==='rev-parse')console.log((s.sourceDrift?'f':'e').repeat(40));
 else if(a[0]==='status'){}
 else if(a[0]==='archive')process.stdout.write('synthetic git archive');
 else if(!['merge-base','diff'].includes(a[0]))process.exit(3);
}else if(program==='kubectl'){
 if(a.includes('port-forward')){s.forwardPID=process.pid;save();console.log('Forwarding from 127.0.0.1:45231 -> 5003');setInterval(()=>{},1000);}
 else{
  const i=a.indexOf('get'),kind=a[i+1],name=a[i+2];
  if(kind==='namespace'){if(a.some(x=>x.startsWith('jsonpath=')))console.log('uid-'+name);else console.log(JSON.stringify({metadata:{uid:'uid-'+name}}));}
  else if(kind==='secret'){
   if(a.some(x=>x.startsWith('jsonpath=')))console.log('fixture-writer|1');
   else console.log(JSON.stringify({metadata:{uid:'fixture-writer',resourceVersion:'1'},data:Object.fromEntries([
     'registry-client.crt','registry-client.key','ca.pem','promotion.username','promotion.password'].map(k=>[k,Buffer.from('PRIVATE_AUTH_SENTINEL').toString('base64')]))}));
  }else if(a.some(x=>x.startsWith('jsonpath=')))console.log('uid-'+kind+'-'+name);
  else console.log(JSON.stringify({metadata:{name,namespace:'kodex-system',uid:'uid-'+kind+'-'+name,
    labels:{'app.kubernetes.io/part-of':'kodex'}},spec:{replicas:s.registryDrift?2:1,
    env:[{name:'SENSITIVE',value:'PRIVATE_REGISTRY_ENV_SENTINEL'}]}}));
 }
}else if(program==='docker'){
 if(a[0]==='image'&&a[1]==='inspect')console.log('sha256:'+'1'.repeat(64));
 else if(a[0]==='create'){
  const mounts=a.filter((_,i)=>a[i-1]==='-v'),work=mounts.find(v=>v.endsWith(':/work:ro')).slice(0,-9);
  const c=JSON.parse(fs.readFileSync(path.join(work,'regctl.json'),'utf8')),host=Object.values(c.hosts)[0];
  if(host.tls!=='enabled'||!host.clientCert||!host.clientKey||a.some(x=>x.includes('PRIVATE_AUTH_SENTINEL')))process.exit(4);
  if(!a.includes('--read-only')||!mounts.every(v=>v.endsWith(':ro')))process.exit(5);
  s.creates++;s.mode=a.find(x=>x.startsWith('KODEX_PUBLICATION_MODE=')).split('=')[1];save();console.log('2'.repeat(64));
 }else if(a[0]==='start'){
  if(s.mode==='apply'){s.imports++;s.published=true;}else if(!s.published)process.exit(6);
  save();if(s.loseResponse&&s.mode==='apply')process.exit(7);
 }else if(a[0]==='inspect')console.log('0');
 else if(a[0]==='rm'){if(a.at(-1)!=='2'.repeat(64))process.exit(8);s.removes++;save();}
 else process.exit(9);
}else process.exit(10);
`;

async function setup() {
  const directory = await mkdtemp(join(tmpdir(), "kodex-stable-publication-test-"));
  await mkdir(join(directory, "bin")); await mkdir(join(directory, "tools"), { mode: 0o700 });
  for (const name of ["git", "kubectl", "docker"]) await writeFile(join(directory, "bin", name), mock, { mode: 0o700 });
  await writeFile(join(directory, "tools", "image-supply-chain-tools-docker-tag"), "kodex-local/image-admission-tools:" + "1".repeat(64), { mode: 0o600 });
  const fixture = { creates: 0, imports: 0, removes: 0 };
  await writeFile(join(directory, "fixture.json"), JSON.stringify(fixture), { mode: 0o600 });
  const layout = join(directory, "oci"); await mkdir(join(layout, "blobs", "sha256"), { recursive: true });
  const files = [];
  const document = async (value, mediaType) => {
    const bytes = typeof value === "string" ? value : JSON.stringify(value), digest = sha(bytes);
    const name = "blobs/sha256/" + digest; await writeFile(join(layout, name), bytes); files.push(name);
    return { mediaType, digest: "sha256:" + digest, size: Buffer.byteLength(bytes) };
  };
  const config = await document({ os: "linux", architecture: "amd64", config: { User: "10001:10001",
    Entrypoint: ["/usr/local/bin/control-plane"] } }, "application/vnd.oci.image.config.v1+json");
  const layer = await document("synthetic layer", "application/vnd.oci.image.layer.v1.tar");
  const manifest = await document({ schemaVersion: 2, config, layers: [layer] }, "application/vnd.oci.image.manifest.v1+json");
  manifest.annotations = { "org.opencontainers.image.ref.name": "a".repeat(40),
    "io.containerd.image.name": "registry.local.kodex/kodex/control-plane:" + "a".repeat(40) };
  await writeFile(join(layout, "index.json"), JSON.stringify({ schemaVersion: 2, manifests: [manifest] }));
  await writeFile(join(layout, "oci-layout"), '{"imageLayoutVersion":"1.0.0"}'); files.push("index.json", "oci-layout");
  const archive = join(directory, "image.tar"); execFileSync("tar", ["-cf", archive, "-C", layout, ...files]); await chmod(archive, 0o600);
  const receipt = { schema: "kodex.stable-local-build/v1", component: "control-plane", sourceSHA: "a".repeat(40),
    contextSHA256: sha("synthetic git archive"), archiveSHA256: sha(await readFile(archive)), imageDigest: manifest.digest,
    dockerfile: "services/internal/control-plane/Dockerfile", target: "", admission: "NOT_RUN", publication: "NOT_RUN", nodePull: "NOT_RUN" };
  await writeFile(join(directory, "receipt.json"), JSON.stringify(receipt), { mode: 0o600 });
  const run = (mode, output, extra = []) => {
    try { return { status: 0, output: execFileSync(process.execPath, [cli, mode, "--context", "fixture",
      "--output", join(directory, output), ...extra], { env: { PATH: join(directory, "bin") + ":/usr/local/bin:/usr/bin:/bin",
        PYTHONDONTWRITEBYTECODE: "1" }, timeout: 30000, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }) }; }
    catch (error) { return { status: error.status, output: (error.stdout ?? "") + (error.stderr ?? "") }; }
  };
  const plan = () => run("plan", "plan", ["--receipt", join(directory, "receipt.json"), "--archive", archive,
    "--tool-state-directory", join(directory, "tools")]);
  return { directory, fixture, run, plan };
}

test("real publication CLI and worker keep credentials private and readback never imports again", async () => {
  const { directory, run, plan } = await setup();
  try {
    const planned = plan(); assert.equal(planned.status, 0, planned.output);
    const encoded = await readFile(join(directory, "plan", "plan.json"), "utf8"), value = JSON.parse(encoded);
    assert.equal(encoded.includes("PRIVATE_AUTH_SENTINEL"), false); assert.equal(encoded.includes("PRIVATE_REGISTRY_ENV_SENTINEL"), false);
    const args = ["--plan", join(directory, "plan", "plan.json")];
    const wrong = run("apply", "wrong.jsonl", [...args, "--approved-plan-sha", "0".repeat(64)]);
    assert.notEqual(wrong.status, 0); assert.match(wrong.output, /EXACT_OWNER_CONFIRMATION_REQUIRED/);
    const applied = run("apply", "apply.jsonl", [...args, "--approved-plan-sha", fingerprint(value)]);
    assert.equal(applied.status, 0, applied.output); assert.equal(applied.output.includes("PRIVATE_AUTH_SENTINEL"), false);
    const readback = run("readback", "readback.jsonl", args); assert.equal(readback.status, 0, readback.output);
    const state = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    assert.equal(state.imports, 1); assert.equal(state.creates, 2); assert.equal(state.removes, 2);
    assert.throws(() => process.kill(state.forwardPID, 0), { code: "ESRCH" });
    const trace = await readFile(join(directory, "trace"), "utf8"); assert.equal(trace.includes("PRIVATE_AUTH_SENTINEL"), false);
    assert.equal((await readFile(join(directory, "apply.jsonl"), "utf8")).includes("PRIVATE_AUTH_SENTINEL"), false);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("lost registry response is resolved through readback and registry drift blocks helper creation", async () => {
  const { directory, fixture, run, plan } = await setup();
  try {
    const planned = plan(); assert.equal(planned.status, 0, planned.output);
    const value = JSON.parse(await readFile(join(directory, "plan", "plan.json"), "utf8"));
    const args = ["--plan", join(directory, "plan", "plan.json")];
    fixture.registryDrift = true; await writeFile(join(directory, "fixture.json"), JSON.stringify(fixture));
    const drift = run("apply", "drift.jsonl", [...args, "--approved-plan-sha", fingerprint(value)]);
    assert.notEqual(drift.status, 0); assert.match(drift.output, /REGISTRY_DRIFT/);
    assert.equal(JSON.parse(await readFile(join(directory, "fixture.json"), "utf8")).creates, 0);
    fixture.registryDrift = false; fixture.loseResponse = true;
    await writeFile(join(directory, "fixture.json"), JSON.stringify(fixture));
    const lost = run("apply", "lost.jsonl", [...args, "--approved-plan-sha", fingerprint(value)]);
    assert.notEqual(lost.status, 0); assert.match(lost.output, /OPERATION_FAILED_OR_UNKNOWN/);
    const readback = run("readback", "readback.jsonl", args); assert.equal(readback.status, 0, readback.output);
    const state = JSON.parse(await readFile(join(directory, "fixture.json"), "utf8"));
    assert.equal(state.imports, 1); assert.equal(state.removes, 2);
  } finally { await rm(directory, { recursive: true, force: true }); }
});
