import assert from "node:assert/strict";
import test from "node:test";
import {
  classifyWorkerLine,
  sessionBinding,
  validPVC,
  validJobPod,
  metadataTemplates,
} from "./watch-session-archive.mjs";

test("metadata projections preserve the closing JSON object delimiter", () => {
  for (const template of Object.values(metadataTemplates)) {
    // Printf-блоки заменяются синтетическими строками, condition — boolean.
    const fixture = template
      .replace(
        /{{if .metadata.deletionTimestamp}}true{{else}}false{{end}}/g,
        "false",
      )
      .replace(/{{printf .*?}}/g, '"fixture"');
    assert.doesNotThrow(() => JSON.parse(fixture));
    assert.ok(!template.includes(".data") && !template.includes(".spec"));
  }
});

test("fixed producer stages are closed and do not expose raw messages", () => {
  assert.equal(
    classifyWorkerLine("write session archive worker result"),
    "RESULT_WRITE_FAILED",
  );
  assert.equal(
    classifyWorkerLine("session archive worker configuration is invalid"),
    "CONFIGURATION_INVALID",
  );
  for (const [message, stage] of [
    ["session source file identity is invalid", "SOURCE_IDENTITY_INVALID"],
    ["session source file digest mismatch", "SOURCE_DIGEST_MISMATCH"],
    ["put session archive object", "OBJECT_PUT_FAILED"],
    ["read back session archive object", "OBJECT_READBACK_FAILED"],
    ["session archive object readback mismatch", "OBJECT_READBACK_MISMATCH"],
    ["inspect session source component 0", "SOURCE_COMPONENT_UNAVAILABLE"],
    ["inspect session source component 31", "SOURCE_COMPONENT_UNAVAILABLE"],
  ])
    assert.equal(
      classifyWorkerLine(`session archive worker failed: ${message}`),
      stage,
    );
});

test("unknown, arbitrary suffixes, private errors and oversized lines stay UNKNOWN", () => {
  for (const value of [
    "private sentinel token=synthetic",
    "session archive worker failed: put session archive object?token=synthetic",
    "session archive worker failed: put session archive object\nsecret=synthetic",
    "session archive worker failed: inspect session source component 32",
    "session archive worker failed: inspect session source component 00",
    "session archive worker failed: session source file identity is invalid\r",
    "session archive worker failed: " + "x".repeat(4096),
    null,
    "write session archive worker result\nprivate sentinel",
  ])
    assert.equal(classifyWorkerLine(value), "UNKNOWN");
});

test("model and worker task errors use exact closed stages only", () => {
  for (const [message, stage] of [
    ["session archive task is missing", "TASK_MISSING"],
    ["read session archive task", "TASK_READ_FAILED"],
    ["decode session archive task", "TASK_DECODE_FAILED"],
    ["session archive task identity is invalid", "TASK_IDENTITY_INVALID"],
    ["session archive task binding is invalid", "TASK_BINDING_INVALID"],
    ["session archive PVC binding is invalid", "TASK_PVC_BINDING_INVALID"],
    ["session snapshot task is invalid", "SNAPSHOT_TASK_INVALID"],
    ["worker task kind is unsupported", "TASK_KIND_UNSUPPORTED"],
  ]) {
    const line = `session archive worker failed: ${message}`;
    assert.equal(classifyWorkerLine(line), stage);
    for (const suffix of [
      " private sentinel",
      "?token=synthetic",
      "\nheader=synthetic",
      "\r",
    ]) {
      assert.equal(classifyWorkerLine(line + suffix), "UNKNOWN");
    }
    assert.equal(classifyWorkerLine(message), "UNKNOWN");
  }
});

test("exact session, owner, PVC UID and Pod Job owner are required", () => {
  const binding = sessionBinding({
    sessionRef: "ses_fixture01",
    organizationRef: "org_fixture01",
  });
  const pvc = {
    uid: "uid-pvc",
    phase: "Bound",
    deleting: false,
    managed: "true",
    ...binding,
  };
  const pod = {
    name: "session-archive-fixture-pod",
    uid: "uid-pod",
    managed: "true",
    phase: "Running",
    worker: true,
    pvcs: [binding.pvc],
    owners: [
      {
        kind: "Job",
        name: "session-archive-fixture",
        uid: "uid-job",
        controller: true,
      },
    ],
  };
  const job = { uid: "uid-job", managed: "true", pvcUID: pvc.uid };
  assert.equal(validJobPod(pod, null, pvc, binding), false);
  assert.equal(validJobPod(pod, {}, pvc, binding), false);
  assert.equal(validPVC(pvc, binding), true);
  assert.equal(validJobPod(pod, job, pvc, binding), true);
  for (const change of [
    { organization: "foreign" },
    { project: "foreign" },
    { session: "foreign" },
    { deleting: true },
    { phase: "Pending" },
    { managed: "false" },
  ])
    assert.equal(validPVC({ ...pvc, ...change }, binding), false);
  for (const change of [
    { pvcs: ["foreign"] },
    { pvcs: [binding.pvc, "foreign"] },
    { owners: [] },
    { worker: false },
    { managed: "false" },
  ])
    assert.equal(validJobPod({ ...pod, ...change }, job, pvc, binding), false);
  for (const change of [
    { uid: "foreign" },
    { pvcUID: "replaced" },
    { managed: "false" },
  ])
    assert.equal(validJobPod(pod, { ...job, ...change }, pvc, binding), false);
  assert.throws(() =>
    sessionBinding({
      sessionRef: "../../private",
      organizationRef: "org_fixture01",
    }),
  );
});
