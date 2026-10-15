import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import test from "node:test";
import { fingerprint } from "./stable-inventory.mjs";
import { publicClients } from "./stable-public-clients.mjs";
import { publicClientFingerprint, publicClientPhase, publicClientPut, publicClientTransition } from "./stable-public-clients-model.mjs";
const profile = JSON.parse(await readFile(new URL("../../infra/public-ingress/identity-profile.json", import.meta.url), "utf8"));
function client(name, n) {
  const callback = name === "kodex-control-center" ? "/auth/callback" : "/oauth2/callback";
  return { id: "aaaaaaaa-bbbb-cccc-dddd-" + String(n).padStart(12, "0"), clientId: name, enabled: true, protocol: "openid-connect", publicClient: n === 1,
    standardFlowEnabled: true, implicitFlowEnabled: false, directAccessGrantsEnabled: false, serviceAccountsEnabled: false,
    redirectUris: ["https://platform.example.test" + callback], webOrigins: ["https://platform.example.test"],
    attributes: { "pkce.code.challenge.method": "S256", "post.logout.redirect.uris": "https://platform.example.test/*", "client.session.idle.timeout": "43200" },
    defaultClientScopes: ["roles", "openid"], secret: "PRIVATE_CLIENT_SECRET_SENTINEL", registrationAccessToken: "PRIVATE_REGISTRATION_SENTINEL", access: { configure: true } };
}

test("client transition changes only exact callback/origin/logout without secret, scope or role updates", () => {
  const c = client("kodex-control-center", 1), original = structuredClone(c), row = publicClientTransition(c, "https://platform.example.test", profile);
  assert.deepEqual(c, original); assert.equal(JSON.stringify(row).includes("PRIVATE_"), false);
  const body = publicClientPut(c, row, true); assert.deepEqual(Object.keys(body).sort(), ["attributes", "redirectUris", "webOrigins"]);
  assert.equal(body.attributes["pkce.code.challenge.method"], "S256"); assert.equal(body.attributes["client.session.idle.timeout"], "43200");
  const updated = { ...c, ...body }; assert.equal(publicClientPhase(updated, row), "after");
  assert.equal(publicClientFingerprint({ ...updated, ...publicClientPut(updated, row, false) }), row.beforeSHA256);
  updated.defaultClientScopes.push("admin"); assert.throws(() => publicClientPut(updated, row, false), /PUBLIC_CLIENT_CONFIGURATION_DRIFT/);
});

test("wildcard browser origin, extra callback, wrong PKCE/grant or conflicting base URLs fail closed", () => {
  for (const mutate of [c => { c.webOrigins = ["*"]; }, c => { c.redirectUris.push("https://foreign.example.test/callback"); },
    c => { c.attributes["pkce.code.challenge.method"] = "plain"; }, c => { c.directAccessGrantsEnabled = true; },
    c => { c.rootUrl = "https://foreign.example.test"; }]) {
    const c = client("kodex-control-center", 1); mutate(c); assert.throws(() => publicClientTransition(c, "https://platform.example.test", profile), /PUBLIC_CLIENT_/);
  }
});

async function fixture(t) {
  const dir = await mkdtemp(tmpdir() + "/kodex-public-clients-"); t.after(() => rm(dir, { recursive: true, force: true }));
  const source = "a".repeat(40), trace = [], flags = {}, clients = new Map([["kodex-control-center", client("kodex-control-center", 1)],
    ["kodex-control-center-proxy", client("kodex-control-center-proxy", 2)]]);
  const endpoint = { kind: "ConfigMap", metadata: { name: "kodex-platform-endpoints", namespace: "kodex-system", uid: "endpoint-uid", resourceVersion: "1" },
    data: { publicOrigin: "https://platform.example.test", oidcIssuer: "https://sso.example.test/realms/kodex", oidcJwksUrl: "https://sso.example.test/realms/kodex/protocol/openid-connect/certs",
      oidcConnectAddress: profile.oidcConnectAddress, oidcTlsServerName: "sso.example.test" } };
  const ca = { metadata: { uid: "ca-uid", resourceVersion: "1" }, data: { "ca.crt": "FIXTURE_PUBLIC_CA" } };
  const lb = { Id: "lb-id", State: { Running: true }, Config: { Labels: { "k3d.cluster": "kodex" } },
    NetworkSettings: { Ports: { "443/tcp": [{ HostIp: "127.0.0.2", HostPort: "443" }] } } };
  const command = async (program, argv) => {
    trace.push({ program, argv });
    if (program === "git") return argv[0] === "rev-parse" ? source : "";
    if (program === "docker") return JSON.stringify(lb);
    if (argv.includes("secret")) return argv.at(-1).includes(".data.client-id") ? "credential-uid|1|" + Buffer.from("fixture-client").toString("base64") + "|" +
      Buffer.from("PRIVATE_ADMIN_CLIENT_CREDENTIAL_SENTINEL").toString("base64") : "credential-uid|1";
    if (argv.includes("namespace")) return JSON.stringify({ metadata: { uid: "cluster-uid" } });
    if (argv.includes("kodex-platform-endpoints")) return JSON.stringify(endpoint);
    if (argv.includes("oauth2-oidc-provider-ca")) return JSON.stringify(ca);
    throw Error("UNEXPECTED_FIXTURE_COMMAND");
  };
  const factory = async (origin, ip, publicCA, clientID, secret) => {
    trace.push({ program: "api-connect", origin, ip }); assert.equal(publicCA, ca.data["ca.crt"]);
    assert.equal(clientID, "fixture-client"); assert.equal(secret, "PRIVATE_ADMIN_CLIENT_CREDENTIAL_SENTINEL");
    return async (method, path, body) => {
      trace.push({ program: "api", method, path });
      if (method === "GET" && path.includes("?clientId=")) {
        const name = new URL("https://fixture.invalid" + path).searchParams.get("clientId"), c = clients.get(name);
        return [{ id: c.id, clientId: c.clientId }];
      }
      const c = [...clients.values()].find(c => c.id === path.split("/").at(-1)); assert.ok(c);
      if (method === "GET") return structuredClone(c);
      assert.equal(method, "PUT"); assert.deepEqual(Object.keys(body).sort(), ["attributes", "redirectUris", "webOrigins"]);
      Object.assign(c, structuredClone(body));
      if (flags.lostPut) { flags.lostPut = false; throw Error("FIXTURE_PUT_ACK_LOST"); }
      return null;
    };
  };
  const planPath = dir + "/plan.json";
  const makePlan = async () => publicClients(["plan", "--context", "k3d-kodex", "--output", planPath], command, factory);
  const invoke = async (mode, file, override) => {
    const { plan } = JSON.parse(await readFile(planPath, "utf8"));
    return publicClients([mode, "--context", "k3d-kodex", "--plan", planPath, "--output", dir + "/" + file,
      ...(["continue", "inspect", "readback", "rollback"].includes(mode) ? ["--journal", dir + "/apply.jsonl"] : []),
      ...(["apply", "continue", "rollback"].includes(mode) ? ["--approved-plan-sha", override ?? fingerprint(plan)] : [])], command, factory);
  };
  const writes = () => trace.filter(r => r.program === "api" && r.method === "PUT");
  return { dir, makePlan, invoke, trace, flags, clients, endpoint, ca, writes };
}

test("synthetic Keycloak plan/apply/readback/recovery updates two exact clients and repeated recovery performs no PUT", async t => {
  const f = await fixture(t); await f.makePlan(); assert.equal(f.writes().length, 0);
  await f.invoke("apply", "apply.jsonl"); assert.equal(f.writes().length, 2); await f.invoke("readback", "readback.jsonl");
  await f.invoke("rollback", "rollback.jsonl"); assert.equal(f.writes().length, 4);
  await f.invoke("rollback", "rollback-again.jsonl"); assert.equal(f.writes().length, 4);
  for (const file of ["plan.json", "apply.jsonl", "readback.jsonl", "rollback.jsonl"]) {
    const raw = await readFile(f.dir + "/" + file, "utf8"); assert.equal(raw.includes("PRIVATE_"), false);
  }
  assert.equal(JSON.stringify(f.trace).includes("PRIVATE_"), false);
});

test("wrong owner hash rejects before credential value read or authentication; foreign client/CA/endpoint UID blocks all PUT", async t => {
  const f = await fixture(t); await f.makePlan(); f.trace.length = 0;
  await assert.rejects(f.invoke("apply", "wrong.jsonl", "f".repeat(64)), /PUBLIC_CLIENT_EXACT_OWNER_CONFIRMATION_REQUIRED/);
  assert.equal(f.trace.some(r => r.program === "api-connect" || r.argv?.some(a => a.includes(".data.client-id"))), false);
  for (const mutate of [f => { f.clients.get("kodex-control-center").defaultClientScopes.push("admin"); },
    f => { f.ca.metadata.resourceVersion = "2"; }, f => { f.endpoint.metadata.uid = "foreign"; }]) {
    const f = await fixture(t); await f.makePlan(); mutate(f);
    await assert.rejects(f.invoke("apply", "apply.jsonl"), /PUBLIC_CLIENT_/); assert.equal(f.writes().length, 0);
  }
});

test("lost API PUT response can be inspected but is never retried or automatically overwritten by rollback", async t => {
  const f = await fixture(t); await f.makePlan(); f.flags.lostPut = true;
  await assert.rejects(f.invoke("apply", "apply.jsonl"), /FIXTURE_PUT_ACK_LOST/); assert.equal(f.writes().length, 1);
  await f.invoke("inspect", "inspect.jsonl"); assert.equal(f.writes().length, 1);
  await assert.rejects(f.invoke("rollback", "rollback.jsonl"), /PUBLIC_CLIENT_UNKNOWN_PUT_REQUIRES_OWNER/);
  assert.equal(f.writes().length, 1);
});

test("explicit continuation observes exact completed PUT and writes only the untouched next client", async t => {
  const f = await fixture(t); await f.makePlan(); f.flags.lostPut = true;
  await assert.rejects(f.invoke("apply", "apply.jsonl"), /FIXTURE_PUT_ACK_LOST/);
  await f.invoke("continue", "continued.jsonl"); assert.equal(f.writes().length, 2);
  const rows = (await readFile(f.dir + "/continued.jsonl", "utf8")).trim().split("\n").map(JSON.parse);
  assert.equal(rows[0].mode, "apply"); assert.ok(rows[0].originalJournalSHA256);
  assert.equal(rows.filter(r => r.status === "APPLIED").length, 2);
});

test("lost rollback PUT ACK leaves a canonical recovery claim; another output cannot replay the write", async t => {
  const f = await fixture(t); await f.makePlan(); await f.invoke("apply", "apply.jsonl"); f.flags.lostPut = true;
  await assert.rejects(f.invoke("rollback", "rollback.jsonl"), /FIXTURE_PUT_ACK_LOST/); assert.equal(f.writes().length, 3);
  await assert.rejects(f.invoke("rollback", "rollback-again.jsonl"), /PUBLIC_CLIENT_RECOVERY_ALREADY_STARTED_REQUIRES_OWNER/);
  assert.equal(f.writes().length, 3);
});
