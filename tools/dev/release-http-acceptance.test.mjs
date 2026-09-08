import test from "node:test";
import assert from "node:assert/strict";
import { observeHTTPRelease } from "./release-http-acceptance.mjs";

const origin = "https://control.disposable.invalid";
const storage = { origins: [], cookies: [
  { name: "__Host-kodex-session", value: `v1.${"s".repeat(64)}`, domain: "control.disposable.invalid", path: "/", secure: true, httpOnly: true, sameSite: "Strict", expires: -1 },
  { name: "__Host-kodex-csrf", value: "c".repeat(43), domain: "control.disposable.invalid", path: "/", secure: true, httpOnly: false, sameSite: "Strict", expires: -1 },
] };

async function run({ failedProbe = false, failedRefresh = false, failedHTML = false } = {}) {
  let clock = Date.now(), sessionReads = 0;
  const rows = [];
  const fetchAPI = async (url, options) => {
    const requested = new Headers(options.headers);
    assert.equal(requested.get("cookie")?.includes("__Host-kodex-session="), true);
    if (url.pathname === "/") {
      const accepted = requested.get("accept") === "text/html" && !failedHTML;
      return new Response(accepted ? "<!doctype html><title>Fixture</title>" : "private fixture", {
        status: accepted ? 200 : 404, headers: { "Content-Type": "text/html" },
      });
    }
    assert.equal(requested.get("accept"), "application/json");
    const headers = new Headers({ "Content-Type": "application/json", "Cache-Control": "no-store" });
    let value = {};
    if (url.pathname === "/api/v1/session") {
      if (options.method === "GET") sessionReads++;
      if ((failedProbe && sessionReads === 3 && options.method === "GET") || (failedRefresh && options.method === "PUT"))
        return new Response("synthetic private error", { status: 502, headers });
      const renewed = options.method === "PUT";
      if (renewed) for (const cookie of storage.cookies) headers.append("Set-Cookie", `${cookie.name}=${cookie.httpOnly ? `v1.${"n".repeat(64)}` : "n".repeat(43)}; Path=/; Secure; SameSite=Strict; Max-Age=1800${cookie.httpOnly ? "; HttpOnly" : ""}`);
      value = { generation: "11111111-1111-4111-8111-111111111111", version: renewed ? 2 : 1, sessionRevision: 2,
        renewalMode: "BACKEND_REFRESH", serverTime: new Date(clock).toISOString(), accessExpiresAt: new Date(clock + 600000).toISOString(),
        expiresAt: new Date(clock + 1800000).toISOString(), absoluteExpiresAt: new Date(clock + 3600000).toISOString(),
        renewAfter: new Date(clock + (renewed ? 480000 : 0)).toISOString() };
    }
    return new Response(JSON.stringify(value), { headers });
  };
  const result = await observeHTTPRelease({ origin, storage, durationSeconds: 6, fetchAPI, now: () => clock,
    wait: async (milliseconds) => { clock += milliseconds; }, record: async (row) => { rows.push(row); } });
  return { result, rows };
}

test("release probes renew before expiry and record the renewal separately", async () => {
  const { result, rows } = await run();
  assert.equal(result.status, "PASS");
  assert.equal(result.counts["/api/v1/session|PUT|200"], 1);
  assert.equal(result.rounds, 6);
  assert.equal(JSON.stringify(rows).includes("v1."), false);
});

test("a failed probe remains visible without an immediate repeat", async () => {
  const { result, rows } = await run({ failedProbe: true });
  assert.equal(result.status, "FAIL");
  assert.equal(result.counts["/api/v1/session|GET|502"], 1);
  assert.equal(result.rounds, 6);
  const requests = rows.filter((row) => row.path === "/api/v1/session" && row.method === "GET");
  assert.equal(requests.length, 7);
  assert.equal(JSON.stringify(rows).includes("synthetic private error"), false);
});

test("uncertain refresh failure stops observation without a second PUT", async () => {
  const { result } = await run({ failedRefresh: true });
  assert.equal(result.status, "FAIL");
  assert.equal(result.counts["/api/v1/session|PUT|502"], 1);
  assert.equal(result.observationFailed, true);
});

test("HTML negotiation is distinct from JSON and real HTML failures remain failures", async () => {
  const healthy = await run();
  assert.equal(healthy.result.counts["/|GET|200"], 6);
  assert.equal(healthy.rows.filter((row) => row.path === "/").every((row) => row.responseType === "text/html"), true);
  const failed = await run({ failedHTML: true });
  assert.equal(failed.result.status, "FAIL");
  assert.equal(failed.result.counts["/|GET|404"], 6);
  assert.equal(failed.result.rounds, 6);
  assert.equal(JSON.stringify(failed.rows).includes("private fixture"), false);
});
