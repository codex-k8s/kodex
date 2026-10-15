import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmod, mkdir, mkdtemp, open, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { edgeCollections, protectedCommitment, publicCAProjection, readPrivateJSON, validateEdgeArtifacts } from "./edge-plan.mjs";
import { renderPublicEdge } from "./edge-model.mjs";
import { fingerprint } from "../../tools/release/stable-inventory.mjs";

const config = { platformHost: "platform.example.test", oidcHost: "sso.example.test", namespace: "kube-system",
  bridgeIP: "172.20.0.1", allowedPeers: ["172.20.0.2", "172.20.0.3"], backendIP: "127.0.0.2", relayPorts: [18443, 18444],
  upstreamServerName: "upstream.example.test", certResolver: "public" };
const cli = fileURLToPath(new URL("./edge-plan.mjs", import.meta.url));

async function certificateFixture(directory) {
  const keyPath = join(directory, "fixture-key.pem"), certPath = join(directory, "fixture-ca.pem");
  execFileSync("openssl", ["req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2",
    "-subj", "/CN=kodex-edge-fixture", "-addext", "basicConstraints=critical,CA:TRUE", "-keyout", keyPath,
    "-out", certPath], { stdio: "ignore", timeout: 15000 });
  await chmod(keyPath, 0o600);
  return { kind: "ConfigMap", metadata: { name: "kodex-oidc-ca", namespace: "kodex-system", uid: "source-ca" },
    data: { "ca.pem": await readFile(certPath, "utf8") } };
}

test("protected ingress baseline excludes only owned edge names and detects routing drift", () => {
  const ingress = { kind: "Deployment", metadata: { name: "traefik", uid: "uid", annotations: { exact: "fixture" } }, spec: { replicas: 1 } };
  const route = { kind: "Ingress", metadata: { name: "existing", uid: "route" }, spec: { rules: [{ host: "neighbor.example.test" }] } };
  const hash = protectedCommitment([ingress, route]);
  assert.equal(hash, protectedCommitment([route, ingress, { kind: "Service", metadata: { name: "kodex-public-edge",
    labels: { "app.kubernetes.io/managed-by": "kodex-public-edge" } }, spec: {} }]));
  assert.notEqual(hash, protectedCommitment([route, ingress, { kind: "Service", metadata: { name: "kodex-public-edge",
    uid: "unmanaged" }, spec: { ports: [] } }]));
  const changed = structuredClone(route); changed.spec.rules[0].host = "changed.example.test";
  assert.notEqual(hash, protectedCommitment([ingress, changed]));
  const relabeled = structuredClone(route); relabeled.metadata.labels = { sensitive: "changed" };
  assert.notEqual(hash, protectedCommitment([ingress, relabeled]));
  const misleading = { kind: "Deployment", metadata: { name: "kodex-public-edge", uid: "foreign",
    labels: { "app.kubernetes.io/managed-by": "kodex-public-edge" } }, spec: { replicas: 1 } };
  assert.notEqual(hash, protectedCommitment([ingress, route, misleading]));
  const endpoints = { kind: "EndpointSlice", metadata: { name: "foreign", namespace: "fixture", uid: "endpoints" },
    addressType: "IPv4", endpoints: [{ addresses: ["172.20.0.9"] }], ports: [{ name: "https", port: 443 }] };
  const before = protectedCommitment([ingress, route, endpoints]);
  endpoints.endpoints[0].addresses = ["172.20.0.10"];
  assert.notEqual(before, protectedCommitment([ingress, route, endpoints]));
  assert.throws(() => protectedCommitment([route]), /INGRESS_REQUIRED/);
});

test("private plans reject broad modes and links without reading their contents", async () => {
  const directory = await mkdtemp(join(tmpdir(), "kodex-edge-plan-test-"));
  try {
    const path = join(directory, "plan.json");
    const file = await open(path, "wx", 0o600);
    try { await file.writeFile('{"fixture":true}'); } finally { await file.close(); }
    assert.deepEqual(await readPrivateJSON(path), { fixture: true });
    await chmod(path, 0o644);
    await assert.rejects(readPrivateJSON(path), /PRIVATE_FILE_REQUIRED/);
    await chmod(path, 0o600);
    await symlink(path, join(directory, "link"));
    await assert.rejects(readPrivateJSON(join(directory, "link")));
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("CA projection accepts only a current public CA from the exact source and revalidates every artifact", async () => {
  const directory = await mkdtemp(join(tmpdir(), "kodex-edge-ca-test-"));
  try {
    const source = await certificateFixture(directory), ca = publicCAProjection(source, config.namespace);
    assert.deepEqual(Object.keys(ca.data), ["ca.crt"]);
    assert.equal(Buffer.from(ca.data["ca.crt"], "base64").toString(), source.data["ca.pem"]);
    for (const value of [null, { ...source, kind: "Secret" }, { ...source, metadata: { ...source.metadata, namespace: "foreign" } },
      { ...source, data: { "ca.pem": "not a certificate" } },
      { ...source, data: { "ca.pem": source.data["ca.pem"] + "\n-----BEGIN PRIVATE KEY-----\n" } },
      { ...source, data: { "ca.pem": source.data["ca.pem"] + source.data["ca.pem"] } }])
      assert.throws(() => publicCAProjection(value, config.namespace), /SOURCE_CA_INVALID/);
    assert.throws(() => publicCAProjection(source, "foreign/namespace"), /SOURCE_CA_INVALID/);
    const rendered = renderPublicEdge(config), plan = { schema: "kodex.public-edge-plan/v2", config,
      clusterUID: "edge-cluster", namespaceUID: "edge-cluster", contentSHA256: rendered.contentSHA256,
      source: { clusterUID: "source-cluster", caUID: source.metadata.uid, caProjectionSHA256: fingerprint(ca) } };
    const bundle = { apiVersion: "v1", kind: "List", items: [ca, ...rendered.resources] };
    assert.equal(validateEdgeArtifacts(plan, bundle, rendered.units).resources.length, 6);
    const drift = structuredClone(bundle); drift.items.at(-1).spec.routes[0].match = "HostRegexp(`.*`)";
    assert.throws(() => validateEdgeArtifacts(plan, drift, rendered.units), /ARTIFACT_MISMATCH/);
    const extraSecret = structuredClone(bundle); extraSecret.items[0].data["private-key"] = "sentinel";
    assert.throws(() => validateEdgeArtifacts(plan, extraSecret, rendered.units), /ARTIFACT_MISMATCH/);
    assert.throws(() => validateEdgeArtifacts(plan, bundle, { ...rendered.units, "foreign.service": "bad" }), /ARTIFACT_MISMATCH/);
    assert.throws(() => validateEdgeArtifacts({ ...plan, source: { ...plan.source, clusterUID: plan.clusterUID } },
      bundle, rendered.units), /PLAN_IDENTITY_INVALID/);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

const mock = `#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),a=process.argv.slice(2),base=path.join(__dirname,'..');
const state=JSON.parse(fs.readFileSync(path.join(base,'fixture.json'),'utf8'));
fs.appendFileSync(path.join(base,'trace'),JSON.stringify({program:path.basename(process.argv[1]),args:a})+'\\n');
if(path.basename(process.argv[1])==='docker'){
  if(a[0]!=='inspect')process.exit(4);
  const index=Number(a[1].slice(-1));
  console.log(JSON.stringify([{Id:String(index+1).repeat(64),State:{Running:true},
    NetworkSettings:{Networks:{fixture:{IPAddress:state.badPeer?'172.20.0.99':'172.20.0.'+(index+2)}}}}]));
}else{
  const i=a.indexOf('get');if(i<0)process.exit(5);
  const kind=a[i+1];
  if(kind==='secret'){if(state.collision)console.log('secret/kodex-public-edge-upstream-ca');}
  else if(kind==='namespace')console.log(JSON.stringify({metadata:{uid:a.includes('--kubeconfig')?'source-cluster':'edge-cluster'}}));
  else if(kind==='nodes')console.log(JSON.stringify({items:[0,1].map(i=>({metadata:{name:'k3d-fixture-agent-'+i}}))}));
  else if(kind==='configmap')console.log(JSON.stringify(state.ca));
  else if(kind.includes('deployments,'))console.log(JSON.stringify({items:state.existing}));
  else process.exit(6);
}
`;

test("actual plan CLI binds both clusters, public CA and Docker peers without any writes", async () => {
  const directory = await mkdtemp(join(tmpdir(), "kodex-edge-cli-test-"));
  try {
    await mkdir(join(directory, "bin"), { mode: 0o700 });
    for (const program of ["kubectl", "docker"]) await writeFile(join(directory, "bin", program), mock, { mode: 0o700 });
    const state = { ca: await certificateFixture(directory), existing: [{ kind: "Deployment", metadata: {
      name: "traefik", namespace: "kube-system", uid: "traefik" }, spec: { template: { spec: { containers: [{
      args: ["--certificatesresolvers.public.acme.email=fixture@example.test"] }] } } } }] };
    await writeFile(join(directory, "fixture.json"), JSON.stringify(state), { mode: 0o600 });
    await writeFile(join(directory, "config.json"), JSON.stringify(config), { mode: 0o600 });
    const kubeconfig = join(directory, "source-kubeconfig");
    await writeFile(kubeconfig, "PRIVATE_KUBECONFIG_SENTINEL", { mode: 0o600 });
    const run = output => {
      try { return { status: 0, output: execFileSync(process.execPath, [cli, "--context", "fixture-edge",
        "--kodex-context", "fixture-source", "--kodex-kubeconfig", kubeconfig, "--config", join(directory, "config.json"),
        "--output", join(directory, output)], { env: { PATH: join(directory, "bin") + ":/usr/bin:/bin" },
        timeout: 15000, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }) }; }
      catch (error) { return { status: error.status, output: (error.stdout ?? "") + (error.stderr ?? "") }; }
    };
    const result = run("valid"); assert.equal(result.status, 0, result.output);
    const plan = await readPrivateJSON(join(directory, "valid", "plan.json"));
    assert.equal(plan.schema, "kodex.public-edge-plan/v2");
    assert.equal(plan.source.clusterUID, "source-cluster");
    assert.equal(plan.peers.length, 2);
    const trace = (await readFile(join(directory, "trace"), "utf8")).trim().split("\n").map(JSON.parse);
    assert.equal(trace.every(row => row.program === "docker" ? row.args[0] === "inspect" : row.args.includes("get")), true);
    assert.equal(trace.some(row => row.args.includes(edgeCollections)), true);
    assert.equal(result.output.includes("PRIVATE_KUBECONFIG_SENTINEL"), false);
    const bundle = await readPrivateJSON(join(directory, "valid", "resources.json"));
    const rendered = renderPublicEdge(config);
    assert.equal(validateEdgeArtifacts(plan, bundle, rendered.units).resources.length, 6);
    state.badPeer = true;
    await writeFile(join(directory, "fixture.json"), JSON.stringify(state));
    const badPeer = run("bad-peer"); assert.notEqual(badPeer.status, 0); assert.match(badPeer.output, /PEER_NODE_BINDING_INVALID/);
    state.badPeer = false; state.collision = true;
    await writeFile(join(directory, "fixture.json"), JSON.stringify(state));
    const collision = run("collision"); assert.notEqual(collision.status, 0); assert.match(collision.output, /NAME_COLLISION/);
    await chmod(kubeconfig, 0o644); state.collision = false;
    await writeFile(join(directory, "fixture.json"), JSON.stringify(state));
    const unsafe = run("unsafe"); assert.notEqual(unsafe.status, 0); assert.match(unsafe.output, /PRIVATE_KUBECONFIG_REQUIRED/);
  } finally { await rm(directory, { recursive: true, force: true }); }
});
