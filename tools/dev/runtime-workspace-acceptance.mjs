import { createHash } from "node:crypto";

const proofFile = "mvp-workspace-proof.json";
const resultFile = "mvp-workspace-result.txt";
const provenanceFile = "workspace-write-result.json";
const checks = [
  "create",
  "read",
  "atomicReplace",
  "readReplacement",
  "delete",
  "input",
  "source",
  "context",
  "skills",
  "memory",
  "credential",
  "symlink",
  "traversal",
  "outside",
];

function fail(message) {
  throw new Error(`Runtime workspace acceptance failed: ${message}`);
}

function validNonce(nonce) {
  if (typeof nonce !== "string" || !/^[a-f0-9]{32}$/.test(nonce))
    fail("fixture nonce is invalid");
}

// Код выполняет сама модель своим shell tool внутри настоящего provider sandbox.
// Он не читает credential/source/context contents и публикует только synthetic proof.
async function runWorkspaceProbe(nonce) {
  const fs = await import("node:fs");
  const { createHash } = await import("node:crypto");
  const require = (condition) => {
    if (!condition) throw new Error("workspace probe condition failed");
  };
  require(/^[a-f0-9]{32}$/.test(nonce) && process.getuid() > 0);
  const directory = `/workspace/work/mvp-${nonce}`;
  fs.mkdirSync("/workspace/work", { recursive: true, mode: 0o770 });
  fs.chmodSync("/workspace/work", 0o2770);
  fs.mkdirSync(directory, { mode: 0o770 });
  fs.chmodSync(directory, 0o2770);
  const current = `${directory}/current.txt`;
  const replacement = `${directory}/next.txt`;
  const initial = `mvp-workspace:${nonce}:initial\n`;
  const final = `mvp-workspace:${nonce}:replacement\n`;
  const results = {};
  function exclusiveWrite(path, content) {
    const fd = fs.openSync(
      path,
      fs.constants.O_CREAT |
        fs.constants.O_EXCL |
        fs.constants.O_WRONLY |
        fs.constants.O_NOFOLLOW,
      0o640,
    );
    try {
      fs.writeFileSync(fd, content);
      fs.fchmodSync(fd, 0o640);
      fs.fsyncSync(fd);
    } finally {
      fs.closeSync(fd);
    }
  }
  exclusiveWrite(current, initial);
  results.create = true;
  require(fs.readFileSync(current, "utf8") === initial);
  results.read = true;
  const before = fs.statSync(current);
  exclusiveWrite(replacement, final);
  fs.renameSync(replacement, current);
  const after = fs.statSync(current);
  require(before.ino !== after.ino || before.dev !== after.dev);
  results.atomicReplace = true;
  const bytes = fs.readFileSync(current);
  require(bytes.toString("utf8") === final);
  results.readReplacement = true;
  fs.unlinkSync(current);
  require(fs.statSync(current, { throwIfNoEntry: false }) === undefined);
  results.delete = true;
  function denied(path, existing = false) {
    let fd;
    try {
      // Existing credential проверяется без TRUNC и без чтения единого байта.
      fd = fs.openSync(
        path,
        fs.constants.O_WRONLY |
          (existing ? 0 : fs.constants.O_CREAT | fs.constants.O_EXCL),
      );
    } catch (error) {
      require(
        ["EROFS", "EACCES", "EPERM"].includes(error.code) ||
          (existing && error.code === "ENOENT"),
      );
      return true;
    } finally {
      if (fd !== undefined) fs.closeSync(fd);
    }
    if (!existing) fs.unlinkSync(path);
    throw new Error("protected workspace write was allowed");
  }
  for (const [key, path] of Object.entries({
    input: "/workspace/input",
    source: "/workspace/knowledge",
    context: "/workspace/context",
    skills: "/workspace/context/skills",
    memory: "/workspace/context/memory",
  })) {
    require(fs.statSync(path).isDirectory());
    results[key] = denied(`${path}/mvp-${nonce}`);
  }
  results.credential = denied(
    "/workspace/.kodex/state/codex-home/auth.json",
    true,
  );
  const alias = `${directory}/context-alias`;
  fs.symlinkSync("/workspace/context", alias);
  try {
    results.symlink = denied(`${alias}/mvp-${nonce}`);
  } finally {
    fs.unlinkSync(alias);
  }
  results.traversal = denied(`${directory}/../../context/mvp-${nonce}`);
  results.outside = denied(`/mvp-workspace-${nonce}`);
  fs.rmdirSync(directory);
  const outbox = "/workspace/.kodex/outbox";
  require(fs.statSync(outbox).isDirectory());
  exclusiveWrite(`${outbox}/mvp-workspace-result.txt`, bytes);
  exclusiveWrite(
    `${outbox}/mvp-workspace-proof.json`,
    JSON.stringify({
      schema: "kodex.workspace-acceptance.v1",
      nonce,
      uid: process.getuid(),
      checks: results,
      resultSHA256: createHash("sha256").update(bytes).digest("hex"),
    }),
  );
  console.log("Workspace acceptance probe completed");
}

export function workspaceProbeSource(nonce) {
  validNonce(nonce);
  return `${runWorkspaceProbe.toString()}\nawait runWorkspaceProbe(${JSON.stringify(nonce)});\n`;
}

export function workspaceAcceptanceTask(nonce) {
  return (
    "Выполни этот точный JavaScript через Node.js shell tool в текущем workspace. " +
    "Это synthetic acceptance: создание, чтение, атомарная замена и удаление своего файла; " +
    "отдельно ожидаемые отказы записи в защищённые пути. Не читай секреты или содержимое контекста. " +
    "Не подменяй выполнение описанием и не исправляй проверки при отказе. " +
    "Запусти как ES module. После успешного выполнения оставь оба созданных файла в .kodex/outbox " +
    "и ответь одной короткой фразой.\n```javascript\n" +
    workspaceProbeSource(nonce) +
    "```"
  );
}

// Отдельный Run превышает только число файлов: 10001 пустой regular file,
// без заполнения диска и изменения чужих путей. Пауза нужна exact Pod readback.
async function runWorkspaceQuotaProbe(nonce) {
  const fs = await import("node:fs");
  if (!/^[a-f0-9]{32}$/.test(nonce) || process.getuid() <= 0)
    throw new Error("workspace quota probe identity is invalid");
  const directory = `/workspace/work/mvp-quota-${nonce}`;
  fs.mkdirSync("/workspace/work", { recursive: true, mode: 0o770 });
  fs.chmodSync("/workspace/work", 0o2770);
  fs.mkdirSync(directory, { mode: 0o770 });
  fs.chmodSync(directory, 0o2770);
  for (let index = 0; index < 10001; index += 1) {
    const fd = fs.openSync(`${directory}/${index}`, "wx", 0o600);
    fs.closeSync(fd);
  }
  if (fs.readdirSync(directory).length !== 10001)
    throw new Error("workspace quota probe file count differs");
  console.log("Workspace quota fixture prepared");
  await new Promise((resolve) => setTimeout(resolve, 45000));
}

export function workspaceQuotaProbeSource(nonce) {
  validNonce(nonce);
  return `${runWorkspaceQuotaProbe.toString()}\nawait runWorkspaceQuotaProbe(${JSON.stringify(nonce)});\n`;
}

export function workspaceQuotaTask(nonce) {
  return (
    "Выполни этот точный JavaScript через Node.js shell tool как ES module. " +
    "Установи timeout команды не меньше 120 секунд и дождись её завершения. " +
    "Это отдельный synthetic negative acceptance квоты: 10001 пустой собственный файл. " +
    "Ожидается последующий отказ платформы. Не удаляй файлы, не исправляй проверку, " +
    "не читай секреты/контекст и не создавай result artifacts. После выхода команды " +
    "ответь одной короткой фразой.\n```javascript\n" +
    workspaceQuotaProbeSource(nonce) +
    "```"
  );
}

function digest(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

export async function boundedResponseBody(response, maximumBytes) {
  const declared = response.headers.get("content-length");
  if (
    declared !== null &&
    (!/^[0-9]+$/.test(declared) || Number(declared) > maximumBytes)
  ) {
    await response.body?.cancel().catch(() => {});
    fail("response size exceeds limit");
  }
  const reader = response.body?.getReader();
  if (!reader) fail("response body is absent");
  const chunks = [];
  let total = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > maximumBytes) fail("response size exceeds limit");
      chunks.push(Buffer.from(value));
    }
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
  return Buffer.concat(chunks, total);
}

export async function verifyWorkspaceAcceptance({
  getJSON,
  getContent,
  runRef,
  projectRef,
  agentRef,
  nonce,
}) {
  validNonce(nonce);
  const run = await getJSON(`/api/v1/runs/${encodeURIComponent(runRef)}`);
  if (
    run.ref !== runRef ||
    run.projectRef !== projectRef ||
    run.target?.ref !== agentRef ||
    run.state !== "SUCCEEDED" ||
    !Number.isSafeInteger(run.attempt) ||
    run.attempt < 1 ||
    !Array.isArray(run.artifactRefs) ||
    run.artifactRefs.length > 200
  )
    fail("successful run binding is invalid");
  await verifyNativeAgentShell(getJSON, run);
  const artifacts = new Map();
  for (const ref of run.artifactRefs) {
    const artifact = await getJSON(
      `/api/v1/artifacts/${encodeURIComponent(ref)}`,
    );
    if (![proofFile, resultFile, provenanceFile].includes(artifact.fileName))
      continue;
    if (
      artifacts.has(artifact.fileName) ||
      artifact.ref !== ref ||
      artifact.runRef !== runRef ||
      artifact.projectRef !== projectRef ||
      artifact.sessionRef !== run.sessionRef ||
      artifact.source !== "AGENT_RESULT" ||
      artifact.lifecycleState !== "ACTIVE" ||
      !Number.isSafeInteger(artifact.sizeBytes) ||
      artifact.sizeBytes < 1 ||
      artifact.sizeBytes > 16384
    )
      fail("artifact owner or eligibility is invalid");
    if (["PENDING", "SCANNING"].includes(artifact.scanState)) {
      const pending = new Error("Runtime workspace artifact scan is pending");
      pending.code = "ARTIFACT_SCAN_PENDING";
      throw pending;
    }
    if (artifact.scanState !== "CLEAN") fail("artifact scan is not clean");
    const bytes = await getContent(
      `/api/v1/artifacts/${encodeURIComponent(ref)}/content`,
      16384,
    );
    if (
      bytes.length !== artifact.sizeBytes ||
      artifact.digest !== `sha256:${digest(bytes)}`
    )
      fail("artifact content pins differ");
    artifacts.set(artifact.fileName, { artifact, bytes });
  }
  return verifyPersistedProof({
    getJSON,
    artifacts,
    run,
    runRef,
    projectRef,
    nonce,
  });
}

async function verifyNativeAgentShell(getJSON, run) {
  if (!Number.isSafeInteger(run.lastEventSequence) || run.lastEventSequence < 1)
    fail("run event sequence is invalid");
  let afterSequence = 0;
  let shellCall = false;
  for (
    let page = 0;
    page < 25 && afterSequence < run.lastEventSequence;
    page += 1
  ) {
    const events = await getJSON(
      `/api/v1/runs/${encodeURIComponent(run.ref)}/events?afterSequence=${afterSequence}&limit=500`,
    );
    if (
      !Array.isArray(events.items) ||
      events.items.length < 1 ||
      events.items.length > 500
    )
      fail("native execution event page is invalid");
    for (const event of events.items) {
      if (
        event.runRef !== run.ref ||
        !Number.isSafeInteger(event.sequence) ||
        event.sequence <= afterSequence
      )
        fail("native execution event binding differs");
      afterSequence = event.sequence;
      shellCall ||=
        event.type === "TOOL_CALL_RECORDED" &&
        event.toolCall?.tool === "CODEX_SHELL" &&
        event.toolCall.state === "SUCCEEDED" &&
        event.toolCall.safeParameters?.source === "AGENT" &&
        event.toolCall.safeParameters?.cwd_scope === "WORKSPACE" &&
        event.toolCall.safeParameters?.exit_code === "ZERO";
    }
  }
  if (afterSequence < run.lastEventSequence || !shellCall)
    fail("successful native agent shell event is absent");
}

async function verifyPersistedProof({
  getJSON,
  artifacts,
  run,
  runRef,
  projectRef,
  nonce,
}) {
  if (artifacts.size !== 3) fail("required persisted artifacts are absent");
  let proof, provenance;
  try {
    proof = JSON.parse(artifacts.get(proofFile).bytes.toString("utf8"));
    provenance = JSON.parse(
      artifacts.get(provenanceFile).bytes.toString("utf8"),
    );
  } catch {
    fail("artifact proof is invalid JSON");
  }
  const result = artifacts.get(resultFile).bytes;
  if (
    proof.schema !== "kodex.workspace-acceptance.v1" ||
    proof.nonce !== nonce ||
    !Number.isSafeInteger(proof.uid) ||
    proof.uid <= 0 ||
    !proof.checks ||
    Object.keys(proof.checks).length !== checks.length ||
    !checks.every((check) => proof.checks[check] === true) ||
    proof.resultSHA256 !== digest(result) ||
    result.toString("utf8") !== `mvp-workspace:${nonce}:replacement\n`
  )
    fail("agent workspace proof differs");
  const revision = (
    await getJSON(
      `/api/v1/runs/${encodeURIComponent(runRef)}/runtime-revision-diff`,
    )
  ).current;
  if (
    !revision ||
    revision.runRef !== runRef ||
    revision.sessionRef !== run.sessionRef ||
    revision.attempt !== run.attempt ||
    provenance.schema !== "kodex.workspace-write-result.v1" ||
    provenance.runtime_revision_ref !== revision.ref ||
    provenance.runtime_revision_version !== revision.version ||
    provenance.runtime_revision_digest !== revision.revisionDigest ||
    provenance.attempt !== revision.attempt ||
    !/^[a-f0-9]{64}$/.test(provenance.execution_binding_digest)
  )
    fail("persisted attempt provenance differs");
  return {
    schema: "kodex.workspace-acceptance-evidence.v1",
    runRef,
    projectRef,
    runtimeRevisionRef: revision.ref,
    runtimeRevisionVersion: revision.version,
    runtimeRevisionDigest: revision.revisionDigest,
    attempt: revision.attempt,
    checks: {
      ...Object.fromEntries(checks.map((key) => [key, "PASS"])),
      nativeAgentShell: "PASS",
    },
    artifacts: [...artifacts.values()].map(({ artifact }) => ({
      ref: artifact.ref,
      digest: artifact.digest,
    })),
    quota: "NOT RUN",
  };
}

export async function verifyWorkspaceQuota({
  getJSON,
  runRef,
  projectRef,
  agentRef,
  observation,
}) {
  const run = await getJSON(`/api/v1/runs/${encodeURIComponent(runRef)}`);
  const revision = (
    await getJSON(
      `/api/v1/runs/${encodeURIComponent(runRef)}/runtime-revision-diff`,
    )
  ).current;
  if (
    run.ref !== runRef ||
    run.projectRef !== projectRef ||
    run.target?.ref !== agentRef ||
    run.state !== "FAILED" ||
    run.safeErrorCode !== "RUNTIME_INPUT_INVALID" ||
    !Number.isSafeInteger(run.attempt) ||
    run.attempt < 1 ||
    !Array.isArray(run.artifactRefs) ||
    run.artifactRefs.length !== 0 ||
    !revision ||
    revision.runRef !== runRef ||
    revision.sessionRef !== run.sessionRef ||
    revision.attempt !== run.attempt ||
    !/^[a-f0-9]{64}$/.test(revision.revisionDigest)
  )
    fail("quota terminal run binding is invalid");
  if (
    !observation ||
    observation.reason !== "QUOTA_EXCEEDED" ||
    observation.runRef !== runRef ||
    observation.revisionDigest !== revision.revisionDigest ||
    observation.attempt !== run.attempt ||
    !/^[a-z0-9][a-z0-9-]{1,252}$/.test(observation.podName) ||
    !/^[a-f0-9-]{36}$/.test(observation.podUID)
  )
    fail("exact runtime quota readback is absent");
  await verifyNativeAgentShell(getJSON, run);
  return {
    schema: "kodex.workspace-quota-evidence.v1",
    runRef,
    projectRef,
    runtimeRevisionRef: revision.ref,
    runtimeRevisionDigest: revision.revisionDigest,
    attempt: revision.attempt,
    podUID: observation.podUID,
    reason: observation.reason,
    terminalCode: run.safeErrorCode,
    nativeAgentShell: "PASS",
    resultArtifactsAbsent: "PASS",
    quota: "PASS",
  };
}
