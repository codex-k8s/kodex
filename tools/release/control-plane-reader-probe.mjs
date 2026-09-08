import { connect } from "node:http2";
import { checkServerIdentity } from "node:tls";
import { X509Certificate } from "node:crypto";
import { isIP } from "node:net";
import { execFileSync } from "node:child_process";
import { readFileSync, statSync } from "node:fs";

const serverName = "control-plane.kodex-system.svc.cluster.local";
const method = "/internalrpcauthority.v1.AuthorityProofResolverService/CheckReadiness";
function requireValue(value, code) { if (!value) throw new Error(code); }

// Закрытая форма текущего generated Proto response, без reflection и неизвестных полей.
export function decodeReaderReadiness(frame) {
  requireValue(Buffer.isBuffer(frame) && frame.length >= 7 && frame.length <= 4096 &&
    frame[0] === 0 && frame.readUInt32BE(1) === frame.length - 5, "INVALID_GRPC_FRAME");
  let offset = 5;
  const integer = () => {
    let result = 0, multiplier = 1;
    for (let index = 0; index < 8; index++) {
      requireValue(offset < frame.length, "TRUNCATED_PROTO_RESPONSE");
      const value = frame[offset++]; result += (value & 127) * multiplier;
      requireValue(Number.isSafeInteger(result), "PROTO_INTEGER_OVERFLOW");
      if (!(value & 128)) return result;
      multiplier *= 128;
    }
    throw new Error("PROTO_INTEGER_OVERFLOW");
  };
  const fields = new Map();
  while (offset < frame.length) {
    const tag = integer(), field = Math.floor(tag / 8), wire = tag % 8;
    requireValue(field >= 1 && field <= 6 && !fields.has(field), "UNKNOWN_OR_DUPLICATE_PROTO_FIELD");
    if (field === 3 || field === 5) {
      requireValue(wire === 2, "INVALID_PROTO_WIRE_TYPE");
      const length = integer(); requireValue(length === 64 && offset + length <= frame.length, "INVALID_READINESS_DIGEST");
      const value = frame.subarray(offset, offset + length).toString("ascii"); offset += length;
      requireValue(/^[a-f0-9]{64}$/.test(value), "INVALID_READINESS_DIGEST"); fields.set(field, value);
    } else {
      requireValue(wire === 0, "INVALID_PROTO_WIRE_TYPE"); fields.set(field, integer());
    }
  }
  requireValue(fields.size === 6 && fields.get(1) === 1 && fields.get(6) === 1 &&
    fields.get(2) > 0 && fields.get(4) > 0, "READER_NOT_READY");
  return { policyRevision: fields.get(2), policySHA256: fields.get(3), signerGeneration: fields.get(4), signerThumbprintSHA256: fields.get(5) };
}

export async function callReaderReadiness({ address, port = 8443, ca, cert, key, timeoutMilliseconds = 5000 }) {
  requireValue(isIP(address) === 4 && Number.isSafeInteger(port) && port > 0 && port <= 65535 &&
    Number.isSafeInteger(timeoutMilliseconds) && timeoutMilliseconds > 0 && timeoutMilliseconds <= 5000, "INVALID_READER_TARGET");
  return new Promise((resolve, reject) => {
    const client = connect(`https://${address}:${port}`, {
      ca, cert, key, servername: serverName, rejectUnauthorized: true, minVersion: "TLSv1.3",
      checkServerIdentity: (_host, certificate) => checkServerIdentity(serverName, certificate),
    });
    let stream, finished = false, size = 0, status, grpcStatus;
    const chunks = [];
    const done = (error, value) => {
      if (finished) return; finished = true; clearTimeout(timer);
      stream?.destroy(); client.destroy();
      if (error) reject(new Error("READER_RPC_FAILED")); else resolve(value);
    };
    const timer = setTimeout(() => done(true), timeoutMilliseconds);
    client.on("error", () => done(true));
    client.once("connect", () => {
      if (finished) return;
      try { stream = client.request({ ":method": "POST", ":path": method, "content-type": "application/grpc", te: "trailers", "grpc-timeout": "4S" }); }
      catch { done(true); return; }
      stream.on("response", (headers) => { status = headers[":status"]; if (headers["grpc-status"] !== undefined) grpcStatus = headers["grpc-status"]; });
      stream.on("trailers", (headers) => { grpcStatus = headers["grpc-status"]; });
      stream.on("error", () => done(true));
      stream.on("data", (chunk) => { size += chunk.length; if (size > 4096) done(true); else chunks.push(chunk); });
      stream.on("end", () => {
        try {
          requireValue(status === 200 && grpcStatus === "0", "READER_RPC_STATUS_REJECTED");
          done(false, decodeReaderReadiness(Buffer.concat(chunks)));
        } catch { done(true); }
      });
      // Пустой protobuf request; application credential в request отсутствует по этому контракту.
      stream.end(Buffer.alloc(5));
    });
  });
}

export function verifyReaderContainer(pod, runtime) {
  requireValue(pod?.metadata?.namespace === "kodex-system" && pod.metadata.labels?.["app.kubernetes.io/name"] === "control-plane" &&
    !pod.metadata.deletionTimestamp && pod.status?.phase === "Running", "EXACT_READER_POD_REQUIRED");
  const status = pod.status.containerStatuses?.filter((item) => item.name === "control-plane") ?? [];
  requireValue(status.length === 1 && status[0].ready && /^containerd:\/\/[a-f0-9]{64}$/.test(status[0].containerID), "EXACT_READER_CONTAINER_REQUIRED");
  const id = status[0].containerID.slice("containerd://".length);
  requireValue(runtime.status?.id === id && runtime.status.state === "CONTAINER_RUNNING" &&
    runtime.status.metadata?.name === "control-plane" && runtime.status.labels?.["io.kubernetes.pod.uid"] === pod.metadata.uid &&
    Number.isSafeInteger(runtime.info?.pid) && runtime.info.pid > 1 && isIP(pod.status.podIP) === 4, "RUNTIME_CONTAINER_BINDING_REJECTED");
  return { id, pid: runtime.info.pid, podUID: pod.metadata.uid, address: pod.status.podIP };
}

// SRE на dev host читает материал непосредственно из mount namespace точного
// CRI container. Ключ не копируется в файл/argv/Pod/QA и очищается после RPC.
export async function probeControlPlaneReader(pod) {
  requireValue(process.getuid?.() === 0, "SRE_HOST_ROOT_REQUIRED");
  const containers = pod.status?.containerStatuses?.filter((item) => item.name === "control-plane") ?? [];
  requireValue(containers.length === 1 && /^containerd:\/\/[a-f0-9]{64}$/.test(containers[0].containerID), "EXACT_READER_CONTAINER_REQUIRED");
  const id = containers[0].containerID.slice("containerd://".length);
  const readRuntime = () => JSON.parse(execFileSync("k3s", ["crictl", "inspect", id], { encoding: "utf8", stdio: "pipe", timeout: 10_000, maxBuffer: 4 << 20 }));
  const binding = verifyReaderContainer(pod, readRuntime());
  const root = `/proc/${binding.pid}/root`;
  const read = (path) => {
    requireValue(statSync(root + path).isFile() && statSync(root + path).size <= 65536, "BOUNDED_TLS_MATERIAL_REQUIRED");
    return readFileSync(root + path);
  };
  let key;
  try {
    const ca = read("/var/run/config/kodex/control-plane/internal-ca/ca.pem");
    const cert = read("/var/run/secrets/kodex/control-plane/workload-tls/tls.crt");
    key = read("/var/run/secrets/kodex/control-plane/workload-tls/tls.key");
    requireValue(new X509Certificate(cert).subjectAltName.split(", ").includes("URI:spiffe://kodex.local/ns/kodex-system/sa/control-plane"), "EXACT_CLIENT_IDENTITY_REQUIRED");
    requireValue(verifyReaderContainer(pod, readRuntime()).pid === binding.pid, "READER_RESTARTED");
    const result = await callReaderReadiness({ address: binding.address, ca, cert, key });
    requireValue(verifyReaderContainer(pod, readRuntime()).pid === binding.pid, "READER_RESTARTED");
    return { podUID: binding.podUID, status: "PASS", ...result };
  } finally { key?.fill(0); }
}
