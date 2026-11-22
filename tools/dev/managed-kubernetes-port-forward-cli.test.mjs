import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { once } from "node:events";
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { createServer } from "node:https";
import { connect } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { test } from "node:test";
import {
  input,
  connection,
  page,
  submission,
} from "../../services/staff/control-center/src/shared/api/kubernetes-port-forward-fixtures.test-support.ts";
import { encodePortForwardInput } from "../../services/staff/control-center/src/shared/lib/kubernetes-port-forward-frame.ts";
import { parsePortForwardArguments } from "./managed-kubernetes-port-forward.mjs";

const command = new URL(
  "./managed-kubernetes-port-forward.mjs",
  import.meta.url,
).pathname;
async function child(t, args) {
  const process = spawn(globalThis.process.execPath, [command, ...args], {
    stdio: ["ignore", "pipe", "pipe"],
  });
  let out = "",
    err = "";
  const lines = [];
  let waiter;
  process.stdout.setEncoding("utf8");
  process.stderr.setEncoding("utf8");
  process.stdout.on("data", (body) => {
    out += body;
    while (out.includes("\n")) {
      const end = out.indexOf("\n"),
        line = out.slice(0, end);
      out = out.slice(end + 1);
      lines.push(line);
      waiter?.();
    }
  });
  process.stderr.on("data", (body) => {
    err += body;
  });
  const exited = once(process, "exit");
  let joined = false;
  exited.then(() => (joined = true));
  t.after(async () => {
    if (joined) return;
    process.kill("SIGTERM");
    await Promise.race([exited, delay(1000)]);
    if (!joined) process.kill("SIGKILL");
    await exited;
  });
  return {
    process,
    exited,
    async line() {
      while (!lines.length) {
        await Promise.race([
          new Promise((resolve) => {
            waiter = resolve;
          }),
          exited.then(() => {
            throw Error("owned CLI exited before metadata");
          }),
        ]);
      }
      return JSON.parse(lines.shift());
    },
    diagnostics: () => err,
  };
}

test(
  "CLI argument boundary rejects remote addresses, raw token flags and ambiguous mapping",
  { timeout: 1000 },
  () => {
    const valid = [
      "forward",
      "--origin",
      "https://control.invalid",
      "--session-file",
      "/private/session",
      "--run-ref",
      "run_synthetic",
      "--node-ref",
      "nod_synthetic",
      "--invocation-ref",
      "inv_synthetic",
      "--stream-ref",
      "kst_synthetic",
      "--map",
      "0:0",
    ];
    assert.equal(parsePortForwardArguments(valid).mappings[0].localPort, 0);
    for (const extras of [
      ["--host", "0.0.0.0"],
      ["--remote-port", "22"],
      ["--token", "private"],
      ["--map", "0:1"],
      ["--map", "16:1"],
      ["--map", "1:65536"],
    ])
      assert.throws(() => parsePortForwardArguments([...valid, ...extras]));
  },
);

test(
  "actual CLI token-file→session→HTTPS owner APIs→loopback TCP round-trip",
  { timeout: 15000 },
  async (t) => {
    const dir = mkdtempSync(join(tmpdir(), "kodex-pf-cli-"));
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
        join(dir, "ca.pem"),
      ],
      { stdio: "ignore", timeout: 10000 },
    );
    const marker = "synthetic-owner-token-".repeat(4),
      tokenFile = join(dir, "token.json"),
      stateFile = join(dir, "session.json");
    let view = {
        ...input(),
        maximumStagedBytes: 1073741824,
        maximumStagedObjects: 32768,
        hardDeadline: new Date(Date.now() + 60000).toISOString(),
      },
      conn = { ...connection(), hardDeadline: view.hardDeadline },
      submitted = 0,
      opens = 0,
      logins = 0,
      ordinal = 0n,
      outputs = [],
      lastCursor = 0n;
    const loginCookies = [
      "__Host-kodex-session=v1." +
        "1".repeat(64) +
        "; Path=/; Max-Age=1800; HttpOnly; Secure; SameSite=Strict",
      "__Host-kodex-csrf=" +
        "1".repeat(43) +
        "; Path=/; Max-Age=1800; Secure; SameSite=Strict",
    ];
    function emit(action, body = new Uint8Array()) {
      const wire = Buffer.from(
        encodePortForwardInput(
          conn,
          action === 2 ? body : new Uint8Array(),
          action !== 2,
        ),
      );
      wire[4] = action;
      wire.writeBigUInt64BE(++ordinal, 43);
      outputs.push(wire);
    }
    const server = createServer(
      {
        key: readFileSync(join(dir, "key.pem")),
        cert: readFileSync(join(dir, "ca.pem")),
        minVersion: "TLSv1.3",
      },
      async (req, res) => {
        const url = new URL(req.url, "https://localhost");
        res.setHeader("Cache-Control", "no-store");
        res.setHeader("Content-Type", "application/json");
        const now = Date.now();
        if (url.pathname === "/api/v1/session" && req.method === "POST") {
          assert.equal(req.headers.authorization, "Bearer " + marker);
          logins++;
          res.setHeader("Set-Cookie", loginCookies);
          res.writeHead(204);
          res.end();
          return;
        }
        assert.ok(req.headers.cookie?.includes("__Host-kodex-session="));
        assert.equal(req.headers.authorization, undefined);
        assert.equal(req.socket.getProtocol(), "TLSv1.3");
        if (url.pathname === "/api/v1/session") {
          res.end(
            JSON.stringify({
              generation: "11111111-1111-4111-8111-111111111111",
              version: 1,
              sessionRevision: 1,
              renewalMode: "BACKEND_REFRESH",
              serverTime: new Date(now).toISOString(),
              accessExpiresAt: new Date(now + 300000).toISOString(),
              expiresAt: new Date(now + 1800000).toISOString(),
              absoluteExpiresAt: new Date(now + 3600000).toISOString(),
              renewAfter: new Date(now + 180000).toISOString(),
            }),
          );
          return;
        }
        let raw = "";
        for await (const chunk of req) raw += chunk;
        const body = raw ? JSON.parse(raw) : undefined;
        if (req.method === "POST") {
          assert.equal(req.headers["x-csrf-token"], "1".repeat(43));
          assert.ok(req.headers["idempotency-key"]);
          assert.equal(req.headers["if-match"], '"' + view.version + '"');
        }
        if (
          url.pathname.endsWith("/port-forward-connections") &&
          req.method === "GET"
        ) {
          res.end(
            JSON.stringify({
              ...page(),
              items: [],
              profile: { ...page().profile, activeConnections: 0 },
            }),
          );
          return;
        }
        if (
          url.pathname.endsWith("/port-forward-connections") &&
          req.method === "POST"
        ) {
          opens++;
          assert.deepEqual(body, { slot: 0 });
          view = { ...view, version: view.version + 1 };
          conn = { ...conn, inputVersion: view.version };
          res.writeHead(201);
          res.end(
            JSON.stringify({
              connection: conn,
              input: view,
              submission: { ...submission(), state: "ACKNOWLEDGED" },
            }),
          );
          return;
        }
        if (url.pathname.endsWith("/port-forward-connection")) {
          res.end(
            JSON.stringify({
              connection: { ...conn, inputVersion: view.version },
            }),
          );
          return;
        }
        if (url.pathname.endsWith("/input") && req.method === "GET") {
          res.end(
            JSON.stringify({
              view,
              ...(url.searchParams.has("submissionIdempotencyKey")
                ? {
                    submission: {
                      ...submission(),
                      sequence: submitted + 1,
                      state: "ACKNOWLEDGED",
                    },
                  }
                : {}),
            }),
          );
          return;
        }
        if (url.pathname.endsWith("/input") && req.method === "POST") {
          const wire = Buffer.from(body.payloadBase64, "base64");
          assert.equal(
            createHash("sha256").update(wire).digest("hex"),
            body.payloadDigest,
          );
          assert.equal(body.endOfInput, false);
          submitted++;
          if (wire[4] === 2)
            emit(
              2,
              Uint8Array.from(wire.subarray(51), (b) =>
                b >= 97 && b <= 122 ? b - 32 : b,
              ),
            );
          else {
            emit(3);
            emit(4);
            conn = {
              ...conn,
              state: "CLOSED",
              nativeJoined: true,
              readEndObserved: true,
            };
          }
          view = { ...view, version: view.version + 1 };
          res.writeHead(201);
          res.end(
            JSON.stringify({
              view,
              submission: {
                ...submission(),
                sequence: submitted + 1,
                byteCount: wire.length,
                payloadDigest: body.payloadDigest,
                state: "ACKNOWLEDGED",
              },
            }),
          );
          wire.fill(0);
          return;
        }
        if (url.pathname.endsWith("/output")) {
          const after = BigInt(url.searchParams.get("afterSequence"));
          lastCursor = after;
          const wire = outputs[Number(after)];
          res.end(
            JSON.stringify({
              invocationRef: view.invocationRef,
              streamRef: view.streamRef,
              generation: String(view.generation),
              afterSequence: String(after),
              outputWatermark: String(outputs.length),
              droppedBytes: "0",
              status: wire ? "FRAME" : "WAITING",
              authorizedUntil: new Date(now + 4000).toISOString(),
              nativeTerminalKnown: false,
              transportJoined: false,
              ...(wire
                ? {
                    frame: {
                      stageRef: "kof_synthetic",
                      sequence: String(after + 1n),
                      channel: "PORT_FORWARD",
                      payloadDigest: createHash("sha256")
                        .update(wire)
                        .digest("hex"),
                      payloadBase64: wire.toString("base64"),
                    },
                  }
                : {}),
            }),
          );
          return;
        }
        assert.fail("unexpected endpoint");
      },
    );
    server.on("tlsClientError", () => {});
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    t.after(
      () =>
        new Promise((resolve) => {
          server.closeAllConnections();
          server.close(resolve);
        }),
    );
    const origin = "https://localhost:" + server.address().port,
      auth = [
        "--origin",
        origin,
        "--session-file",
        stateFile,
        "--ca-file",
        join(dir, "ca.pem"),
      ];
    writeFileSync(tokenFile, JSON.stringify({ origin, accessToken: marker }), {
      mode: 0o600,
    });
    const login = await child(t, ["login", ...auth, "--token-file", tokenFile]);
    assert.deepEqual(await login.line(), { sessionCreated: true });
    assert.equal((await login.exited)[0], 0);
    assert.equal(logins, 1);
    assert.ok(!readFileSync(stateFile, "utf8").includes(marker));
    const profileCLI = await child(t, [
      "profile",
      ...auth,
      "--run-ref",
      view.runRef,
      "--node-ref",
      view.nodeRef,
      "--invocation-ref",
      view.invocationRef,
      "--stream-ref",
      view.streamRef,
    ]);
    const selectedProfile = await profileCLI.line();
    assert.equal(selectedProfile.profile.protocol, "PORT_FORWARD_SPDY_V1");
    assert.deepEqual(selectedProfile.profile.ports, [
      { port: 18080, targetPort: 8080 },
    ]);
    assert.equal(opens, 0);
    assert.equal((await profileCLI.exited)[0], 0);
    const cli = await child(t, [
      "forward",
      ...auth,
      "--run-ref",
      view.runRef,
      "--node-ref",
      view.nodeRef,
      "--invocation-ref",
      view.invocationRef,
      "--stream-ref",
      view.streamRef,
      "--map",
      "0:0",
    ]);
    const announced = await cli.line();
    assert.equal(announced.listening[0].host, "127.0.0.1");
    const socket = connect({
        host: "127.0.0.1",
        port: announced.listening[0].localPort,
        allowHalfOpen: true,
      }),
      chunks = [];
    socket.on("data", (chunk) => chunks.push(chunk));
    await once(socket, "connect");
    socket.end("hello");
    await once(socket, "close");
    while (lastCursor < 3n) await delay(5);
    cli.process.kill("SIGINT");
    const result = await cli.line();
    assert.equal(result.deliveryComplete, true);
    assert.equal(result.nativeConnectionsJoined, true);
    assert.equal((await cli.exited)[0], 0);
    assert.equal(Buffer.concat(chunks).toString(), "HELLO");
    assert.equal(opens, 1);
    assert.equal(submitted, 2);
    assert.equal(cli.diagnostics(), "");
    for (const wire of outputs) wire.fill(0);
  },
);
