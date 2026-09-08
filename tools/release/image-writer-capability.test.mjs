import test from "node:test";
import assert from "node:assert/strict";
import { writerCapability, requireImageWriterTarget, verifyImageWriterBinding, inspectImageWriter, processStartTicks } from "./image-writer-capability.mjs";

const cap = writerCapability();
const uid = "11111111-1111-4111-8111-111111111111";
function fixture(native = false) {
  const container = { name: cap.container, image: cap.image, command: [cap.executable], args: [] };
  const status = { name: container.name, containerID: `containerd://${"a".repeat(64)}`, imageID: cap.image,
    ready: true, restartCount: 0, state: { running: { startedAt: "2026-09-08T10:00:00Z" } } };
  const pod = { metadata: { uid, name: "role-image-builder-one", namespace: "kodex-system",
    labels: { "app.kubernetes.io/name": "role-image-builder" } }, spec: { containers: native ? [] : [container], initContainers: native ? [container] : [] },
    status: { phase: "Running", containerStatuses: native ? [] : [status], initContainerStatuses: native ? [status] : [] } };
  const runtime = { status: { id: "a".repeat(64), state: "CONTAINER_RUNNING", imageRef: cap.image,
    metadata: { name: container.name, attempt: 0 }, labels: {
      "io.kubernetes.pod.uid": uid, "io.kubernetes.pod.name": pod.metadata.name,
      "io.kubernetes.pod.namespace": "kodex-system", "io.kubernetes.container.name": container.name,
    } }, info: { pid: 12345, runtimeSpec: { process: { args: [cap.executable], env: ["SECRET=must-not-appear"] } } } };
  const process = { executable: cap.executable, executableSHA256: cap.executableSHA256, startTicks: "123", device: "8", inode: "42" };
  return { pod, container, runtime, process };
}

test("closed capability records exact source trees and canonical build recipe", () => {
  assert.equal(cap.target, "role-image-builder");
  assert.deepEqual(cap.capabilities, ["platform-worker-grant.v1", "platform-worker-grant.v2.pod-uid"]);
  assert.equal(cap.build.toolchain, "go1.26.6");
  for (const [path, tree] of Object.entries(cap.source.trees)) {
    assert.match(path, /^(services\/internal\/internal-rpc-authority|libs\/go\/[a-z]+)$/);
    assert.match(tree, /^[a-f0-9]{40}$/);
  }
  assert.equal(Object.keys(cap.source.trees).length, 7);
  assert.match(cap.source.revision, /^[a-f0-9]{40}$/);
  assert.deepEqual(cap.build.arguments, ["build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "./cmd/internal-rpc-authority-platform-worker-grant-agent"]);
  const copy = writerCapability(); copy.executableSHA256 = "b".repeat(64);
  assert.equal(writerCapability().executableSHA256, cap.executableSHA256);
});

test("regular/native immutable writer readback contains only safe identity", () => {
  for (const native of [false, true]) {
    const { pod, container, runtime, process } = fixture(native); let reads = 0, processes = 0;
    const result = inspectImageWriter(pod, container, {
      readRuntime: (id) => { assert.equal(id, "a".repeat(64)); reads++; return runtime; },
      readProcess: (pid) => { assert.equal(pid, 12345); processes++; return process; },
    });
    assert.equal(reads, 2); assert.equal(processes, 2);
    assert.equal(result.executableSHA256, cap.executableSHA256);
    assert.equal(result.podUID, uid);
    assert.equal(JSON.stringify(result).includes("SECRET"), false);
    assert.equal(JSON.stringify(result).includes("runtimeSpec"), false);
  }
});

test("target, image/tag, command and source ambiguity fail closed", () => {
  const { container } = fixture();
  for (const target of ["control-plane", "runtime-controller", "foreign"]) {
    assert.throws(() => requireImageWriterTarget(target, container), /IMAGE_WRITER_CAPABILITY_REQUIRED/);
  }
  for (const mutate of [
    (c) => { c.name = "foreign"; }, (c) => { c.image = "image:latest"; },
    (c) => { c.image = c.image.replace(/.$/, "a"); }, (c) => { c.command = ["/bin/sh", "-c", cap.executable]; },
    (c) => { c.args = ["unknown"]; }, (c) => { c.volumeMounts = [{ mountPath: "/workspace" }]; },
  ]) { const copy = structuredClone(container); mutate(copy); assert.throws(() => requireImageWriterTarget("role-image-builder", copy)); }
});

test("CRI identity, imageID, state, restart and executable command drift are rejected", () => {
  for (const mutate of [
    (f) => { f.pod.metadata.namespace = "production"; }, (f) => { f.pod.metadata.deletionTimestamp = "now"; },
    (f) => { f.pod.status.containerStatuses[0].imageID = "sha256:unknown"; },
    (f) => { f.pod.status.containerStatuses[0].ready = false; },
    (f) => { f.runtime.status.id = "b".repeat(64); }, (f) => { f.runtime.status.state = "CONTAINER_EXITED"; },
    (f) => { f.runtime.status.imageRef = "image:latest"; }, (f) => { f.runtime.status.metadata.attempt = 1; },
    (f) => { f.runtime.info.pid = 1; }, (f) => { f.runtime.info.pid = "12345"; },
    (f) => { f.runtime.info.runtimeSpec.process.args.push("unknown"); },
    ...["uid", "name", "namespace"].map((key) => (f) => { f.runtime.status.labels[`io.kubernetes.pod.${key}`] = "foreign"; }),
    (f) => { f.runtime.status.labels["io.kubernetes.container.name"] = "foreign"; },
  ]) { const f = fixture(); mutate(f); assert.throws(() => verifyImageWriterBinding(f.pod, f.container, f.runtime)); }
});

test("unknown executable hash and process replacement between reads cannot pass", () => {
  for (const key of ["executableSHA256", "executable", "startTicks", "inode", "device"]) {
    const f = fixture(); let reads = 0;
    assert.throws(() => inspectImageWriter(f.pod, f.container, { readRuntime: () => f.runtime,
      readProcess: () => { reads++; return reads === 2 ? { ...f.process, [key]: "changed" } : f.process; },
    }), /IMAGE_WRITER_PROCESS_CHANGED/);
  }
  const f = fixture();
  assert.throws(() => inspectImageWriter(f.pod, f.container, { readRuntime: () => f.runtime,
    readProcess: () => ({ ...f.process, executableSHA256: "b".repeat(64) }),
  }), /IMAGE_WRITER_EXECUTABLE_REJECTED/);
  let reads = 0;
  assert.throws(() => inspectImageWriter(f.pod, f.container, { readRuntime: () => {
    reads++; return reads === 2 ? { ...f.runtime, info: { ...f.runtime.info, pid: 22222 } } : f.runtime;
  }, readProcess: () => f.process }), /IMAGE_WRITER_PROCESS_CHANGED/);
});

test("proc start ticks tolerate spaces in comm but reject missing identity", () => {
  assert.equal(processStartTicks(`123 (writer (comm)) S ${Array(18).fill("0").join(" ")} 456 0`), "456");
  for (const text of ["", "123 comm missing", "123 (writer) S 0"]) assert.throws(() => processStartTicks(text));
});
