import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { createServer } from "node:https";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { createOwnerRuntimeHTTPS } from "./owner-runtime-https.mjs";
import { proxyCookieName } from "./owner-session-storage.mjs";

function cert(t) {
  const dir = mkdtempSync(join(tmpdir(), "kodex-owner-https-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  execFileSync(
    "openssl",
    [
      "req",
      "-x509",
      "-nodes",
      "-newkey",
      "rsa:2048",
      "-days",
      "1",
      "-subj",
      "/CN=localhost",
      "-addext",
      "subjectAltName=DNS:localhost",
      "-keyout",
      join(dir, "key.pem"),
      "-out",
      join(dir, "cert.pem"),
    ],
    { stdio: "ignore", timeout: 10000 },
  );
  return {
    key: readFileSync(join(dir, "key.pem")),
    cert: readFileSync(join(dir, "cert.pem")),
  };
}
async function fixture(t, options, handler) {
  const server = createServer(options, handler);
  server.on("tlsClientError", () => {});
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(
    () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(resolve);
      }),
  );
  return `https://localhost:${server.address().port}`;
}
const options = (signal) => ({ redirect: "error", signal });

test(
  "actual TLS1.3 verifies exact CA/SNI, preserves cookie headers and joins owned sockets",
  { timeout: 15000 },
  async (t) => {
    const material = cert(t);
    let calls = 0;
    const origin = await fixture(
      t,
      { ...material, minVersion: "TLSv1.3" },
      (req, res) => {
        calls++;
        assert.equal(req.socket.getProtocol(), "TLSv1.3");
        res.setHeader("Set-Cookie", [
          "first=synthetic; Secure",
          "second=synthetic; Secure",
        ]);
        res.end("safe");
      },
    );
    const transport = createOwnerRuntimeHTTPS({ ca: material.cert });
    t.after(() => transport.close(AbortSignal.timeout(1000)));
    const response = await transport.fetchAPI(
      origin + "/",
      options(AbortSignal.timeout(3000)),
    );
    assert.equal(await response.text(), "safe");
    assert.equal(response.headers.getSetCookie().length, 2);
    assert.equal(calls, 1);
    await assert.rejects(
      transport.fetchAPI(
        origin.replace("localhost", "127.0.0.1") + "/",
        options(AbortSignal.timeout(3000)),
      ),
    );
    assert.equal(calls, 1);
  },
);

test(
  "TLS1.2 is refused before the HTTP handler; no plaintext or redirect fallback",
  { timeout: 15000 },
  async (t) => {
    const material = cert(t);
    let calls = 0;
    const origin = await fixture(
      t,
      { ...material, minVersion: "TLSv1.2", maxVersion: "TLSv1.2" },
      (_req, res) => {
        calls++;
        res.end("unexpected");
      },
    );
    const transport = createOwnerRuntimeHTTPS({ ca: material.cert });
    t.after(() => transport.close(AbortSignal.timeout(1000)));
    await assert.rejects(
      transport.fetchAPI(origin + "/", options(AbortSignal.timeout(3000))),
    );
    await assert.rejects(
      transport.fetchAPI(
        origin.replace("https:", "http:") + "/",
        options(AbortSignal.timeout(3000)),
      ),
    );
    assert.equal(calls, 0);
  },
);

test(
  "cancel after response headers terminates the body instead of reporting clean EOF",
  { timeout: 15000 },
  async (t) => {
    const material = cert(t);
    const origin = await fixture(
      t,
      { ...material, minVersion: "TLSv1.3" },
      (_req, res) => {
        res.writeHead(200);
        res.write("prefix");
      },
    );
    const transport = createOwnerRuntimeHTTPS({ ca: material.cert });
    t.after(() => transport.close(AbortSignal.timeout(1000)));
    const controller = new AbortController(),
      response = await transport.fetchAPI(
        origin + "/",
        options(controller.signal),
      ),
      reader = response.body.getReader();
    assert.equal(
      new TextDecoder().decode((await reader.read()).value),
      "prefix",
    );
    controller.abort();
    await assert.rejects(reader.read());
  },
);

test(
  "bounded HTTPS headers accept a full session cookie generation and reject oversized headers without retry",
  { timeout: 15000 },
  async (t) => {
    const material = cert(t);
    let calls = 0;
    const cookies = [
      ...Array.from(
        { length: 4 },
        (_, index) =>
          `${proxyCookieName}_${index}=${"x".repeat(4096)}; Path=/; Secure; HttpOnly; SameSite=Lax`,
      ),
      `__Host-kodex-session=${"s".repeat(4096)}; Path=/; Secure; HttpOnly; SameSite=Lax`,
      `__Host-kodex-csrf=${"c".repeat(43)}; Path=/; Secure; SameSite=Lax`,
    ];
    assert.ok(Buffer.byteLength(cookies.join("\r\n")) > 16384);
    const origin = await fixture(
      t,
      { ...material, minVersion: "TLSv1.3" },
      (req, res) => {
        calls++;
        if (req.url === "/session") res.setHeader("Set-Cookie", cookies);
        else res.setHeader("X-Oversized-Fixture", "x".repeat(65536));
        res.end("safe");
      },
    );
    const transport = createOwnerRuntimeHTTPS({ ca: material.cert });
    t.after(() => transport.close(AbortSignal.timeout(1000)));
    const response = await transport.fetchAPI(
      origin + "/session",
      options(AbortSignal.timeout(3000)),
    );
    assert.deepEqual(response.headers.getSetCookie(), cookies);
    assert.equal(await response.text(), "safe");
    await assert.rejects(
      transport.fetchAPI(
        origin + "/oversized",
        options(AbortSignal.timeout(3000)),
      ),
    );
    assert.equal(calls, 2);
  },
);
