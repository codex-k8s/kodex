#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { open } from "node:fs/promises";
import { dirname, isAbsolute, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createOwnerSessionClient } from "./owner-session-client.mjs";
import { exactOrigin, readAuthenticatedState, sessionHeaders } from "./owner-session-storage.mjs";
import { boundedResponseBody } from "./runtime-workspace-acceptance.mjs";

const root = fileURLToPath(new URL("../../", import.meta.url));
const paths = new Set(["/", "/api/v1/session", "/api/v1/bootstrap"]);
const mediaTypes = new Set(["application/json", "application/problem+json", "text/plain", "text/html"]);
function requireValue(condition) { if (!condition) throw new Error("Release HTTP acceptance configuration is invalid"); }

// Запросы не повторяются после ошибки. Каждая плановая проба и единственный
// запрос refresh фиксируются отдельно; значения cookies и bodies не пишутся.
export async function observeHTTPRelease({ origin, storage, storagePath, durationSeconds, intervalMilliseconds = 1000,
  fetchAPI = fetch, now = Date.now, wait = (milliseconds) => new Promise((done) => setTimeout(done, milliseconds)),
  record = async () => {}, shouldStop = () => false }) {
  origin = exactOrigin(origin);
  requireValue(Number.isSafeInteger(durationSeconds) && durationSeconds > 0 && durationSeconds <= 1800 &&
    Number.isSafeInteger(intervalMilliseconds) && intervalMilliseconds >= 1000 && intervalMilliseconds <= 5000);
  const started = now(), counts = {};
  let failures = 0, rounds = 0, observationFailed = false;
  const observedFetch = async (input, options = {}) => {
    const url = new URL(input, origin), method = options.method ?? "GET";
    requireValue(url.origin === origin && paths.has(url.pathname) && !url.search &&
      (method === "GET" || (method === "PUT" && url.pathname === "/api/v1/session")));
    const began = now();
    let response, status, responseType = "OTHER";
    try {
      response = await fetchAPI(url, options);
      status = response.status;
      const type = response.headers.get("content-type")?.split(";")[0].trim();
      if (mediaTypes.has(type)) responseType = type;
    } catch { status = "TRANSPORT_ERROR"; }
    const key = `${url.pathname}|${method}|${status}`;
    counts[key] = (counts[key] ?? 0) + 1;
    if (status !== 200) failures++;
    await record({ kind: "HTTP", at: new Date(now()).toISOString(), elapsedMilliseconds: now() - started,
      path: url.pathname, method, status, responseType, milliseconds: now() - began });
    if (!response) throw new Error("Release HTTP acceptance transport failed");
    return response;
  };
  const client = createOwnerSessionClient({ origin, storage, storagePath, fetchAPI: observedFetch, now });
  while (now() - started < durationSeconds * 1000 && !shouldStop()) {
    try {
      // Existing owner client выполняет своевременный CAS refresh без retry.
      // Не больше 72 защищённых GET/минуту при интервале 1 секунда.
      if (rounds % 5 === 0) {
        const response = await client.request("/api/v1/bootstrap", { signal: AbortSignal.timeout(15000) });
        await boundedResponseBody(response, 1 << 20);
      }
      const headers = sessionHeaders({ cookies: client.authenticatedCookies(), origins: [] }, origin, now());
      for (const path of ["/api/v1/session", "/"]) {
        const response = await observedFetch(new URL(path, origin), { method: "GET", headers, redirect: "manual", signal: AbortSignal.timeout(5000) });
        await boundedResponseBody(response, 1 << 20);
        if (response.status === 401) throw new Error("Release HTTP acceptance session is unavailable");
      }
    } catch {
      observationFailed = true;
      await record({ kind: "OBSERVATION_FAILURE", at: new Date(now()).toISOString(), code: "SESSION_OR_RESPONSE_UNAVAILABLE" });
      break;
    }
    rounds++;
    await wait(intervalMilliseconds);
  }
  return { status: failures === 0 && !observationFailed && rounds > 0 ? "PASS" : "FAIL", counts,
    durationMilliseconds: now() - started, rounds, observationFailed, operatorStopped: shouldStop() };
}

async function main() {
  const args = process.argv.slice(2);
  requireValue(args.length === 4 && args[0] === "--expected-sha" && /^[a-f0-9]{40}$/.test(args[1]) && args[2] === "--seconds" &&
    process.env.KODEX_E2E_CONFIRM_DISPOSABLE === "I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION");
  const seconds = Number(args[3]);
  requireValue(Number.isSafeInteger(seconds) && seconds >= 30 && seconds <= 1800);
  const git = (...options) => execFileSync("git", options, { cwd: root, encoding: "utf8", timeout: 5000, stdio: ["ignore", "pipe", "pipe"] }).trim();
  requireValue(git("rev-parse", "HEAD") === args[1] && git("status", "--porcelain", "--untracked-files=all") === "");
  const origin = exactOrigin(process.env.KODEX_E2E_BASE_URL);
  const directory = process.env.KODEX_E2E_STATE_DIRECTORY ?? "", storagePath = process.env.KODEX_E2E_STORAGE_STATE ?? "";
  const prefix = process.env.KODEX_E2E_RESOURCE_PREFIX ?? "";
  requireValue(isAbsolute(directory) && dirname(storagePath) === directory && /^[a-z0-9][a-z0-9-]{2,38}[a-z0-9]$/.test(prefix));
  const storage = readAuthenticatedState(storagePath);
  const stopPath = `${directory}/${prefix}-release-http.stop`;
  requireValue(!existsSync(stopPath));
  const log = await open(`${directory}/${prefix}-release-http.jsonl`, "wx", 0o600);
  const record = async (value) => { await log.writeFile(`${JSON.stringify(value)}\n`); await log.sync(); };
  let interrupted = false;
  for (const signal of ["SIGINT", "SIGTERM"]) process.once(signal, () => { interrupted = true; });
  try {
    await record({ kind: "START", toolSourceSHA: args[1], requestedSeconds: seconds, at: new Date().toISOString() });
    const result = await observeHTTPRelease({ origin, storage, storagePath, durationSeconds: seconds, record,
      shouldStop: () => interrupted || existsSync(stopPath) });
    await record({ kind: "RESULT", ...result });
    process.stdout.write(`${JSON.stringify(result)}\n`);
    if (result.status !== "PASS") process.exitCode = 1;
  } finally { await log.close(); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { await main(); }
  catch { process.stderr.write("Release HTTP acceptance failed; no automatic retry was performed\n"); process.exitCode = 1; }
}
