import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { spawnSync } from "node:child_process";
import test from "node:test";
import {
  phaseBinding,
  validPhaseJob,
  validPhasePod,
  terminationProjection,
  watchPhase,
  jobTemplate,
  podTemplate,
} from "./watch-image-admission-phase.mjs";

const binding = phaseBinding({
  jobRef: "mc-admit-" + "c".repeat(32) + "-scan",
  admissionRunId: "v20261006001531-" + "a".repeat(40),
});
const job = () => ({
  name: binding.jobRef,
  namespace: "kodex-system",
  uid: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  managed: "true",
  id: binding.id,
  phase: binding.phase,
  runId: binding.admissionRunId,
  serviceAccount: binding.serviceAccount,
  automount: false,
  restartPolicy: "Never",
  containers: [
    {
      name: "scan",
      command: ["/bin/sh", "/opt/kodex/image-admission.sh", "scan"],
    },
  ],
});
const pod = () => ({
  name: binding.jobRef + "-fixture",
  namespace: "kodex-system",
  uid: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
  id: binding.id,
  phase: binding.phase,
  containers: structuredClone(job().containers),
  owners: [
    { kind: "Job", controller: true, name: binding.jobRef, uid: job().uid },
  ],
  termination: null,
});
const terminated = () => ({
  ...pod(),
  termination: {
    reason: "OOMKilled",
    exitCode: 137,
    signal: 9,
    startedAt: "2026-10-06T00:15:31Z",
    finishedAt: "2026-10-06T00:18:30Z",
  },
});

test("metadata identity and exact workload commands reject foreign replacements", () => {
  assert.equal(validPhaseJob(job(), binding), true);
  assert.equal(validPhasePod(pod(), job(), binding), true);
  for (const mutate of [
    (j) => {
      j.uid += "\n";
    },
    (j) => {
      j.runId = "foreign";
    },
    (j) => {
      j.managed = "false";
    },
    (j) => {
      j.serviceAccount = "image-admission";
    },
    (j) => {
      j.containers[0].command.push("foreign");
    },
    (_j, p) => {
      p.owners[0].uid = "cccccccc-cccc-cccc-cccc-cccccccccccc";
    },
    (_j, p) => {
      p.phase = "sign";
    },
  ]) {
    const j = job(),
      p = pod();
    mutate(j, p);
    assert.equal(validPhasePod(p, j, binding), false);
  }
  assert.throws(() =>
    phaseBinding({
      ...binding,
      jobRef: binding.jobRef.replace("scan", "admit"),
    }),
  );
  assert.doesNotMatch(
    jobTemplate + podTemplate,
    /\.message|\.env|\.data|secret|volumeMount|\.image/,
  );
});

test("termination outputs only closed reason/numeric/timestamp metadata", () => {
  const observed = terminationProjection(
    terminated(),
    job(),
    binding,
    pod().uid,
  );
  assert.equal(observed.reason, "OOM_KILLED");
  assert.equal(observed.exitCode, 137);
  assert.equal(observed.signal, 9);
  const arbitrary = terminated();
  arbitrary.termination.reason = "private-secret-must-not-leak";
  arbitrary.termination.message = "raw-log-must-not-leak";
  assert.equal(
    terminationProjection(arbitrary, job(), binding, pod().uid).reason,
    "UNKNOWN",
  );
  assert.doesNotMatch(
    JSON.stringify(terminationProjection(arbitrary, job(), binding, pod().uid)),
    /private-secret|raw-log|message/,
  );
  assert.equal(
    terminationProjection(
      terminated(),
      job(),
      binding,
      "cccccccc-cccc-cccc-cccc-cccccccccccc",
    ),
    null,
  );
  for (const changes of [
    { exitCode: -1 },
    { signal: 65 },
    { finishedAt: "unknown" },
    { finishedAt: "2026-10-06T00:14:30Z" },
  ]) {
    const p = terminated();
    Object.assign(p.termination, changes);
    assert.equal(terminationProjection(p, job(), binding, pod().uid), null);
  }
  assert.equal(terminationProjection(pod(), job(), binding, pod().uid), null);
});

test("Pod watch captures termination before Job cleanup, joins child, and never reads logs", async () => {
  const previous = process.env.KUBECONFIG;
  process.env.KUBECONFIG = "/home/s/.kube/config";
  const outputs = [],
    calls = [];
  let deleted = false,
    joined = false;
  try {
    assert.equal(
      await watchPhase(
        { ...binding, timeoutSeconds: 1 },
        {
          configMetadata: {
            isFile: () => true,
            isSymbolicLink: () => false,
            uid: process.getuid(),
            mode: 0o600,
          },
          emit: (value) => outputs.push(value),
          read: (args) => {
            calls.push(args);
            if (deleted) return null;
            if (args[1] === "job") return job();
            if (args[1] === "pods") return [pod()];
            return pod();
          },
          spawn: (binary, args, options) => {
            assert.equal(binary, "kubectl");
            assert.ok(args.includes("--watch"));
            assert.equal(args.includes("logs"), false);
            assert.deepEqual(Object.keys(options.env).sort(), [
              "KUBECONFIG",
              "PATH",
            ]);
            const child = new EventEmitter();
            child.stdout = new PassThrough();
            child.kill = () => {
              queueMicrotask(() => {
                joined = true;
                child.emit("close", 0);
              });
              return true;
            };
            setTimeout(() => {
              deleted = true;
              const foreign = terminated();
              foreign.uid = "cccccccc-cccc-cccc-cccc-cccccccccccc";
              child.stdout.write(JSON.stringify(foreign) + "\n");
              child.stdout.write(JSON.stringify(terminated()) + "\n");
            }, 5);
            return child;
          },
        },
      ),
      "OBSERVED",
    );
    assert.equal(joined, true);
    assert.equal(outputs.at(-1).event, "PHASE_TERMINATION_OBSERVED");
    assert.equal(outputs.at(-1).exitCode, 137);
    assert.ok(
      calls.every(
        (args) => args[0] === "get" && ["job", "pod", "pods"].includes(args[1]),
      ),
    );
  } finally {
    if (previous === undefined) delete process.env.KUBECONFIG;
    else process.env.KUBECONFIG = previous;
  }
});

test("CLI unknown keys fail without raw errors or cluster reads", () => {
  const result = spawnSync(
    process.execPath,
    [
      new URL("./watch-image-admission-phase.mjs", import.meta.url).pathname,
      "--unknown",
      "private-value",
    ],
    { encoding: "utf8" },
  );
  assert.equal(result.status, 1);
  assert.equal(result.stderr, "");
  assert.equal(
    result.stdout.trim(),
    '{"event":"PHASE_WATCH_FAILED","code":"METADATA_UNAVAILABLE"}',
  );
});
