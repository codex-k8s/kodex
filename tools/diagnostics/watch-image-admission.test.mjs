import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  lstatSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import {
  admissionBinding,
  parseDiagnostic,
  publicDiagnostic,
  saveRemediation,
  validJobPod,
  jobTemplate,
  podTemplate,
  recipeBinding,
  savePendingDiagnostic,
  watchRecipe,
} from "./watch-image-admission.mjs";

const run = "v20261005235959-" + "a".repeat(40);
const binding = admissionBinding({
  admissionRunId: run,
  artifactRef: "imgart_fixture01",
  imageDigest: "sha256:" + "b".repeat(64),
  jobRef: "mc-admit-" + "c".repeat(32) + "-admit",
});
const diagnostic = () => ({
  event: "IMAGE_ADMISSION_DIAGNOSTIC",
  version: 1,
  admissionRunId: run,
  artifactRef: binding.artifactRef,
  imageDigest: binding.imageDigest,
  vulnerabilityEvidenceSha256: "d".repeat(64),
  verdict: "REJECTED",
  recipeRef: "imgrec_fixture01",
  recipeGeneration: 4,
  buildRef: "imgbld_fixture01",
  reason: "VULNERABILITY",
  failureCode: null,
  highOrCriticalMatchCount: 2,
  blockingMatchCount: 1,
  unresolvedNoFixMatchCount: 1,
  remediation: [
    {
      cve: "CVE-2026-12345",
      package: "@scope/package",
      version: "1.2.3",
      fixes: ["1.2.4"],
    },
  ],
});
const command = ["/bin/sh", "/opt/kodex/image-admission.sh", "admit"];
const job = () => ({
  name: binding.jobRef,
  namespace: "kodex-system",
  uid: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  id: binding.id,
  phase: "admit",
  managed: "true",
  runId: run,
  serviceAccount: "image-admission",
  automount: false,
  restartPolicy: "Never",
  containers: [{ name: "admit", command: [...command] }],
});
const pod = () => ({
  name: binding.jobRef + "-fixture",
  namespace: "kodex-system",
  uid: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
  id: binding.id,
  phase: "admit",
  containers: [{ name: "admit", command: [...command] }],
  owners: [
    { name: binding.jobRef, uid: job().uid, kind: "Job", controller: true },
  ],
});
const clone = (value) => structuredClone(value);

test("diagnostic accepts only exact owner/run/image and public bounded remediation", () => {
  assert.deepEqual(
    parseDiagnostic(JSON.stringify(diagnostic()), binding),
    diagnostic(),
  );
  const summary = publicDiagnostic(diagnostic());
  assert.equal(Object.hasOwn(summary, "remediation"), false);
  for (const [key, value] of [
    ["artifactRef", "imgart_foreign01"],
    ["imageDigest", "sha256:" + "f".repeat(64)],
    ["vulnerabilityEvidenceSha256", "d".repeat(63)],
    ["admissionRunId", "v20261005235958-" + "a".repeat(40)],
    ["version", 2],
    ["reason", "raw-private-message"],
    ["failureCode", "raw-private-message"],
    ["blockingMatchCount", -1],
    ["highOrCriticalMatchCount", 99],
    ["token", "must-not-leak"],
  ]) {
    assert.equal(
      parseDiagnostic(
        JSON.stringify({ ...diagnostic(), [key]: value }),
        binding,
      ),
      null,
      key,
    );
  }
  for (const change of [
    (d) => d.remediation.push(...Array(20).fill(d.remediation[0])),
    (d) => {
      d.remediation[0].cve = "https://private.invalid/token";
    },
    (d) => {
      d.remediation[0].package = "https://private.invalid/token";
    },
    (d) => {
      d.remediation[0].version = "secret\nheader";
    },
    (d) => {
      d.remediation[0].version = "1.2.3\n";
    },
    (d) => {
      d.remediation[0].cve += "\n";
    },
    (d) => {
      d.remediation[0].package += "\r\n";
    },
    (d) => {
      d.remediation[0].fixes.push("1.2.4");
    },
    (d) => {
      d.remediation[0].credentials = "must-not-leak";
    },
    (d) => {
      d.verdict = "ACCEPTED";
    },
  ]) {
    const value = diagnostic();
    change(value);
    assert.equal(parseDiagnostic(JSON.stringify(value), binding), null);
  }
  assert.equal(parseDiagnostic("x".repeat(32769), binding), null);
  assert.equal(parseDiagnostic("raw-secret-error", binding), null);
  for (const key of [
    "admissionRunId",
    "artifactRef",
    "imageDigest",
    "jobRef",
  ]) {
    assert.throws(() =>
      admissionBinding({ ...binding, [key]: binding[key] + "\n" }),
    );
  }
});

test("accepted and technical rejections have honest count semantics", () => {
  const accepted = {
    ...diagnostic(),
    verdict: "ACCEPTED",
    reason: "ACCEPTED",
    highOrCriticalMatchCount: 1,
    blockingMatchCount: 0,
    remediation: [],
  };
  assert.deepEqual(
    parseDiagnostic(JSON.stringify(accepted), binding),
    accepted,
  );
  for (const reason of [
    "SCAN_TECHNICAL_REJECTION",
    "SIGN_TECHNICAL_REJECTION",
  ]) {
    const technical = {
      ...diagnostic(),
      reason,
      failureCode: "TECHNICAL_DETAIL_UNKNOWN",
      highOrCriticalMatchCount: null,
      blockingMatchCount: null,
      unresolvedNoFixMatchCount: null,
      remediation: [],
    };
    assert.deepEqual(
      parseDiagnostic(JSON.stringify(technical), binding),
      technical,
    );
    assert.equal(
      parseDiagnostic(
        JSON.stringify({ ...technical, blockingMatchCount: 0 }),
        binding,
      ),
      null,
    );
  }
});

test("Job/Pod eligibility requires exact UID/run/phase/command with no authority widening", () => {
  assert.equal(validJobPod(job(), pod(), binding), true);
  for (const key of ["SCAN_PREDECESSOR_FAILED", "SIGN_PREDECESSOR_FAILED"]) {
    const j = job(),
      p = pod();
    j.containers[0].command.push(key);
    p.containers[0].command.push(key);
    assert.equal(validJobPod(j, p, binding), true);
  }
  for (const change of [
    (j) => {
      j.runId = "foreign";
    },
    (j) => {
      j.id = "f".repeat(32);
    },
    (j) => {
      j.namespace = "kodex-runtime";
    },
    (j) => {
      j.phase = "scan";
    },
    (j) => {
      j.managed = "false";
    },
    (j) => {
      j.automount = true;
    },
    (j) => {
      j.serviceAccount = "image-promotion";
    },
    (j) => {
      j.containers[0].command.push("forged");
    },
    (_j, p) => {
      p.owners[0].uid = "cccccccc-cccc-cccc-cccc-cccccccccccc";
    },
    (_j, p) => {
      p.owners[0].controller = false;
    },
    (_j, p) => {
      p.owners.push(p.owners[0]);
    },
    (_j, p) => {
      p.containers[0].command = ["/bin/sh", "unsafe", "admit"];
    },
    (_j, p) => {
      p.containers.push({ name: "foreign" });
    },
  ]) {
    const j = job(),
      p = pod();
    change(j, p);
    assert.equal(validJobPod(j, p, binding), false);
  }
  assert.doesNotMatch(
    jobTemplate + podTemplate,
    /\.env|secret|volumeMount|\.data|\.image/,
  );
});

test("remediation uses exclusive private file; no non-vulnerability or unsafe output", () => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-admission-output-test-"));
  chmodSync(dir, 0o700);
  try {
    const path = join(dir, "remediation.json");
    saveRemediation(path, diagnostic());
    assert.equal(lstatSync(path).mode & 0o777, 0o600);
    assert.deepEqual(JSON.parse(readFileSync(path, "utf8")), diagnostic());
    assert.throws(() => saveRemediation(path, diagnostic()));
    assert.throws(() =>
      saveRemediation(join(dir, "technical.json"), {
        ...diagnostic(),
        reason: "SCAN_TECHNICAL_REJECTION",
      }),
    );
    symlinkSync(path, join(dir, "link.json"));
    assert.throws(() => saveRemediation(join(dir, "link.json"), diagnostic()));
    mkdirSync(join(dir, "public"), { mode: 0o755 });
    assert.throws(() =>
      saveRemediation(join(dir, "public", "leak.json"), diagnostic()),
    );
    symlinkSync(dir, join(dir, "linked"));
    assert.throws(() =>
      saveRemediation(join(dir, "linked", "leak.json"), diagnostic()),
    );
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("producer derives projection only from readback after successful record", () => {
  const source = readFileSync(
    new URL(
      "../../deploy/k8s/base/image-supply-chain/image-admission.sh",
      import.meta.url,
    ),
    "utf8",
  );
  assert.match(
    source,
    /image-admission-bridge record\n    write_marker admission\.complete\n    emit_admission_diagnostic 2>\/dev\/null/,
  );
  const fn = source.match(
    /^emit_admission_diagnostic\(\) \{\n[\s\S]*?^\}/m,
  )?.[0];
  assert.ok(fn);
  const dir = mkdtempSync(join(tmpdir(), "kodex-admission-producer-test-"));
  try {
    const sha = "d".repeat(64);
    const receipt = {
      artifactId: binding.artifactRef,
      imageDigest: binding.imageDigest,
      verdict: "REJECTED",
      policyRevision: "3",
      policySHA256: sha,
      vulnerabilityEvidenceSHA256: sha,
    };
    const matches = [
      {
        vulnerability: {
          id: "CVE-2026-12345",
          severity: "High",
          fix: { state: "fixed", versions: ["1.2.4"] },
        },
        artifact: { name: "@scope/package", version: "1.2.3" },
      },
      {
        vulnerability: {
          id: "CVE-2026-54321",
          severity: "Critical",
          fix: { state: "not-fixed", versions: [] },
        },
        artifact: { name: "other", version: "2" },
      },
    ];
    const policy = {
      schema: "kodex.dev/fix-available-high-or-critical/v1",
      policyRevision: 3,
      policySHA256: sha,
      highOrCriticalMatchCount: 2,
      blockingMatchCount: 1,
      unresolvedNoFixMatchCount: 1,
    };
    const invoke = (report, expectedReceipt = receipt) => {
      writeFileSync(
        join(dir, "owner-claim.json"),
        JSON.stringify({
          artifactId: expectedReceipt.artifactId,
          manifestDigest: expectedReceipt.imageDigest,
          recipeId: diagnostic().recipeRef,
          recipeGeneration: diagnostic().recipeGeneration,
          buildId: diagnostic().buildRef,
        }),
      );
      writeFileSync(
        join(dir, "admission.receipt.json"),
        JSON.stringify(expectedReceipt),
      );
      writeFileSync(join(dir, "vulnerability.json"), JSON.stringify(report));
      const script = `${fn.replaceAll("/work/evidence.readback", dir).replaceAll("/work/owner-claim.json", join(dir, "owner-claim.json"))}\nADMISSION_RUN_ID='${run}'\nemit_admission_diagnostic\n`;
      return spawnSync("sh", ["-eu", "-c", script], { encoding: "utf8" });
    };
    const result = invoke({ matches, kodexPolicy: policy });
    assert.equal(result.status, 0, result.stderr);
    assert.deepEqual(
      parseDiagnostic(result.stdout.trim(), binding),
      diagnostic(),
    );
    for (const phase of ["scan", "sign"]) {
      const technical = invoke({
        schema: "kodex.dev/vulnerability-evidence-unavailable/v1",
        phase,
        reason: "raw-secret-must-not-leak",
      });
      assert.equal(technical.status, 0, technical.stderr);
      assert.doesNotMatch(technical.stdout, /raw-secret/);
      assert.equal(JSON.parse(technical.stdout).blockingMatchCount, null);
      assert.equal(
        JSON.parse(technical.stdout).failureCode,
        "TECHNICAL_DETAIL_UNKNOWN",
      );
    }
    for (const [reason, code] of [
      ["vulnerability scan failed", "VULNERABILITY_SCAN_FAILED"],
      ["SBOM generation failed", "SBOM_GENERATION_FAILED"],
      ["vulnerability policy evaluation failed", "VULNERABILITY_POLICY_FAILED"],
    ]) {
      const known = invoke({
        schema: "kodex.dev/vulnerability-evidence-unavailable/v1",
        phase: "scan",
        reason,
      });
      assert.equal(known.status, 0);
      assert.equal(JSON.parse(known.stdout).failureCode, code);
      assert.equal(known.stdout.includes(reason), false);
    }
    assert.notEqual(
      invoke({ matches, kodexPolicy: { ...policy, blockingMatchCount: 0 } })
        .status,
      0,
    );
    assert.notEqual(
      invoke({
        matches,
        kodexPolicy: { ...policy, policySHA256: "e".repeat(64) },
      }).status,
      0,
    );
    const unsafe = clone(matches);
    unsafe[0].artifact.name = "https://private.invalid/token";
    assert.deepEqual(
      JSON.parse(invoke({ matches: unsafe, kodexPolicy: policy }).stdout)
        .remediation,
      [],
    );
    for (const key of ["name", "version"]) {
      const unsafeIdentifier = clone(matches);
      unsafeIdentifier[0].artifact[key] += "\n";
      assert.deepEqual(
        JSON.parse(
          invoke({ matches: unsafeIdentifier, kodexPolicy: policy }).stdout,
        ).remediation,
        [],
      );
    }
    const many = Array.from({ length: 25 }, (_, i) => ({
      ...clone(matches[0]),
      vulnerability: {
        ...clone(matches[0].vulnerability),
        id: `CVE-2026-${10000 + i}`,
      },
    }));
    const bounded = invoke({
      matches: many,
      kodexPolicy: {
        ...policy,
        highOrCriticalMatchCount: 25,
        blockingMatchCount: 25,
        unresolvedNoFixMatchCount: 0,
      },
    });
    assert.equal(JSON.parse(bounded.stdout).remediation.length, 20);
    const accepted = invoke(
      {
        matches: [matches[1]],
        kodexPolicy: {
          ...policy,
          highOrCriticalMatchCount: 1,
          blockingMatchCount: 0,
          unresolvedNoFixMatchCount: 1,
        },
      },
      { ...receipt, verdict: "ACCEPTED" },
    );
    assert.equal(JSON.parse(accepted.stdout).reason, "ACCEPTED");
    execFileSync("sh", [
      "-n",
      new URL(
        "../../deploy/k8s/base/image-supply-chain/image-admission.sh",
        import.meta.url,
      ).pathname,
    ]);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("closed early log stream reattaches same Pod only after fresh UID checks", async () => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-admission-reattach-test-"));
  chmodSync(dir, 0o700);
  const previous = process.env.KUBECONFIG;
  process.env.KUBECONFIG = "/home/s/.kube/config";
  let starts = 0,
    jobReads = 0,
    podReads = 0;
  try {
    const result = await watchRecipe(
      {
        recipeRef: diagnostic().recipeRef,
        recipeGeneration: 4,
        pendingOutput: join(dir, "pending.json"),
        timeoutSeconds: 2,
      },
      {
        configMetadata: {
          isFile: () => true,
          isSymbolicLink: () => false,
          uid: process.getuid(),
          mode: 0o600,
        },
        emit: () => {},
        read: (args) => {
          if (args[1] === "jobs") return [job()];
          if (args[1] === "job") {
            jobReads++;
            return job();
          }
          podReads++;
          return [pod()];
        },
        spawn: () => {
          starts++;
          const attempt = starts;
          assert.ok(jobReads >= starts);
          assert.ok(podReads >= starts * 2);
          const child = new EventEmitter();
          child.stdout = new PassThrough();
          child.kill = () => {
            queueMicrotask(() => child.emit("close", 0));
            return true;
          };
          setTimeout(() => {
            if (attempt === 1) child.emit("close", 1);
            else child.stdout.write(JSON.stringify(diagnostic()) + "\n");
          }, 5);
          return child;
        },
      },
    );
    assert.equal(result, "PENDING_OWNER_CONFIRMATION");
    assert.equal(starts, 2);
  } finally {
    if (previous === undefined) delete process.env.KUBECONFIG;
    else process.env.KUBECONFIG = previous;
    rmSync(dir, { recursive: true, force: true });
  }
});

test("CLI rejects unknown arguments without raw exceptions", () => {
  const result = spawnSync(
    process.execPath,
    [
      new URL("./watch-image-admission.mjs", import.meta.url).pathname,
      "--unknown",
      "private-value",
    ],
    { encoding: "utf8" },
  );
  assert.equal(result.status, 1);
  assert.equal(result.stderr, "");
  assert.equal(
    result.stdout.trim(),
    '{"event":"WATCH_FAILED","code":"DIAGNOSTIC_UNAVAILABLE"}',
  );
});

test("recipe mode binds exact generation/build; pending output is not owner confirmation", () => {
  const selected = recipeBinding({
    recipeRef: diagnostic().recipeRef,
    recipeGeneration: 4,
  });
  assert.deepEqual(
    parseDiagnostic(JSON.stringify(diagnostic()), selected),
    diagnostic(),
  );
  for (const changes of [
    { recipeRef: "imgrec_foreign01" },
    { recipeGeneration: 5 },
    { buildRef: "imgbld_foreign01" },
  ]) {
    const exact = recipeBinding({
      recipeRef: diagnostic().recipeRef,
      recipeGeneration: 4,
      buildRef: diagnostic().buildRef,
    });
    assert.equal(
      parseDiagnostic(JSON.stringify({ ...diagnostic(), ...changes }), exact),
      null,
    );
  }
  for (const invalid of [
    { recipeRef: "imgrec_fixture01\n", recipeGeneration: 4 },
    { recipeRef: "imgrec_fixture01", recipeGeneration: 0 },
    {
      recipeRef: "imgrec_fixture01",
      recipeGeneration: 4,
      buildRef: "imgbld_fixture01\n",
    },
  ])
    assert.throws(() => recipeBinding(invalid));
  const dir = mkdtempSync(join(tmpdir(), "kodex-admission-pending-test-"));
  chmodSync(dir, 0o700);
  try {
    const path = join(dir, "pending.json");
    savePendingDiagnostic(path, diagnostic(), selected);
    const value = JSON.parse(readFileSync(path, "utf8"));
    assert.equal(value.confirmation, "PENDING_OWNER_CONFIRMATION");
    assert.equal(lstatSync(path).mode & 0o777, 0o600);
    assert.throws(() => savePendingDiagnostic(path, diagnostic(), selected));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("recipe stream retains candidate after Job deletion, ignores foreign/raw lines, and joins child", async () => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-admission-stream-test-"));
  chmodSync(dir, 0o700);
  const previous = process.env.KUBECONFIG;
  process.env.KUBECONFIG = "/home/s/.kube/config";
  const emitted = [],
    calls = [];
  let deleted = false,
    killed = false,
    closed = false;
  try {
    const path = join(dir, "pending.json");
    const result = await watchRecipe(
      {
        recipeRef: diagnostic().recipeRef,
        recipeGeneration: 4,
        pendingOutput: path,
        timeoutSeconds: 1,
      },
      {
        configMetadata: {
          isFile: () => true,
          isSymbolicLink: () => false,
          uid: process.getuid(),
          mode: 0o600,
        },
        emit: (line) => emitted.push(JSON.parse(line)),
        read: (args) => {
          calls.push(args);
          if (deleted) return null;
          if (args[1] === "jobs") return [job()];
          if (args[1] === "job") return job();
          return [pod()];
        },
        spawn: (binary, args, options) => {
          assert.equal(binary, "kubectl");
          assert.ok(args.includes("--follow"));
          assert.ok(args.includes(pod().name));
          assert.deepEqual(Object.keys(options.env).sort(), [
            "KUBECONFIG",
            "PATH",
          ]);
          const child = new EventEmitter();
          child.stdout = new PassThrough();
          child.kill = (signal) => {
            assert.equal(signal, "SIGKILL");
            killed = true;
            queueMicrotask(() => {
              closed = true;
              child.emit("close", 0);
            });
            return true;
          };
          setTimeout(() => {
            deleted = true;
            child.stdout.write("raw-secret-must-not-leak\n");
            child.stdout.write(
              JSON.stringify({
                ...diagnostic(),
                recipeRef: "imgrec_foreign01",
              }) + "\n",
            );
            child.stdout.write(
              JSON.stringify({
                ...diagnostic(),
                admissionRunId: "v20261005235958-" + "a".repeat(40),
              }) + "\n",
            );
            const line = JSON.stringify(diagnostic());
            child.stdout.write(line.slice(0, 30));
            child.stdout.write(line.slice(30) + "\n");
          }, 5);
          return child;
        },
      },
    );
    assert.equal(result, "PENDING_OWNER_CONFIRMATION");
    assert.equal(killed, true);
    assert.equal(closed, true);
    assert.equal(
      JSON.parse(readFileSync(path, "utf8")).buildRef,
      diagnostic().buildRef,
    );
    assert.equal(emitted.at(-1).event, "OBSERVED_PENDING_OWNER_CONFIRMATION");
    assert.doesNotMatch(
      JSON.stringify(emitted),
      /verdict|blockingMatchCount|CVE-|raw-secret|artifactRef|imageDigest/,
    );
    assert.ok(
      calls.every(
        (args) =>
          args[0] === "get" && ["jobs", "job", "pods"].includes(args[1]),
      ),
    );
  } finally {
    if (previous === undefined) delete process.env.KUBECONFIG;
    else process.env.KUBECONFIG = previous;
    rmSync(dir, { recursive: true, force: true });
  }
});

test("CLI closes mixed recipe/artifact modes without reading auth or cluster", () => {
  const result = spawnSync(
    process.execPath,
    [
      new URL("./watch-image-admission.mjs", import.meta.url).pathname,
      "--recipe-ref",
      diagnostic().recipeRef,
      "--recipe-generation",
      "4",
      "--artifact-ref",
      diagnostic().artifactRef,
    ],
    { encoding: "utf8" },
  );
  assert.equal(result.status, 1);
  assert.equal(result.stderr, "");
  assert.equal(
    result.stdout.trim(),
    '{"event":"WATCH_FAILED","code":"DIAGNOSTIC_UNAVAILABLE"}',
  );
});

test("cancelled recipe observation closes log child without pending report", async () => {
  const dir = mkdtempSync(join(tmpdir(), "kodex-admission-cancel-test-"));
  chmodSync(dir, 0o700);
  const previous = process.env.KUBECONFIG;
  process.env.KUBECONFIG = "/home/s/.kube/config";
  const cancellation = new AbortController();
  const emitted = [];
  let joined = false;
  try {
    const path = join(dir, "pending.json");
    assert.equal(
      await watchRecipe(
        {
          recipeRef: diagnostic().recipeRef,
          recipeGeneration: 4,
          pendingOutput: path,
          timeoutSeconds: 1,
        },
        {
          signal: cancellation.signal,
          configMetadata: {
            isFile: () => true,
            isSymbolicLink: () => false,
            uid: process.getuid(),
            mode: 0o600,
          },
          emit: (line) => emitted.push(JSON.parse(line)),
          read: (args) =>
            args[1] === "jobs" ? [job()] : args[1] === "job" ? job() : [pod()],
          spawn: () => {
            const child = new EventEmitter();
            child.stdout = new PassThrough();
            child.kill = (signal) => {
              assert.equal(signal, "SIGKILL");
              queueMicrotask(() => {
                joined = true;
                child.emit("close", 0);
              });
              return true;
            };
            setTimeout(() => cancellation.abort(), 5);
            return child;
          },
        },
      ),
      "UNKNOWN",
    );
    assert.equal(joined, true);
    assert.equal(emitted.at(-1).observation, "UNKNOWN_CANCELLED");
    assert.throws(() => lstatSync(path));
  } finally {
    if (previous === undefined) delete process.env.KUBECONFIG;
    else process.env.KUBECONFIG = previous;
    rmSync(dir, { recursive: true, force: true });
  }
});
