import assert from "node:assert/strict";
import test from "node:test";
import { execFileSync } from "node:child_process";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import {
  buildRecoveryReaderPlan,
  sameRecoveryReaderPlan,
  inspectRecoveryReaderSource,
  recoveryBuildContextRules,
  requireExistingRecoveryMount,
} from "./recover-local-image-admission-reader.mjs";
const uid = (n) => `${String(n).padStart(8, "0")}-0000-4000-8000-000000000001`;
const image = `registry.local.kodex/kodex/image-admission@sha256:${"1".repeat(64)}`,
  readerImage = image.replace("1".repeat(64), "2".repeat(64));
const now = Date.parse("2026-10-06T06:00:00Z"),
  revision = "a".repeat(40),
  run = `v20261006050000-${revision}`,
  id = "b".repeat(32),
  policyName = "kodex-image-admission-policy";
const metadata = (name, n, scoped = true) => ({
  name,
  ...(scoped ? { namespace: "kodex-system" } : {}),
  uid: uid(n),
  resourceVersion: String(n),
  labels: { "kodex.dev/local-profile": "hot-reload" },
});

test("canonical manifest явно снимает reader pause по тому же env.name", () => {
  const path = fileURLToPath(
    new URL(
      "../../deploy/k8s/base/image-supply-chain/image-admission-controller.yaml",
      import.meta.url,
    ),
  );
  const deployment = JSON.parse(
    execFileSync(
      "yq",
      [
        "-o=json",
        "-I=0",
        'select(.kind == "Deployment" and .metadata.name == "image-admission-controller")',
        path,
      ],
      { encoding: "utf8", timeout: 5000 },
    ),
  );
  const key = "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS",
    desired = deployment.spec.template.spec.containers[0].env;
  assert.deepEqual(
    desired.filter((e) => e.name === key),
    [{ name: key, value: "false" }],
  );
  const state = snapshot();
  state.controller.spec.template.spec.containers[0].env.push({
    name: key,
    value: "false",
  });
  const plan = buildRecoveryReaderPlan(state, {
    phase: "reader",
    readerImage,
    source: "/fixture/source",
    revision,
    now,
  });
  const paused = plan.after.template.spec.containers[0].env;
  assert.deepEqual(
    paused.filter((e) => e.name === key),
    [{ name: key, value: "true" }],
  );
  // Canonical apply объявляет прежний patch-owned ключ явно, а не удалением.
  const resumed = new Map([...paused, ...desired].map((e) => [e.name, e]));
  assert.deepEqual(resumed.get(key), { name: key, value: "false" });
  assert.deepEqual(
    plan.policy,
    buildRecoveryReaderPlan(snapshot(), {
      phase: "reader",
      readerImage,
      source: "/fixture/source",
      revision,
      now,
    }).policy,
  );
});

test("paused reader принимает exact clean existing Git source с ignored private env, но не cutover", (t) => {
  const root = mkdtempSync(join(tmpdir(), "kodex-reader-real-git-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  chmodSync(root, 0o755);
  const git = (...args) =>
    execFileSync("git", ["-C", root, ...args], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  git("init");
  git("config", "user.name", "fixture");
  git("config", "user.email", "fixture@example.invalid");
  git("remote", "add", "origin", "https://github.com/codex-k8s/kodex");
  mkdirSync(join(root, "tools/dev"), { recursive: true, mode: 0o755 });
  writeFileSync(
    join(root, "tools/dev/Dockerfile.local-image-supply-chain.dockerignore"),
    recoveryBuildContextRules.join("\n"),
    { mode: 0o644 },
  );
  writeFileSync(join(root, ".gitignore"), ".env\n", { mode: 0o644 });
  git("add", ".");
  git("commit", "-m", "fixture");
  const expected = git("rev-parse", "HEAD"),
    env = join(root, ".env");
  writeFileSync(env, "fixture-do-not-read", { mode: 0o600 });
  chmodSync(env, 0o000); // Успех с unreadable env доказывает отсутствие чтения.
  for (const remote of [
    "https://github.com/codex-k8s/kodex",
    "https://github.com/codex-k8s/kodex.git",
    "git@github.com:codex-k8s/kodex",
    "git@github.com:codex-k8s/kodex.git",
  ]) {
    git("remote", "set-url", "origin", remote);
    assert.equal(inspectRecoveryReaderSource(root).revision, expected);
  }
  for (const remote of [
    "https://github.com/foreign/kodex",
    "https://fixture@github.com/codex-k8s/kodex",
    "https://github.com/codex-k8s/kodex?extra=1",
  ]) {
    git("remote", "set-url", "origin", remote);
    assert.throws(() => inspectRecoveryReaderSource(root), /CHECKOUT/);
  }
  git("remote", "set-url", "origin", "https://github.com/codex-k8s/kodex");
  writeFileSync(join(root, "untracked.go"), "fixture");
  assert.throws(() => inspectRecoveryReaderSource(root), /CHECKOUT/);
  rmSync(join(root, "untracked.go"));
  chmodSync(env, 0o644);
  assert.throws(
    () => inspectRecoveryReaderSource(root),
    /PRIVATE_INPUT_INVALID/,
  );
  chmodSync(env, 0o000);
  writeFileSync(join(root, ".gitignore"), "");
  git("add", ".gitignore");
  git("commit", "-m", "fixture ignored path removed");
  assert.throws(() => inspectRecoveryReaderSource(root), /CHECKOUT/);
});

test("каждый существующий trusted mount сверяется без нового mount/source", () => {
  for (const name of [
    "control-plane",
    "control-api-gateway",
    "staff-control-center",
  ]) {
    const path =
      name === "staff-control-center"
        ? "/workspace/services/staff/control-center"
        : "/workspace";
    const source = "/fixture/source",
      state = "/fixture/state";
    const workload = {
      spec: {
        template: {
          metadata: {
            labels: { "kodex.dev/security-profile": "trusted-cluster" },
            annotations: {
              "kodex.dev/source-root": source,
              "kodex.dev/cache-root": state + "/cache",
            },
          },
          spec: {
            containers: [
              {
                name,
                volumeMounts: [
                  { name: "source", mountPath: path, readOnly: true },
                ],
              },
            ],
            volumes: [
              {
                name: "source",
                hostPath: {
                  path:
                    source +
                    (name === "staff-control-center"
                      ? "/services/staff/control-center"
                      : ""),
                },
              },
            ],
          },
        },
      },
    };
    requireExistingRecoveryMount(workload, source, state, name);
    for (const change of [
      (w) =>
        (w.spec.template.spec.containers[0].volumeMounts[0].readOnly = false),
      (w) => (w.spec.template.spec.volumes[0].hostPath.path += "/foreign"),
      (w) =>
        (w.spec.template.spec.containers[0].volumeMounts[0].subPath = "extra"),
      (w) =>
        (w.spec.template.metadata.labels["kodex.dev/security-profile"] =
          "protected"),
      (w) =>
        (w.spec.template.metadata.annotations["kodex.dev/cache-root"] =
          "/foreign"),
    ]) {
      const bad = structuredClone(workload);
      change(bad);
      assert.throws(
        () => requireExistingRecoveryMount(bad, source, state, name),
        /EXISTING_SOURCE_MOUNT/,
      );
    }
  }
});

test("Dockerfile-specific context закрывает все COPY targets и исключает private inputs до передачи", () => {
  const ignored = readFileSync(
    new URL(
      "./Dockerfile.local-image-supply-chain.dockerignore",
      import.meta.url,
    ),
    "utf8",
  )
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter((s) => s && !s.startsWith("#"));
  assert.deepEqual(ignored, recoveryBuildContextRules);
  const dockerfile = readFileSync(
    new URL("./Dockerfile.local-image-supply-chain", import.meta.url),
    "utf8",
  );
  const sources = [
    ...dockerfile.matchAll(/^COPY (?!.*--from=)(?:--chmod=\d+ )?(\S+) /gm),
  ].map((m) => m[1]);
  assert.deepEqual(sources, [
    "libs/go/",
    "services/jobs/role-image-builder/",
    "tools/render-image-admission-job.sh",
    "tools/render-image-admission-job.sh",
  ]);
  // Публичный bounded тест использует matcher/ignorefile самого Docker/Moby.
  execFileSync(
    "go",
    [
      "-C",
      fileURLToPath(
        new URL("./image-admission-build-context", import.meta.url),
      ),
      "test",
      "-count=1",
      "./...",
    ],
    {
      env: {
        ...process.env,
        GOTOOLCHAIN: "go1.26.6",
        GOENV: "off",
        GOWORK: "off",
        GOFLAGS: "-mod=readonly",
      },
      timeout: 60_000,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  const builder = readFileSync(
    new URL("./build-local-image-supply-chain.sh", import.meta.url),
    "utf8",
  );
  assert.match(
    builder,
    /tools\/dev\/Dockerfile\.local-image-supply-chain \\\n\s+tools\/dev\/Dockerfile\.local-image-supply-chain\.dockerignore \\/,
  );
});
function snapshot() {
  const policy = {
    kind: "ConfigMap",
    metadata: metadata(policyName, 2),
    immutable: true,
    data: { admissionImage: image, orchestrationRevision: revision },
  };
  Object.assign(policy.metadata.labels, {
    "kodex.dev/owner-intent": "true",
    "kodex.dev/security-profile": "trusted-cluster",
  });
  const controller = {
    kind: "Deployment",
    metadata: { ...metadata("image-admission-controller", 1), generation: 1 },
    spec: {
      replicas: 1,
      strategy: { type: "Recreate" },
      template: {
        spec: {
          containers: [
            {
              name: "image-admission-controller",
              image,
              command: ["/usr/local/bin/image-admission-controller"],
              env: [
                {
                  name: "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP",
                  value: policyName,
                },
                { name: "KODEX_RPC_PROFILE", value: "trusted-cluster" },
              ],
            },
          ],
        },
      },
    },
    status: {
      observedGeneration: 1,
      replicas: 1,
      updatedReplicas: 1,
      readyReplicas: 1,
      availableReplicas: 1,
    },
  };
  const workspace = {
    kind: "PersistentVolumeClaim",
    metadata: {
      ...metadata(`mc-admit-${id}`, 5),
      creationTimestamp: "2026-10-06T05:00:00Z",
      annotations: {
        "kodex.dev/admission-run-id": run,
        "kodex.dev/admission-recovery-uid": uid(10),
        "kodex.dev/admission-recovery-after": "2026-10-06T05:59:00Z",
      },
    },
    spec: { accessModes: ["ReadWriteOnce"] },
  };
  Object.assign(workspace.metadata.labels, {
    "kodex.dev/image-admission-orchestrated": "true",
    "kodex.dev/image-admission-id": id,
  });
  const job = {
    kind: "Job",
    metadata: metadata(`mc-admit-${id}-claim`, 6),
    spec: {
      template: {
        spec: {
          containers: [
            {
              name: "claim",
              image,
              command: ["/bin/sh", "/opt/kodex/image-admission.sh", "claim"],
            },
          ],
        },
      },
    },
    status: { conditions: [{ type: "Complete", status: "True" }] },
  };
  Object.assign(job.metadata.labels, workspace.metadata.labels, {
    "kodex.dev/image-admission-phase": "claim",
  });
  job.metadata.annotations = { "kodex.dev/admission-run-id": run };
  return {
    clusterUID: uid(7),
    namespaceUID: uid(8),
    controller,
    policy,
    parameters: {
      kind: "ImageAdmissionPolicyParameters",
      metadata: metadata(policyName, 3),
      spec: structuredClone(policy.data),
    },
    binding: {
      kind: "ValidatingAdmissionPolicyBinding",
      metadata: metadata("kodex-image-admission-controller-jobs", 4, false),
      spec: {
        paramRef: {
          name: policyName,
          namespace: "kodex-system",
          parameterNotFoundAction: "Deny",
        },
        validationActions: ["Deny"],
      },
    },
    jobs: [job],
    workspaces: [workspace],
  };
}
const options = {
  phase: "reader",
  readerImage,
  source: "/srv/kodex",
  revision,
  now,
};
test("reader меняет только image/pause, сохраняя old immutable policy/cursor; pending не объявляется idle", () => {
  const state = snapshot(),
    before = structuredClone(state),
    plan = buildRecoveryReaderPlan(state, options);
  assert.deepEqual(state, before);
  const expected = structuredClone(plan.before),
    app = expected.template.spec.containers[0];
  app.image = readerImage;
  app.env.push({
    name: "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS",
    value: "true",
  });
  assert.deepEqual(plan.after, expected);
  assert.equal(plan.policy.uid, state.policy.metadata.uid);
  assert.equal(plan.work.length, 2);
  sameRecoveryReaderPlan(
    plan,
    buildRecoveryReaderPlan(state, { ...options, now: now + 1 }),
    now + 1,
  );
});
test("delivery fail-closed для resource/CM/run/cursor/Job/profile drift и отсутствующей stale workspace", () => {
  const mutations = [
    (s) => (s.controller.metadata.uid = "foreign"),
    (s) => (s.controller.spec.strategy.type = "RollingUpdate"),
    (s) =>
      (s.controller.spec.template.spec.containers[0].env[1].value =
        "service-v1"),
    (s) => (s.policy.immutable = false),
    (s) => (s.parameters.spec.admissionImage = readerImage),
    (s) => (s.binding.spec.paramRef.parameterNotFoundAction = "Allow"),
    (s) => (s.workspaces = []),
    (s) => s.workspaces.push(structuredClone(s.workspaces[0])),
    (s) =>
      (s.workspaces[0].metadata.annotations[
        "kodex.dev/admission-recovery-uid"
      ] = "foreign"),
    (s) =>
      (s.workspaces[0].metadata.annotations["kodex.dev/admission-run-id"] =
        run.replace(revision, "c".repeat(40))),
    (s) =>
      (s.jobs[0].metadata.annotations["kodex.dev/admission-run-id"] =
        "foreign"),
    (s) => (s.jobs[0].spec.template.spec.containers[0].image = readerImage),
    (s) =>
      (s.jobs[0].metadata.labels["kodex.dev/image-admission-phase"] =
        "promote"),
    (s) =>
      (s.workspaces[0].metadata.creationTimestamp = "2026-10-04T05:00:00Z"),
  ];
  for (const mutate of mutations) {
    const s = snapshot();
    mutate(s);
    assert.throws(() => buildRecoveryReaderPlan(s, options));
  }
});
test("fresh plan UID/RV/spec/policy/inventory/source/image CAS закрывает drift и replay", () => {
  const s = snapshot(),
    plan = buildRecoveryReaderPlan(s, options);
  for (const mutate of [
    (x) => (x.controller.metadata.resourceVersion = "99"),
    (x) => (x.policy.metadata.resourceVersion = "99"),
    (x) => (x.jobs[0].metadata.uid = uid(99)),
    (x) =>
      (x.workspaces[0].metadata.annotations[
        "kodex.dev/admission-recovery-uid"
      ] = uid(99)),
  ]) {
    const x = structuredClone(s);
    mutate(x);
    assert.throws(
      () =>
        sameRecoveryReaderPlan(
          plan,
          buildRecoveryReaderPlan(x, { ...options, now: now + 1 }),
          now + 1,
        ),
      /DRIFT/,
    );
  }
  assert.throws(
    () => sameRecoveryReaderPlan(plan, plan, now + 60_001),
    /FRESH/,
  );
  assert.throws(
    () => sameRecoveryReaderPlan({ ...plan, kind: "other" }, plan, now),
    /FRESH/,
  );
});
test("reader не возобновляет B2 старым bridge даже после cleanup", () => {
  const s = snapshot(),
    reader = buildRecoveryReaderPlan(s, options);
  s.controller.spec = reader.after;
  assert.throws(
    () => buildRecoveryReaderPlan(s, { ...options, phase: "resume" }),
    /INPUT/,
  );
  s.jobs = [];
  s.workspaces = [];
  assert.throws(
    () => buildRecoveryReaderPlan(s, { ...options, phase: "resume" }),
    /INPUT/,
  );
  assert.equal(
    reader.after.template.spec.containers[0].env.at(-1).value,
    "true",
  );
});

test("публичный CLI plan/apply делает один fenced Deployment PATCH, не читает Secrets/SQL и не меняет старую policy", (t) => {
  const root = mkdtempSync(join(tmpdir(), "kodex-recovery-reader-cli-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const source = join(root, "source"),
    state = join(root, "state"),
    bin = join(root, "bin");
  mkdirSync(source, { mode: 0o755 });
  mkdirSync(join(source, "tools/dev"), { recursive: true, mode: 0o755 });
  writeFileSync(
    join(source, "tools/dev/Dockerfile.local-image-supply-chain.dockerignore"),
    recoveryBuildContextRules.join("\n"),
  );
  writeFileSync(join(source, ".env"), "fixture-not-a-credential", {
    mode: 0o600,
  });
  mkdirSync(state, { mode: 0o700 });
  mkdirSync(bin, { mode: 0o755 });
  const current = snapshot();
  current.workspaces[0].metadata.creationTimestamp = new Date(
    Date.now() - 60_000,
  ).toISOString();
  current.workspaces[0].metadata.annotations[
    "kodex.dev/admission-recovery-after"
  ] = new Date(Date.now() - 1000).toISOString();
  const inventory = join(root, "inventory.json"),
    calls = join(root, "calls.jsonl");
  writeFileSync(inventory, JSON.stringify(current));
  writeFileSync(join(state, "image-admission-image"), readerImage, {
    mode: 0o600,
  });
  const git = join(bin, "git");
  writeFileSync(
    git,
    `#!/usr/bin/env node
const a=process.argv.slice(2);let out='';if(a.includes('--show-toplevel'))out=process.env.FIXTURE_SOURCE;else if(a.includes('remote'))out='https://github.com/codex-k8s/kodex';else if(a.includes('HEAD'))out=${JSON.stringify(revision)};process.stdout.write(out);`,
  );
  chmodSync(git, 0o755);
  const kube = join(bin, "kubectl");
  writeFileSync(
    kube,
    `#!/usr/bin/env node
const fs=require('node:fs'),a=process.argv.slice(2),p=process.env.FIXTURE_INVENTORY,s=JSON.parse(fs.readFileSync(p));fs.appendFileSync(process.env.FIXTURE_CALLS,JSON.stringify(a)+'\\n');let result;
if(a.includes('config')){process.stdout.write(a.includes('current-context')?'k3d-kodex':'https://127.0.0.1:12345');process.exit(0);}
if(a.includes('patch')){const ops=JSON.parse(a[a.indexOf('-p')+1]);for(const op of ops.filter(x=>x.op==='test')){const value=op.path==='/metadata/uid'?s.controller.metadata.uid:op.path==='/metadata/resourceVersion'?s.controller.metadata.resourceVersion:s.controller.spec;if(JSON.stringify(value)!==JSON.stringify(op.value))process.exit(2);}s.controller.spec=ops.at(-1).value;s.controller.metadata.resourceVersion='99';fs.writeFileSync(p,JSON.stringify(s));result=s.controller;}
else if(a.includes('get')){const i=a.indexOf('get'),kind=a[i+1],name=a[i+2];if(kind==='namespace')result={metadata:{uid:name==='kube-system'?s.clusterUID:s.namespaceUID}};else if(kind==='deployment')result=name!=='image-admission-controller'?{spec:{template:{metadata:{labels:{'kodex.dev/security-profile':'trusted-cluster'},annotations:{'kodex.dev/source-root':process.env.FIXTURE_SOURCE,'kodex.dev/cache-root':process.env.FIXTURE_STATE+'/cache'}},spec:{containers:[{name,volumeMounts:[{name:'source',mountPath:name==='staff-control-center'?'/workspace/services/staff/control-center':'/workspace',readOnly:true}]}],volumes:[{name:'source',hostPath:{path:process.env.FIXTURE_SOURCE+(name==='staff-control-center'?'/services/staff/control-center':'')}}]}}}}:s.controller;else if(kind==='configmap')result=s.policy;else if(kind==='imageadmissionpolicyparameters')result=s.parameters;else if(kind==='validatingadmissionpolicybinding')result=s.binding;else if(kind==='jobs')result={items:s.jobs};else if(kind==='persistentvolumeclaims')result={items:s.workspaces};else process.exit(3);}else process.exit(4);process.stdout.write(JSON.stringify(result));`,
  );
  chmodSync(kube, 0o755);
  const cli = fileURLToPath(
      new URL("./recover-local-image-admission-reader.mjs", import.meta.url),
    ),
    plan = join(state, "plan.json"),
    evidence = join(state, "evidence.jsonl");
  const env = {
    ...process.env,
    PATH: bin + ":" + process.env.PATH,
    FIXTURE_SOURCE: source,
    FIXTURE_STATE: state,
    FIXTURE_INVENTORY: inventory,
    FIXTURE_CALLS: calls,
  };
  const common = [
    "--context",
    "k3d-kodex",
    "--phase",
    "reader",
    "--source-root",
    source,
    "--expected-sha",
    revision,
    "--state-directory",
    state,
  ];
  execFileSync(process.execPath, [cli, "plan", ...common, "--output", plan], {
    env,
    timeout: 15_000,
  });
  const oldPolicy = structuredClone(current.policy);
  execFileSync(
    process.execPath,
    [
      cli,
      "apply",
      ...common,
      "--plan",
      plan,
      "--evidence",
      evidence,
      "--confirm",
      "DELIVER-TRUSTED-ADMISSION-RECOVERY-READER",
    ],
    { env, timeout: 15_000 },
  );
  const actual = JSON.parse(readFileSync(inventory));
  assert.equal(
    actual.controller.spec.template.spec.containers[0].image,
    readerImage,
  );
  assert.equal(
    actual.controller.spec.template.spec.containers[0].env.at(-1).value,
    "true",
  );
  assert.deepEqual(actual.policy, oldPolicy);
  assert.deepEqual(actual.workspaces, current.workspaces);
  assert.deepEqual(actual.jobs, current.jobs);
  const requests = readFileSync(calls, "utf8")
    .trim()
    .split("\n")
    .map(JSON.parse);
  assert.equal(requests.filter((a) => a.includes("patch")).length, 1);
  assert.ok(
    requests
      .filter((a) => a.includes("patch"))
      .every((a) => a.includes("--field-manager=kodex-local-dev")),
  );
  assert.ok(
    requests.every(
      (a) =>
        a.some((v) => v.startsWith("--cache-dir=" + state + "/")) &&
        !a.includes("secret") &&
        !a.includes("exec") &&
        !a.includes("delete") &&
        !a.includes("create"),
    ),
  );
  assert.deepEqual(
    readFileSync(evidence, "utf8")
      .trim()
      .split("\n")
      .map(JSON.parse)
      .map((r) => r.status),
    ["INTENT", "APPLIED"],
  );
});
