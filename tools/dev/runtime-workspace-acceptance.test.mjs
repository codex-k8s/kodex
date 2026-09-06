import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  statSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn, spawnSync } from "node:child_process";
import test from "node:test";
import {
  boundedResponseBody,
  verifyWorkspaceAcceptance,
  verifyWorkspaceQuota,
  workspaceProbeSource,
  workspaceQuotaProbeSource,
} from "./runtime-workspace-acceptance.mjs";

const nonce = "a".repeat(32);
const hash = (value) => createHash("sha256").update(value).digest("hex");

function fixture() {
  const checks = Object.fromEntries(
    [
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
    ].map((key) => [key, true]),
  );
  const result = Buffer.from(`mvp-workspace:${nonce}:replacement\n`);
  const proof = {
    schema: "kodex.workspace-acceptance.v1",
    nonce,
    uid: 10002,
    checks,
    resultSHA256: hash(result),
  };
  const provenance = {
    schema: "kodex.workspace-write-result.v1",
    runtime_revision_ref: "rrev_fixture",
    runtime_revision_version: 3,
    runtime_revision_digest: "b".repeat(64),
    attempt: 2,
    execution_binding_digest: "c".repeat(64),
  };
  const run = {
    ref: "run_fixture",
    projectRef: "prj_fixture",
    sessionRef: "ses_fixture",
    target: { ref: "agt_fixture" },
    state: "SUCCEEDED",
    attempt: 2,
    lastEventSequence: 1,
    artifactRefs: ["art_result", "art_proof", "art_provenance"],
  };
  const revision = {
    ref: provenance.runtime_revision_ref,
    version: 3,
    revisionDigest: provenance.runtime_revision_digest,
    runRef: run.ref,
    sessionRef: run.sessionRef,
    attempt: 2,
  };
  const contents = {
    art_result: result,
    art_proof: Buffer.from(JSON.stringify(proof)),
    art_provenance: Buffer.from(JSON.stringify(provenance)),
  };
  const artifacts = Object.fromEntries(
    Object.entries(contents).map(([ref, bytes], index) => [
      ref,
      {
        ref,
        runRef: run.ref,
        projectRef: run.projectRef,
        sessionRef: run.sessionRef,
        source: "AGENT_RESULT",
        lifecycleState: "ACTIVE",
        scanState: "CLEAN",
        fileName: [
          "mvp-workspace-result.txt",
          "mvp-workspace-proof.json",
          "workspace-write-result.json",
        ][index],
        sizeBytes: bytes.length,
        digest: `sha256:${hash(bytes)}`,
      },
    ]),
  );
  const events = {
    items: [
      {
        runRef: run.ref,
        sequence: 1,
        type: "TOOL_CALL_RECORDED",
        toolCall: {
          tool: "CODEX_SHELL",
          state: "SUCCEEDED",
          safeParameters: {
            source: "AGENT",
            cwd_scope: "WORKSPACE",
            exit_code: "ZERO",
          },
        },
      },
    ],
    complete: true,
    currentSequence: 1,
  };
  function replace(ref, value) {
    contents[ref] = Buffer.from(JSON.stringify(value));
    artifacts[ref].sizeBytes = contents[ref].length;
    artifacts[ref].digest = `sha256:${hash(contents[ref])}`;
  }
  return {
    run,
    revision,
    artifacts,
    contents,
    proof,
    provenance,
    events,
    replace,
    parameters: {
      runRef: run.ref,
      projectRef: run.projectRef,
      agentRef: run.target.ref,
      nonce,
      getJSON: async (path) => {
        if (path.includes("/artifacts/"))
          return artifacts[path.split("/").at(-1)];
        if (path.endsWith("/runtime-revision-diff"))
          return { current: revision };
        if (path.includes("/events?")) return events;
        return run;
      },
      getContent: async (path) => contents[path.split("/").at(-2)],
    },
  };
}

test("Сопоставляет настоящий результат, owner metadata, tool event и runtime attempt", async () => {
  const data = fixture();
  const evidence = await verifyWorkspaceAcceptance(data.parameters);
  assert.equal(evidence.attempt, 2);
  assert.equal(evidence.checks.nativeAgentShell, "PASS");
  assert.equal(evidence.checks.atomicReplace, "PASS");
  assert.equal(evidence.artifacts.length, 3);
  assert.equal(evidence.quota, "NOT RUN");
  assert.ok(!JSON.stringify(evidence).includes(nonce));
});

function quotaFixture() {
  const data = fixture();
  data.run.state = "FAILED";
  data.run.safeErrorCode = "RUNTIME_INPUT_INVALID";
  data.run.artifactRefs = [];
  data.parameters.observation = {
    reason: "QUOTA_EXCEEDED",
    runRef: data.run.ref,
    revisionDigest: data.revision.revisionDigest,
    attempt: data.run.attempt,
    podName: "runtime-fixture",
    podUID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  };
  return data;
}

test("Quota требует одновременно owner terminal, actual canary и native execution", async () => {
  const data = quotaFixture();
  const evidence = await verifyWorkspaceQuota(data.parameters);
  assert.equal(evidence.quota, "PASS");
  assert.equal(evidence.reason, "QUOTA_EXCEEDED");
  assert.equal(evidence.resultArtifactsAbsent, "PASS");
});

for (const [name, mutate] of Object.entries({
  "не quota отказ": (f) => {
    f.parameters.observation.reason = "RUNTIME_IO_ERROR";
  },
  "нет canary": (f) => {
    delete f.parameters.observation;
  },
  "чужой canary Run": (f) => {
    f.parameters.observation.runRef = "run_other";
  },
  "старая canary revision": (f) => {
    f.parameters.observation.revisionDigest = "d".repeat(64);
  },
  "старая attempt": (f) => {
    f.parameters.observation.attempt = 1;
  },
  "нет Pod UID": (f) => {
    f.parameters.observation.podUID = "";
  },
  "provider failure": (f) => {
    f.run.safeErrorCode = "RUNTIME_PROVIDER_UNAVAILABLE";
  },
  "ошибочный SUCCESS": (f) => {
    f.run.state = "SUCCEEDED";
  },
  "чужой проект": (f) => {
    f.run.projectRef = "prj_other";
  },
  "чужой агент": (f) => {
    f.run.target.ref = "agt_other";
  },
  "чужая session revision": (f) => {
    f.revision.sessionRef = "ses_other";
  },
  "артефакт при отказе": (f) => {
    f.run.artifactRefs = ["art_unexpected"];
  },
  "отказ до выполнения": (f) => {
    f.events.items[0].toolCall.state = "FAILED";
  },
}))
  test(`Quota отклоняет: ${name}`, async () => {
    const data = quotaFixture();
    mutate(data);
    await assert.rejects(
      verifyWorkspaceQuota(data.parameters),
      /Runtime workspace acceptance failed/,
    );
  });

test("Pod selector связывает image readback с revision/project/session/attempt и единственным UID", () => {
  const image = `registry.invalid/runtime@sha256:${"b".repeat(64)}`;
  const binding = {
    revisionDigest: "a".repeat(64),
    projectHash: "b".repeat(16),
    sessionHash: "c".repeat(16),
    attempt: 2,
  };
  const pod = {
    metadata: {
      uid: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
      labels: {
        "runtime.kodex.dev/managed": "true",
        "runtime.kodex.dev/mode": "turn",
      },
      annotations: {
        "runtime.kodex.dev/revision-digest": binding.revisionDigest,
        "runtime.kodex.dev/project-hash": binding.projectHash,
        "runtime.kodex.dev/session-hash": binding.sessionHash,
        "runtime.kodex.dev/attempt": "2",
      },
    },
    spec: {
      initContainers: [{ name: "workspace-init", image }],
      containers: [
        { name: "role-runtime", image },
        { name: "provider-runtime", image },
      ],
    },
    status: {
      initContainerStatuses: [{ name: "workspace-init", imageID: image }],
      containerStatuses: [
        { name: "role-runtime", imageID: image },
        { name: "provider-runtime", imageID: image },
      ],
    },
  };
  function select(pods, before = []) {
    const result = spawnSync(
      "jq",
      [
        "-c",
        "--argjson",
        "before",
        JSON.stringify(before),
        "--arg",
        "image",
        image,
        "--argjson",
        "binding",
        JSON.stringify(binding),
        "-f",
        new URL("./runtime-workspace-pod.jq", import.meta.url).pathname,
      ],
      {
        input: JSON.stringify(pods),
        encoding: "utf8",
        timeout: 3000,
        maxBuffer: 8192,
      },
    );
    assert.equal(result.status, 0, result.stderr);
    return result.stdout.trim();
  }
  assert.equal(JSON.parse(select([pod])).metadata.uid, pod.metadata.uid);
  assert.equal(select([pod], [pod.metadata.uid]), "");
  assert.equal(
    select([pod, { ...pod, metadata: { ...pod.metadata, uid: "duplicate" } }]),
    "",
  );
  for (const field of [
    "revision-digest",
    "project-hash",
    "session-hash",
    "attempt",
  ]) {
    const changed = structuredClone(pod);
    changed.metadata.annotations[`runtime.kodex.dev/${field}`] = "wrong";
    assert.equal(select([changed]), "", field);
  }
  for (const mutate of [
    (p) => {
      p.metadata.labels["runtime.kodex.dev/mode"] = "warm";
    },
    (p) => {
      p.spec.containers[1].image = "foreign";
    },
    (p) => {
      p.status.containerStatuses[1].imageID = `registry.invalid/other@sha256:${"c".repeat(64)}`;
    },
    (p) => {
      p.status.containerStatuses = [];
    },
  ]) {
    const changed = structuredClone(pod);
    mutate(changed);
    assert.equal(select([changed]), "");
  }
});

test("Точный quota probe non-root создаёт ограниченные 10001 пустых файлов и ждёт readback", async () => {
  assert.ok(process.getuid() > 0);
  const root = mkdtempSync(join(tmpdir(), "kodex-quota-probe-"));
  let child;
  try {
    child = spawn(
      "bwrap",
      [
        "--unshare-all",
        "--die-with-parent",
        "--uid",
        String(process.getuid()),
        "--gid",
        String(process.getgid()),
        "--tmpfs",
        "/",
        "--ro-bind",
        "/usr",
        "/usr",
        "--ro-bind",
        "/lib",
        "/lib",
        "--ro-bind",
        "/lib64",
        "/lib64",
        "--ro-bind",
        process.execPath,
        "/node",
        "--bind",
        root,
        "/workspace",
        "--chdir",
        "/workspace",
        "--remount-ro",
        "/",
        "/node",
        "--input-type=module",
        "--eval",
        workspaceQuotaProbeSource(nonce),
      ],
      {
        env: { PATH: "/usr/bin:/bin", LANG: "C" },
        stdio: ["ignore", "pipe", "ignore"],
        detached: true,
      },
    );
    const exited = new Promise((resolve) => child.once("close", resolve));
    await new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error("quota fixture preparation timed out")),
        8000,
      );
      child.once("error", (error) => {
        clearTimeout(timer);
        reject(error);
      });
      child.once("exit", () => {
        clearTimeout(timer);
        reject(new Error("quota fixture exited before observation"));
      });
      child.stdout.once("data", (value) => {
        clearTimeout(timer);
        if (value.toString() === "Workspace quota fixture prepared\n")
          resolve();
        else reject(new Error("quota fixture marker is invalid"));
      });
    });
    const directory = join(root, `work/mvp-quota-${nonce}`);
    const files = readdirSync(directory);
    assert.equal(files.length, 10001);
    assert.equal(statSync(directory).mode & 0o2770, 0o2770);
    assert.equal(child.exitCode, null);
    assert.ok(
      files.every((file) => {
        const info = statSync(join(directory, file));
        return info.isFile() && info.size === 0 && info.nlink === 1;
      }),
    );
    process.kill(-child.pid, "SIGKILL");
    await exited;
  } finally {
    if (child && child.exitCode === null && child.signalCode === null) {
      try {
        process.kill(-child.pid, "SIGKILL");
      } catch (error) {
        if (error.code !== "ESRCH") throw error;
      }
    }
    rmSync(root, { recursive: true, force: true });
  }
});

for (const [name, mutate] of Object.entries({
  "чужой проект": (f) => {
    f.run.projectRef = "prj_foreign";
  },
  "чужой агент": (f) => {
    f.run.target.ref = "agt_foreign";
  },
  "неуспешный запуск": (f) => {
    f.run.state = "FAILED";
  },
  "нет native shell": (f) => {
    f.events.items[0].toolCall.tool = "CODEX_SLEEP";
  },
  "чужой event": (f) => {
    f.events.items[0].runRef = "run_foreign";
  },
  "повтор sequence": (f) => {
    f.events.items[0].sequence = 0;
  },
  "неполный catch-up": (f) => {
    f.run.lastEventSequence = 1000;
  },
  "нет persisted artifact": (f) => {
    f.run.artifactRefs.pop();
  },
  "чужой artifact": (f) => {
    f.artifacts.art_proof.runRef = "run_foreign";
  },
  "не агентский source": (f) => {
    f.artifacts.art_proof.source = "CONTROL_CENTER";
  },
  карантин: (f) => {
    f.artifacts.art_proof.scanState = "QUARANTINED";
  },
  "чужой artifact с pending scan": (f) => {
    f.artifacts.art_proof.runRef = "run_foreign";
    f.artifacts.art_proof.scanState = "SCANNING";
  },
  "неверный hash": (f) => {
    f.contents.art_result = Buffer.from("wrong");
  },
  "другая проверка": (f) => {
    f.proof.nonce = "d".repeat(32);
    f.replace("art_proof", f.proof);
  },
  "root процесс": (f) => {
    f.proof.uid = 0;
    f.replace("art_proof", f.proof);
  },
  "пропущенный read": (f) => {
    delete f.proof.checks.read;
    f.replace("art_proof", f.proof);
  },
  "иная attempt": (f) => {
    f.provenance.attempt = 1;
    f.replace("art_provenance", f.provenance);
  },
  "иная revision": (f) => {
    f.revision.ref = "rrev_foreign";
  },
}))
  test(`Отклоняет ${name}`, async () => {
    const data = fixture();
    mutate(data);
    await assert.rejects(
      verifyWorkspaceAcceptance(data.parameters),
      /Runtime workspace acceptance failed/,
    );
  });

test("Незавершённый scan имеет отдельный bounded retry outcome", async () => {
  const data = fixture();
  data.artifacts.art_proof.scanState = "SCANNING";
  await assert.rejects(verifyWorkspaceAcceptance(data.parameters), {
    code: "ARTIFACT_SCAN_PENDING",
  });
});

test("Читает тело с лимитом до полной буферизации и отменяет oversized stream", async () => {
  assert.equal(
    (await boundedResponseBody(new Response("abcd"), 4)).toString(),
    "abcd",
  );
  let cancelled = false;
  const body = new ReadableStream({
    start(controller) {
      controller.enqueue(new Uint8Array(5));
    },
    cancel() {
      cancelled = true;
    },
  });
  await assert.rejects(
    boundedResponseBody(new Response(body), 4),
    /response size exceeds limit/,
  );
  assert.equal(cancelled, true);
  await assert.rejects(
    boundedResponseBody(
      new Response("x", { headers: { "content-length": "100" } }),
      4,
    ),
  );
});

test("Точный выдаваемый модели probe выполняет CRUD и отказы в non-root disposable mount", () => {
  assert.ok(process.getuid() > 0, "non-root test process is required");
  const root = mkdtempSync(join(tmpdir(), "kodex-workspace-probe-"));
  try {
    for (const path of [
      "input",
      "knowledge",
      "context/skills",
      "context/memory",
      ".kodex/state/codex-home",
      ".kodex/outbox",
    ])
      mkdirSync(join(root, path), { recursive: true, mode: 0o700 });
    const credential = join(root, ".kodex/state/codex-home/auth.json");
    writeFileSync(credential, "synthetic-fixture", { mode: 0o600 });
    const args = [
      "--unshare-user",
      "--uid",
      String(process.getuid()),
      "--gid",
      String(process.getgid()),
      "--tmpfs",
      "/",
      "--ro-bind",
      "/usr",
      "/usr",
      "--ro-bind",
      "/lib",
      "/lib",
      "--ro-bind",
      "/lib64",
      "/lib64",
      "--ro-bind",
      process.execPath,
      "/node",
      "--bind",
      root,
      "/workspace",
      ...["input", "knowledge", "context"].flatMap((path) => [
        "--ro-bind",
        join(root, path),
        `/workspace/${path}`,
      ]),
      "--ro-bind",
      credential,
      "/workspace/.kodex/state/codex-home/auth.json",
      "--chdir",
      "/workspace",
      "--remount-ro",
      "/",
      "/node",
      "--input-type=module",
      "--eval",
      workspaceProbeSource(nonce),
    ];
    const result = spawnSync("bwrap", args, {
      env: { PATH: "/usr/bin:/bin", LANG: "C" },
      timeout: 10_000,
      maxBuffer: 8192,
      encoding: "utf8",
    });
    assert.equal(result.status, 0, result.stderr);
    const proof = JSON.parse(
      readFileSync(
        join(root, ".kodex/outbox/mvp-workspace-proof.json"),
        "utf8",
      ),
    );
    assert.equal(Object.keys(proof.checks).length, 14);
    assert.ok(Object.values(proof.checks).every((value) => value === true));
    assert.equal(
      statSync(join(root, ".kodex/outbox/mvp-workspace-proof.json")).mode &
        0o777,
      0o640,
    );
    assert.equal(readFileSync(credential, "utf8"), "synthetic-fixture");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
