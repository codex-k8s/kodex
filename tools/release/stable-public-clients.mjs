#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { lstat, open, readFile } from "node:fs/promises";
import { Agent, request } from "node:https";
import { dirname } from "node:path";
import { connect } from "node:tls";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { publicEndpoints, publicIdentityProfile } from "./stable-public-identity-model.mjs";
import { publicClientPhase, publicClientPut, publicClientTransition } from "./stable-public-clients-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }

// Точный loopback upstream доказан Docker binding выбранного Kodex. HTTPS
// проверяет исходный SNI/CA; DNS, redirects и пользовательский proxy не нужны.
export async function keycloakAPI(origin, connectIP, ca, clientID, clientSecret) {
  const host = new URL(origin).hostname;
  const send = (method, path, body, headers = {}) => new Promise((resolve, reject) => {
    const agent = new Agent({ keepAlive: false, maxSockets: 1 });
    let timer; const fail = () => { clearTimeout(timer); agent.destroy(); reject(Error("PUBLIC_CLIENT_HTTPS_FAILED_OR_UNKNOWN")); };
    agent.createConnection = () => connect({ host: connectIP, port: 443, servername: host, ca, rejectUnauthorized: true, ALPNProtocols: ["http/1.1"] });
    const req = request({ hostname: host, port: 443, method, path, agent,
      headers: { ...headers, ...(body ? { "Content-Length": Buffer.byteLength(body) } : {}) } }, res => {
      const chunks = []; let size = 0;
      res.on("data", chunk => { size += chunk.length; if (size > (2 << 20)) req.destroy(Error("PUBLIC_CLIENT_RESPONSE_TOO_LARGE")); else chunks.push(chunk); });
      res.on("end", () => {
        clearTimeout(timer); agent.destroy();
        if (![200, 204].includes(res.statusCode)) return reject(Error("PUBLIC_CLIENT_HTTP_STATUS_" + String(res.statusCode)));
        try { resolve(res.statusCode === 204 ? null : JSON.parse(Buffer.concat(chunks).toString("utf8"))); }
        catch { reject(Error("PUBLIC_CLIENT_RESPONSE_INVALID")); }
      });
      res.on("error", fail);
    });
    req.on("error", fail); timer = setTimeout(() => req.destroy(), 20000);
    req.setTimeout(15000, () => req.destroy()); if (body) req.write(body); req.end();
  });
  const token = await send("POST", "/realms/master/protocol/openid-connect/token", new URLSearchParams({ grant_type: "client_credentials",
    client_id: clientID, client_secret: clientSecret }).toString(), { "Content-Type": "application/x-www-form-urlencoded" });
  requireValue(/^[A-Za-z0-9._~-]{1,32768}$/.test(token?.access_token ?? ""), "PUBLIC_CLIENT_AUTHENTICATION_FAILED");
  return (method, path, body) => send(method, path, body ? JSON.stringify(body) : undefined,
    { Authorization: "Bearer " + token.access_token, ...(body ? { "Content-Type": "application/json" } : {}) });
}

export async function publicClients(args, port, apiPort = keycloakAPI) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift(); requireValue(["--context", "--output", "--plan", "--journal", "--tls-phase", "--approved-plan-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "PUBLIC_CLIENT_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  requireValue(["plan", "apply", "inspect", "readback", "rollback"].includes(mode) && options["--context"] === "k3d-kodex" &&
    options["--output"]?.startsWith("/") && (mode === "plan" ? !options["--plan"] && !options["--journal"] : options["--plan"]) &&
    (["inspect", "readback", "rollback"].includes(mode) ? options["--journal"] : !options["--journal"]) &&
    ["before", "after"].includes(options["--tls-phase"] ?? "before"), "PUBLIC_CLIENT_ARGUMENTS_INVALID");
  const parent = await lstat(dirname(options["--output"]));
  requireValue(parent.isDirectory() && parent.uid === process.getuid() && (parent.mode & 0o777) === 0o700, "PUBLIC_CLIENT_PRIVATE_DIRECTORY_REQUIRED");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv) => {
    try { return (await execute(program, argv, { cwd: root, env, timeout: 30000, maxBuffer: 4 << 20 })).stdout; }
    catch { throw Error("PUBLIC_CLIENT_PREFLIGHT_FAILED"); }
  });
  const source = (await run("git", ["rev-parse", "HEAD"])).trim(), context = options["--context"];
  const assertSource = async () => requireValue(/^[a-f0-9]{40}$/.test(source) && (await run("git", ["rev-parse", "HEAD"])).trim() === source &&
    !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(), "PUBLIC_CLIENT_CLEAN_SOURCE_REQUIRED");
  await assertSource();
  const kube = argv => run("kubectl", ["--context", context, "--request-timeout=15s", ...argv]);
  const get = async (kind, name, namespace) => JSON.parse(await kube([...(namespace ? ["-n", namespace] : []), "get", kind, name, "-o", "json"]));
  const profile = JSON.parse(await readFile(root + "infra/public-ingress/identity-profile.json", "utf8")); publicIdentityProfile(profile);
  const clusterUID = (await get("namespace", "kube-system")).metadata.uid, sourceEndpoints = await get("configmap", profile.endpointsName, "kodex-system");
  const certificateCA = await get("configmap", "oauth2-oidc-provider-ca", "kodex-system");
  requireValue(certificateCA.metadata.uid && certificateCA.metadata.resourceVersion && typeof certificateCA.data?.["ca.crt"] === "string" &&
    Object.keys(certificateCA.data).length === 1 && !Object.keys(certificateCA.binaryData ?? {}).length, "PUBLIC_CLIENT_CA_INVALID");
  const lb = JSON.parse(await run("docker", ["inspect", "--format", "{{json .}}", "k3d-kodex-serverlb"]));
  const bindings = lb.NetworkSettings?.Ports?.["443/tcp"];
  requireValue(lb.Id && lb.State?.Running === true && lb.Config?.Labels?.["k3d.cluster"] === "kodex" &&
    bindings?.length === 1 && bindings[0].HostIp === "127.0.0.2" && bindings[0].HostPort === "443", "PUBLIC_CLIENT_EXACT_LOOPBACK_BINDING_REQUIRED");
  const credentialMetadata = async () => (await kube(["-n", "identity", "get", "secret", "keycloak-admin-client", "-o",
    'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}'])).split("|");
  const fields = await credentialMetadata();
  requireValue(fields.length === 2 && fields[0] && /^[0-9]+$/.test(fields[1]), "PUBLIC_CLIENT_CREDENTIAL_INVALID");
  const boundary = { clusterUID, loadBalancerID: lb.Id, caUID: certificateCA.metadata.uid, caResourceVersion: certificateCA.metadata.resourceVersion,
    caSHA256: fingerprint(certificateCA.data), credentialUID: fields[0], credentialResourceVersion: fields[1] };
  let plan, hash;
  if (mode === "plan") {
    requireValue(!options["--approved-plan-sha"] && (!options["--tls-phase"] || options["--tls-phase"] === "before"), "PUBLIC_CLIENT_ARGUMENTS_INVALID");
    plan = { schema: "kodex.public-clients-plan/v1", source, context, profileSHA256: fingerprint(profile), boundary,
      endpoints: publicEndpoints(sourceEndpoints, profile), rows: [], apply: "NOT_RUN" };
  } else {
    const document = await readPrivateJSON(options["--plan"]); plan = document.plan; hash = fingerprint(plan);
    requireValue(plan.schema === "kodex.public-clients-plan/v1" && document.planSHA256 === hash && plan.source === source && plan.context === context &&
      plan.profileSHA256 === fingerprint(profile) && fingerprint(plan.boundary) === fingerprint(boundary) && plan.rows?.length === 2 &&
      plan.rows.map(r => r.clientId).join(",") === "kodex-control-center,kodex-control-center-proxy" &&
      sourceEndpoints.metadata.uid === plan.endpoints.uid &&
      [fingerprint(plan.endpoints.before), fingerprint(plan.endpoints.after)].includes(fingerprint(sourceEndpoints.data)), "PUBLIC_CLIENT_PLAN_DRIFT");
    requireValue(["apply", "rollback"].includes(mode) ? options["--approved-plan-sha"] === hash : !options["--approved-plan-sha"], "PUBLIC_CLIENT_EXACT_OWNER_CONFIRMATION_REQUIRED");
  }
  const assertBoundary = async () => {
    await assertSource();
    const ns = await get("namespace", "kube-system"), cm = await get("configmap", profile.endpointsName, "kodex-system"), credential = await credentialMetadata();
    const ca = await get("configmap", "oauth2-oidc-provider-ca", "kodex-system");
    const actualLB = JSON.parse(await run("docker", ["inspect", "--format", "{{json .}}", "k3d-kodex-serverlb"]));
    requireValue(ns.metadata.uid === boundary.clusterUID && cm.metadata.uid === plan.endpoints.uid &&
      [fingerprint(plan.endpoints.before), fingerprint(plan.endpoints.after)].includes(fingerprint(cm.data)) &&
      ca.metadata.uid === boundary.caUID && ca.metadata.resourceVersion === boundary.caResourceVersion && fingerprint(ca.data) === boundary.caSHA256 &&
      credential[0] === boundary.credentialUID && credential[1] === boundary.credentialResourceVersion &&
      actualLB.Id === boundary.loadBalancerID && actualLB.State?.Running === true &&
      fingerprint(actualLB.NetworkSettings?.Ports?.["443/tcp"]) === fingerprint(bindings), "PUBLIC_CLIENT_BOUNDARY_DRIFT");
  };
  const intents = new Set(), acknowledged = new Set(); let originalJournalSHA256, recoveryComplete = false;
  if (options["--journal"]) {
    const f = await open(options["--journal"], constants.O_RDONLY | constants.O_NOFOLLOW); let records;
    try {
      const s = await f.stat(); requireValue(s.isFile() && s.uid === process.getuid() && (s.mode & 0o777) === 0o600 && s.nlink === 1 && s.size > 0 && s.size < (1 << 20), "PUBLIC_CLIENT_JOURNAL_INVALID");
      records = (await f.readFile("utf8")).trim().split("\n").map(JSON.parse);
    } finally { await f.close(); }
    originalJournalSHA256 = fingerprint(records);
    const header = records.shift(); requireValue(header.schema === "kodex.public-clients-journal/v1" && header.mode === "apply" && header.planSHA256 === hash && records.length <= 16,
      "PUBLIC_CLIENT_JOURNAL_INVALID");
    if (mode === "rollback") requireValue(["COMPLETE", "UNKNOWN"].includes(records.at(-1)?.status), "PUBLIC_CLIENT_ORIGINAL_EXECUTOR_NOT_TERMINAL");
    for (const r of records) {
      requireValue(["APPLY_INTENT", "APPLIED", "UNKNOWN", "COMPLETE"].includes(r.status), "PUBLIC_CLIENT_JOURNAL_INVALID");
      if (r.status === "APPLY_INTENT") { requireValue(plan.rows.some(row => row.clientId === r.clientId) && !intents.has(r.clientId), "PUBLIC_CLIENT_JOURNAL_INVALID"); intents.add(r.clientId); }
      if (r.status === "APPLIED") { requireValue(intents.has(r.clientId), "PUBLIC_CLIENT_JOURNAL_INVALID"); acknowledged.add(r.clientId); }
    }
  }
  const recoveryPath = options["--journal"] + ".recovery.jsonl";
  if (mode === "rollback") {
    const parent = await lstat(dirname(options["--journal"]));
    requireValue(parent.isDirectory() && parent.uid === process.getuid() && (parent.mode & 0o777) === 0o700, "PUBLIC_CLIENT_PRIVATE_DIRECTORY_REQUIRED");
    let file;
    try { file = await open(recoveryPath, constants.O_RDONLY | constants.O_NOFOLLOW); }
    catch (error) { if (error.code !== "ENOENT") throw Error("PUBLIC_CLIENT_RECOVERY_JOURNAL_INVALID"); }
    if (file) try {
      const s = await file.stat(); requireValue(s.isFile() && s.uid === process.getuid() && (s.mode & 0o777) === 0o600 && s.nlink === 1 && s.size > 0 && s.size < (1 << 20),
        "PUBLIC_CLIENT_RECOVERY_JOURNAL_INVALID");
      const records = (await file.readFile("utf8")).trim().split("\n").map(JSON.parse), header = records.shift();
      requireValue(header.schema === "kodex.public-clients-recovery/v1" && header.planSHA256 === hash && header.originalJournalSHA256 === originalJournalSHA256 &&
        records.length <= 8 && records.every(r => ["ROLLBACK_INTENT", "RESTORED", "UNKNOWN", "COMPLETE"].includes(r.status)), "PUBLIC_CLIENT_RECOVERY_JOURNAL_INVALID");
      requireValue(records.at(-1)?.status === "COMPLETE", "PUBLIC_CLIENT_RECOVERY_ALREADY_STARTED_REQUIRES_OWNER"); recoveryComplete = true;
    } finally { await file.close(); }
  }
  // Значения читаются только после plan/journal/approval validation. Два
  // заранее известных key остаются в памяти, не в argv/env/plan/journal.
  const credentialRaw = await kube(["-n", "identity", "get", "secret", "keycloak-admin-client", "-o",
    'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.data.client-id}{"|"}{.data.client-secret}']);
  const credential = credentialRaw.split("|"), clientID = Buffer.from(credential[2] ?? "", "base64").toString("utf8"),
    clientSecret = Buffer.from(credential[3] ?? "", "base64").toString("utf8");
  requireValue(credential.length === 4 && credential[0] === boundary.credentialUID && credential[1] === boundary.credentialResourceVersion &&
    /^[a-zA-Z0-9._-]{1,128}$/.test(clientID) && clientSecret.length >= 32 && clientSecret.length <= 4096 && !/[\r\n]/.test(clientSecret),
    "PUBLIC_CLIENT_CREDENTIAL_INVALID");
  const origin = new URL((options["--tls-phase"] ?? "before") === "before" ? plan.endpoints.before.oidcIssuer : plan.endpoints.after.oidcIssuer).origin;
  const api = await apiPort(origin, bindings[0].HostIp, certificateCA.data["ca.crt"], clientID, clientSecret);
  const read = async name => {
    const found = await api("GET", "/admin/realms/kodex/clients?clientId=" + encodeURIComponent(name));
    requireValue(Array.isArray(found) && found.length === 1 && found[0].clientId === name && typeof found[0].id === "string", "PUBLIC_CLIENT_LOOKUP_INVALID");
    const value = await api("GET", "/admin/realms/kodex/clients/" + encodeURIComponent(found[0].id));
    requireValue(value?.id === found[0].id && value.clientId === name, "PUBLIC_CLIENT_LOOKUP_INVALID"); return value;
  };
  const current = new Map();
  for (const name of ["kodex-control-center", "kodex-control-center-proxy"]) {
    const value = await read(name); current.set(name, value);
    if (mode === "plan") plan.rows.push(publicClientTransition(value, plan.endpoints.before.publicOrigin, profile));
    else {
      const row = plan.rows.find(r => r.clientId === name), phase = publicClientPhase(value, row);
      requireValue(phase === "before" || intents.has(name), "PUBLIC_CLIENT_UNOWNED_AFTER_STATE");
      if (mode === "apply") requireValue(phase === "before", "PUBLIC_CLIENT_PREDECESSOR_INVALID");
      if (mode === "readback") requireValue(phase === "after", "PUBLIC_CLIENT_READBACK_INCOMPLETE");
      if (mode === "rollback" && intents.has(name)) requireValue(acknowledged.has(name), "PUBLIC_CLIENT_UNKNOWN_PUT_REQUIRES_OWNER");
      if (recoveryComplete) requireValue(phase === "before", "PUBLIC_CLIENT_COMPLETED_RECOVERY_DRIFT");
    }
  }
  if (mode === "plan") for (const row of plan.rows) requireValue(publicClientPhase(await read(row.clientId), row) === "before", "PUBLIC_CLIENT_SOURCE_CHANGED_DURING_PLAN");
  await assertBoundary();
  let recoveryFile;
  if (mode === "rollback" && !recoveryComplete) {
    try { recoveryFile = await open(recoveryPath, "wx", 0o600); }
    catch { throw Error("PUBLIC_CLIENT_RECOVERY_ALREADY_STARTED_REQUIRES_OWNER"); }
    try {
      await recoveryFile.writeFile(JSON.stringify({ schema: "kodex.public-clients-recovery/v1", planSHA256: hash, originalJournalSHA256 }) + "\n"); await recoveryFile.sync();
    } catch (error) { await recoveryFile.close(); throw error; }
  }
  let output;
  try { output = await open(options["--output"], "wx", 0o600); }
  catch (error) { await recoveryFile?.close(); throw error; }
  const record = async row => {
    const raw = JSON.stringify(row) + "\n"; await output.writeFile(raw); await output.sync();
    if (recoveryFile && !row.schema) { await recoveryFile.writeFile(raw); await recoveryFile.sync(); }
  };
  try {
    if (mode === "plan") { await record({ plan, planSHA256: fingerprint(plan) }); return { plan }; }
    await record({ schema: "kodex.public-clients-journal/v1", mode, planSHA256: hash });
    if (["inspect", "readback"].includes(mode)) { await record({ status: mode === "inspect" ? "OBSERVED" : "SERVING", clients: plan.rows.map(row =>
      ({ clientId: row.clientId, phase: publicClientPhase(current.get(row.clientId), row) })) }); return { plan }; }
    for (const row of (mode === "apply" ? plan.rows : [...plan.rows].reverse())) {
      const client = await read(row.clientId), phase = publicClientPhase(client, row);
      if (mode === "rollback" && phase === "before") continue;
      await assertBoundary();
      await record({ status: mode === "apply" ? "APPLY_INTENT" : "ROLLBACK_INTENT", clientId: row.clientId });
      await api("PUT", "/admin/realms/kodex/clients/" + row.id, publicClientPut(client, row, mode === "apply"));
      requireValue(publicClientPhase(await read(row.clientId), row) === (mode === "apply" ? "after" : "before"), "PUBLIC_CLIENT_PUT_RESULT_UNKNOWN");
      await record({ status: mode === "apply" ? "APPLIED" : "RESTORED", clientId: row.clientId });
    }
    await record({ status: "COMPLETE" }); return { plan };
  } catch (error) { await record({ status: "UNKNOWN" }); throw error; }
  finally { await output.close(); await recoveryFile?.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  publicClients(process.argv.slice(2)).then(r => console.log(JSON.stringify({ status: "PASS", phase: process.argv[2], source: r.plan.source,
    planSHA256: fingerprint(r.plan), clients: r.plan.rows.length, ...(process.argv[2] === "plan" ? { apply: "NOT_RUN" } : {}) })))
    .catch(error => { console.error(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_0-9]+$/.test(error.message ?? "") ? error.message : "PUBLIC_CLIENT_OPERATION_FAILED" })); process.exitCode = 1; });
