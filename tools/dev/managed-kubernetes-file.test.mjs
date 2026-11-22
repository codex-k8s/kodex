import assert from "node:assert/strict";
import { execFile, execFileSync } from "node:child_process";
import { promisify } from "node:util";
import {
  mkdtempSync,
  readFileSync,
  writeFileSync,
  rmSync,
  chmodSync,
  symlinkSync,
} from "node:fs";
import { createServer } from "node:https";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { parseKubernetesFileArguments } from "./managed-kubernetes-file.mjs";
import { readOwnerRuntimeCA } from "./owner-runtime-https.mjs";
import { fileInputFixture } from "../../services/staff/control-center/src/shared/lib/kubernetes-file-input.test-support.ts";

const run = promisify(execFile),
  command = new URL("./managed-kubernetes-file.mjs", import.meta.url).pathname;
test("metadata-only CLI rejects raw file, caller revision, credential and duplicate flags", () => {
  const args = [
    "get",
    "--origin",
    "https://control.invalid",
    "--session-file",
    "/private/session",
    "--invocation-ref",
    "inv_synthetic",
    "--source-ref",
    "kfs_synthetic",
  ];
  assert.equal(
    parseKubernetesFileArguments(args)["source-ref"],
    "kfs_synthetic",
  );
  for (const extra of [
    ["--file", "/private/source"],
    ["--artifact-ref", "art_synthetic"],
    ["--revision", "2"],
    ["--token", "synthetic"],
    ["--kubeconfig", "/private/config"],
    ["--source-ref", "kfs_other000"],
  ])
    assert.throws(() => parseKubernetesFileArguments([...args, ...extra]));
  assert.throws(() => parseKubernetesFileArguments(args.slice(0, -1)));
});
test("shared CA descriptor guard rejects symlink and clears bounded read failures", (t) => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-file-ca-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  writeFileSync(join(dir, "ca"), "synthetic CA");
  symlinkSync(join(dir, "ca"), join(dir, "link"));
  assert.throws(() => readOwnerRuntimeCA(join(dir, "link")));
  assert.equal(readOwnerRuntimeCA(join(dir, "ca")).toString(), "synthetic CA");
});
test(
  "actual CLI private login→TLS1.3→source metadata and closure-only lost-ACK repair, without input mutation",
  { timeout: 20000 },
  async (t) => {
    const dir = mkdtempSync(join(tmpdir(), "kodex-file-cli-"));
    chmodSync(dir, 0o700);
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
    const token = "synthetic-owner-token-".repeat(4),
      view = fileInputFixture(),
      paths = [];
    let mode = "normal",
      cleanupPosts = 0;
    const cleanupView = {
        sourceRef: view.sourceRef,
        invocationRef: view.invocationRef,
        cleanup: {
          version: 4,
          repairGeneration: 1,
          outstandingCount: 1,
          outstandingBytes: 2048,
          exhaustedCount: 0,
          eligibleExhaustedCount: 0,
        },
      },
      cleanupReceipt = {
        sourceRef: view.sourceRef,
        version: 4,
        repairGeneration: 1,
        repairedCount: 1,
        remainingExhaustedCount: 0,
        acceptedAt: new Date().toISOString(),
      };
    const server = createServer(
      {
        key: readFileSync(join(dir, "key.pem")),
        cert: readFileSync(join(dir, "ca.pem")),
        minVersion: "TLSv1.3",
      },
      (req, res) => {
        paths.push([req.method, req.url]);
        assert.equal(req.socket.getProtocol(), "TLSv1.3");
        res.setHeader("Cache-Control", "no-store");
        res.setHeader("Content-Type", "application/json");
        if (req.url === "/api/v1/session" && req.method === "POST") {
          assert.equal(req.headers.authorization, "Bearer " + token);
          res.setHeader("Set-Cookie", [
            "__Host-kodex-session=v1." +
              "1".repeat(64) +
              "; Path=/; Max-Age=1800; HttpOnly; Secure; SameSite=Strict",
            "__Host-kodex-csrf=" +
              "1".repeat(43) +
              "; Path=/; Max-Age=1800; Secure; SameSite=Strict",
          ]);
          res.writeHead(204);
          res.end();
          return;
        }
        assert.ok(req.headers.cookie?.includes("__Host-kodex-session="));
        assert.equal(req.headers.authorization, undefined);
        if (req.url === "/api/v1/session") {
          assert.equal(req.method, "GET");
          const now = Date.now();
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
        assert.ok(
          req.url.startsWith(
            "/api/v1/integration-invocations/inv_synthetic/kubernetes-file-inputs",
          ),
        );
        if (mode === "forbidden") {
          res.writeHead(403);
          res.end('{"private":"must not be printed"}');
          return;
        }
        if (req.url.endsWith("/cleanup-repair")) {
          assert.equal(req.method, "POST");
          assert.equal(req.headers["if-match"], '\"3\"');
          assert.equal(req.headers["idempotency-key"], "original-cleanup-key");
          assert.ok(req.headers["x-csrf-token"]);
          cleanupPosts++;
          // Потеря ответа после owner принятия: следующий вызов допустим только GET исходного key.
          req.resume();
          req.on("end", () => req.socket.destroy());
          return;
        }
        assert.equal(req.method, "GET");
        if (
          new URL(req.url, "https://localhost").searchParams.get(
            "cleanupOnly",
          ) === "true"
        ) {
          const query = new URL(req.url, "https://localhost").searchParams;
          const publishedView =
            mode === "repair-capable"
              ? { ...cleanupView, repairAvailable: true }
              : cleanupView;
          res.end(
            JSON.stringify(
              req.url.includes("/kfs_synthetic")
                ? {
                    projection: "CLEANUP_ONLY",
                    cleanupView: publishedView,
                    ...(query.has("cleanupIdempotencyKey")
                      ? { cleanupReceipt }
                      : {}),
                  }
                : { projection: "CLEANUP_ONLY", items: [publishedView] },
            ),
          );
          return;
        }
        const value =
          mode === "malformed"
            ? { ...view, payload: "must not be printed" }
            : view;
        res.end(
          JSON.stringify(
            req.url.endsWith("/kfs_synthetic")
              ? { projection: "SOURCE", view: value }
              : { projection: "SOURCE", items: [value] },
          ),
        );
      },
    );
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    t.after(
      () =>
        new Promise((resolve) => {
          server.closeAllConnections();
          server.close(resolve);
        }),
    );
    const origin = `https://localhost:${server.address().port}`,
      state = join(dir, "session.json"),
      tokenFile = join(dir, "token.json");
    writeFileSync(tokenFile, JSON.stringify({ origin, accessToken: token }), {
      mode: 0o600,
    });
    const base = [
      "--origin",
      origin,
      "--session-file",
      state,
      "--ca-file",
      join(dir, "ca.pem"),
    ];
    const child = (args) =>
      run(process.execPath, [command, ...args], {
        timeout: 5000,
        maxBuffer: 65536,
      });
    const login = await child(["login", ...base, "--token-file", tokenFile]);
    assert.deepEqual(JSON.parse(login.stdout), { sessionCreated: true });
    const list = await child([
      "list",
      ...base,
      "--invocation-ref",
      view.invocationRef,
    ]);
    assert.deepEqual(JSON.parse(list.stdout), { items: [view] });
    const get = [
      "get",
      ...base,
      "--invocation-ref",
      view.invocationRef,
      "--source-ref",
      view.sourceRef,
    ];
    assert.deepEqual(JSON.parse((await child(get)).stdout), view);
    for (mode of ["forbidden", "malformed"]) {
      const before = paths.length;
      await assert.rejects(
        child(get),
        (error) =>
          error.code === 2 &&
          error.stdout === "" &&
          !error.stderr.includes("must not be printed") &&
          !error.stderr.includes(token),
      );
      assert.equal(
        paths.slice(before).filter(([, path]) => path !== "/api/v1/session")
          .length,
        1,
      );
    }
    mode = "normal";
    const cleanupList = await child([
      "cleanup-list",
      ...base,
      "--invocation-ref",
      view.invocationRef,
    ]);
    assert.deepEqual(JSON.parse(cleanupList.stdout), {
      projection: "CLEANUP_ONLY",
      items: [cleanupView],
    });
    const repairArgs = [
      "cleanup-repair",
      ...base,
      "--invocation-ref",
      view.invocationRef,
      "--source-ref",
      view.sourceRef,
      "--version",
      "3",
      "--idempotency-key",
      "original-cleanup-key",
    ];
    await assert.rejects(
      child(repairArgs),
      (error) => error.code === 2 && error.stdout === "",
    );
    assert.equal(cleanupPosts, 0);
    mode = "repair-capable";
    const repaired = await child(repairArgs);
    assert.deepEqual(JSON.parse(repaired.stdout), {
      projection: "CLEANUP_ONLY",
      cleanupView: { ...cleanupView, repairAvailable: true },
      cleanupReceipt,
    });
    assert.equal(cleanupPosts, 1);
    assert.equal(
      paths.filter(([, path]) =>
        path.includes("cleanupIdempotencyKey=original-cleanup-key"),
      ).length,
      1,
    );
    assert.equal(paths.filter(([method]) => method === "POST").length, 2);
    assert.ok(!repaired.stdout.includes("artifact"));
  },
);
