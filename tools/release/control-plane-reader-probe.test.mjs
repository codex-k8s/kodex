import { test } from "node:test";
import assert from "node:assert/strict";
import { createSecureServer } from "node:http2";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { decodeReaderReadiness, callReaderReadiness, verifyReaderContainer } from "./control-plane-reader-probe.mjs";

function frame(body = Buffer.concat([Buffer.from([8, 1, 16, 7, 26, 64]), Buffer.from("a".repeat(64)),
  Buffer.from([32, 3, 42, 64]), Buffer.from("b".repeat(64)), Buffer.from([48, 1])])) {
  const prefix = Buffer.alloc(5); prefix.writeUInt32BE(body.length, 1); return Buffer.concat([prefix, body]);
}
test("readiness parser rejects partial, false, duplicate, unknown and oversized responses", () => {
  assert.deepEqual(decodeReaderReadiness(frame()), { policyRevision: 7, policySHA256: "a".repeat(64),
    signerGeneration: 3, signerThumbprintSHA256: "b".repeat(64) });
  const falseReady = frame(); falseReady[6] = 0;
  const compressed = frame(); compressed[0] = 1;
  for (const value of [falseReady, compressed, frame().subarray(0, -1), Buffer.alloc(4097),
    frame(Buffer.concat([frame().subarray(5), Buffer.from([8, 1])])),
    frame(Buffer.concat([frame().subarray(5), Buffer.from([56, 1])])), frame(Buffer.from([16, 128]))]) {
    assert.throws(() => decodeReaderReadiness(value));
  }
});

test("CRI binding rejects reused PID, foreign Pod and stopped container", () => {
  const id = "a".repeat(64), uid = "11111111-1111-4111-8111-111111111111";
  const pod = { metadata: { namespace: "kodex-system", uid, labels: { "app.kubernetes.io/name": "control-plane" } },
    status: { phase: "Running", podIP: "10.42.0.10", containerStatuses: [{ name: "control-plane", ready: true, containerID: `containerd://${id}` }] } };
  const runtime = { status: { id, state: "CONTAINER_RUNNING", metadata: { name: "control-plane" }, labels: { "io.kubernetes.pod.uid": uid } }, info: { pid: 123 } };
  assert.equal(verifyReaderContainer(pod, runtime).pid, 123);
  for (const mutate of [
    (r) => { r.status.id = "b".repeat(64); }, (r) => { r.status.state = "CONTAINER_EXITED"; },
    (r) => { r.status.labels["io.kubernetes.pod.uid"] = "foreign"; }, (r) => { r.info.pid = 1; },
  ]) { const r = structuredClone(runtime); mutate(r); assert.throws(() => verifyReaderContainer(pod, r)); }
});

test("addressed HTTP2 RPC requires exact TLS hostname, CA, client certificate and successful bounded gRPC", async (t) => {
  const directory = mkdtempSync(join(tmpdir(), "kodex-reader-tls-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const openssl = (...args) => execFileSync("openssl", args, { cwd: directory, stdio: "pipe" });
  openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", "ca.key", "-out", "ca.crt", "-subj", "/CN=fixture-ca", "-days", "1");
  const issue = (name, san) => {
    openssl("req", "-newkey", "rsa:2048", "-nodes", "-keyout", `${name}.key`, "-out", `${name}.csr`, "-subj", `/CN=${name}`);
    writeFileSync(join(directory, `${name}.ext`), `subjectAltName=${san}\nextendedKeyUsage=serverAuth,clientAuth\n`, { mode: 0o600 });
    openssl("x509", "-req", "-in", `${name}.csr`, "-CA", "ca.crt", "-CAkey", "ca.key", "-CAcreateserial", "-out", `${name}.crt`, "-days", "1", "-extfile", `${name}.ext`);
  };
  issue("server", "DNS:control-plane.kodex-system.svc.cluster.local");
  issue("wrong", "DNS:wrong.kodex-system.svc.cluster.local");
  issue("client", "URI:spiffe://kodex.local/ns/kodex-system/sa/control-plane");
  const read = (name) => readFileSync(join(directory, name));
  const ca = read("ca.crt"), cert = read("client.crt"), key = read("client.key");
  for (const scenario of ["valid", "wrong-host", "untrusted-ca", "no-client", "status", "large", "timeout"]) {
    await t.test(scenario, async () => {
      const name = scenario === "wrong-host" ? "wrong" : "server";
      const server = createSecureServer({ ca, cert: read(`${name}.crt`), key: read(`${name}.key`), requestCert: true, rejectUnauthorized: true, minVersion: "TLSv1.3" });
      const sessions = new Set();
      server.on("session", (session) => { sessions.add(session); session.on("error", () => {}); session.on("close", () => sessions.delete(session)); });
      server.on("tlsClientError", () => {});
      server.on("stream", (stream, headers) => {
        stream.on("error", () => {});
        assert.equal(headers[":path"], "/internalrpcauthority.v1.AuthorityProofResolverService/CheckReadiness");
        assert.equal(headers[":method"], "POST");
        assert.equal(stream.session.socket.authorized, true);
        const chunks = [];
        stream.on("data", (chunk) => chunks.push(chunk));
        stream.on("end", () => {
          assert.deepEqual(Buffer.concat(chunks), Buffer.alloc(5));
          if (scenario === "timeout") return;
          stream.respond({ ":status": 200, "content-type": "application/grpc" }, { waitForTrailers: true });
          stream.on("wantTrailers", () => stream.sendTrailers({ "grpc-status": scenario === "status" ? "14" : "0" }));
          stream.end(scenario === "large" ? Buffer.alloc(4097) : frame());
        });
      });
      await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
      try {
        const operation = callReaderReadiness({ address: "127.0.0.1", port: server.address().port,
          ca: scenario === "untrusted-ca" ? undefined : ca, cert: scenario === "no-client" ? undefined : cert,
          key: scenario === "no-client" ? undefined : key, timeoutMilliseconds: scenario === "timeout" ? 100 : 3000 });
        if (scenario === "valid") assert.equal((await operation).policyRevision, 7);
        else await assert.rejects(operation, /READER_RPC_FAILED/);
      } finally { for (const session of sessions) session.destroy(); await new Promise((resolve) => server.close(resolve)); }
    });
  }
});
