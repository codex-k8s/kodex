import test from "node:test";
import assert from "node:assert/strict";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  readFileSync,
  rmSync,
  chmodSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { fingerprint } from "./scoped-release.mjs";
import { planSourceChange } from "./application-source.mjs";
import {
  frontendDependencyState,
  planFrontendDependencies,
} from "./frontend-dependency-model.mjs";
import {
  main,
  verifyFrontendCache,
} from "./frontend-dependency-transition.mjs";

const image = `docker.io/library/node:24-alpine@sha256:${"1".repeat(64)}`;
const releaseID = "22222222-2222-4222-8222-222222222222";
function fixture(root = "/tmp/frontend-fixture") {
  const old = `${root}/old`,
    next = `${root}/next`,
    identity = "3".repeat(64);
  const cachePath = `${root}/cache/frontend-v1/${identity}/node_modules`;
  const volumes = [
    [
      "dev-frontend-source",
      `${old}/services/staff/control-center`,
      "Directory",
      "/workspace/services/staff/control-center",
    ],
    [
      "dev-node-modules",
      `${root}/cache/frontend-v1/${"4".repeat(64)}/node_modules`,
      "Directory",
      "/workspace/services/staff/control-center/node_modules",
    ],
    [
      "dev-frontend-runner",
      `${old}/tools/dev/run-frontend.sh`,
      "File",
      "/workspace/tools/dev/run-frontend.sh",
    ],
    [
      "dev-frontend-identity",
      `${old}/tools/dev/frontend-cache-identity.sh`,
      "File",
      "/workspace/tools/dev/frontend-cache-identity.sh",
    ],
  ];
  const deployment = {
    apiVersion: "apps/v1",
    kind: "Deployment",
    metadata: {
      name: "staff-control-center",
      namespace: "kodex-system",
      uid: "11111111-1111-4111-8111-111111111111",
      resourceVersion: "10",
      generation: 1,
      labels: {
        "app.kubernetes.io/part-of": "kodex",
        "kodex.dev/local-profile": "hot-reload",
      },
    },
    spec: {
      replicas: 1,
      strategy: {
        type: "RollingUpdate",
        rollingUpdate: { maxUnavailable: 0, maxSurge: 1 },
      },
      template: {
        metadata: {
          annotations: {
            "kubectl.kubernetes.io/default-container": "staff-control-center",
            "kodex.dev/frontend-drain-profile": "1",
          },
        },
        spec: {
          terminationGracePeriodSeconds: 45,
          containers: [
            {
              name: "staff-control-center",
              image,
              command: ["/workspace/tools/dev/run-frontend.sh"],
              args: [],
              workingDir: "/workspace/services/staff/control-center",
              lifecycle: { preStop: { sleep: { seconds: 10 } } },
              env: [{ name: "KODEX_DEV_NODE_IMAGE", value: image }],
              volumeMounts: volumes.map(([name, , , mountPath]) => ({
                name,
                mountPath,
                readOnly: true,
              })),
            },
          ],
          volumes: volumes.map(([name, path, type]) => ({
            name,
            hostPath: { path, type },
          })),
        },
      },
    },
    status: {
      observedGeneration: 1,
      replicas: 1,
      updatedReplicas: 1,
      availableReplicas: 1,
    },
  };
  const source = { path: next, revision: "a".repeat(40) },
    previousSource = { path: old, revision: "b".repeat(40) };
  const cache = {
    path: cachePath,
    source: next,
    revision: source.revision,
    identity,
    image,
    manifestsSHA256: "c".repeat(64),
  };
  return { deployment, source, previousSource, cache, releaseID };
}
function patched(deployment, patch) {
  const result = structuredClone(deployment);
  for (const item of patch) {
    const path = item.path.slice(1).split("/"),
      key = path.pop();
    const parent = path.reduce((value, part) => value[part], result);
    if (item.op === "test") assert.deepEqual(parent[key], item.value);
    else parent[key] = structuredClone(item.value);
  }
  return result;
}

test("prepared frontend меняет только source/cache и application annotations", () => {
  const value = fixture(),
    plan = planFrontendDependencies(value.deployment, value),
    after = patched(value.deployment, plan.patch);
  assert.equal(fingerprint(after.spec), plan.afterSpecSHA256);
  assert.deepEqual(
    after.spec.template.spec.containers,
    value.deployment.spec.template.spec.containers,
  );
  assert.deepEqual(
    after.spec.template.spec.volumes.slice(2),
    value.deployment.spec.template.spec.volumes.slice(2),
  );
  assert.equal(
    after.spec.template.spec.volumes[0].hostPath.path,
    `${value.source.path}/services/staff/control-center`,
  );
  assert.equal(
    after.spec.template.spec.volumes[1].hostPath.path,
    value.cache.path,
  );
  assert.equal(
    plan.previousCache,
    value.deployment.spec.template.spec.volumes[1].hostPath.path,
  );
  assert.equal(plan.patch.filter((item) => item.op !== "test").length, 3);
  assert.throws(
    () =>
      planSourceChange(
        value.deployment,
        structuredClone(value.deployment.spec),
        0,
        value.source,
        (path) => ({
          revision:
            path === value.previousSource.path
              ? value.previousSource.revision
              : value.source.revision,
          dependenciesSHA256:
            path === value.previousSource.path
              ? "0".repeat(64)
              : "1".repeat(64),
          mountpointsReady: true,
        }),
      ),
    /FRONTEND_DEPENDENCY_PREPARATION_REQUIRED/,
  );
});

for (const [name, mutate] of [
  [
    "чужой component",
    (value) => {
      value.deployment.metadata.name = "control-api-gateway";
    },
  ],
  [
    "production profile",
    (value) => {
      value.deployment.metadata.labels["kodex.dev/local-profile"] =
        "production";
    },
  ],
  [
    "не готов",
    (value) => {
      value.deployment.status.availableReplicas = 0;
    },
  ],
  [
    "Recreate",
    (value) => {
      value.deployment.spec.strategy.type = "Recreate";
    },
  ],
  [
    "writable mount",
    (value) => {
      value.deployment.spec.template.spec.containers[0].volumeMounts[1].readOnly = false;
    },
  ],
  [
    "shared init mount",
    (value) => {
      value.deployment.spec.template.spec.initContainers = [
        { name: "other", volumeMounts: [{ name: "dev-node-modules" }] },
      ];
    },
  ],
  [
    "иной Node runtime",
    (value) => {
      value.cache.image = image.replace("24-alpine", "25-alpine");
    },
  ],
  [
    "чужая source revision",
    (value) => {
      value.cache.revision = "d".repeat(40);
    },
  ],
  [
    "подмена cache identity",
    (value) => {
      value.cache.identity = "e".repeat(64);
    },
  ],
  [
    "traversal cache",
    (value) => {
      value.cache.path = value.cache.path.replace("/cache/", "/cache/../");
    },
  ],
  [
    "неизвестный nested mount",
    (value) => {
      value.deployment.spec.template.spec.containers[0].volumeMounts.push({
        name: "other",
        mountPath: "/workspace/services/staff/control-center/other",
        readOnly: true,
      });
    },
  ],
])
  test(`prepared frontend закрыто отклоняет: ${name}`, () => {
    const value = fixture();
    mutate(value);
    assert.throws(() => planFrontendDependencies(value.deployment, value));
  });

test("CLI plan/apply/observe, immutable intent, fresh rollback и namespace", () => {
  const root = mkdtempSync(join(tmpdir(), "frontend-cas-"));
  try {
    const value = fixture(root);
    for (const source of [value.source, value.previousSource]) {
      mkdirSync(`${source.path}/tools/dev`, { recursive: true });
      for (const file of ["run-frontend.sh", "frontend-cache-identity.sh"])
        writeFileSync(`${source.path}/tools/dev/${file}`, "fixture");
    }
    let deployment = value.deployment,
      patches = 0,
      unknown = false;
    const calls = [],
      neighbor = {
        metadata: { name: "control-api-gateway", uid: "neighbor" },
        spec: { unchanged: true },
      };
    const run = (command, args) => {
      calls.push([command, args]);
      const argv = command === "sudo" ? args.slice(3) : args;
      if (argv.includes("namespace"))
        return JSON.stringify({ metadata: { uid: "namespace-uid" } });
      assert.ok(
        argv.includes("-n") && argv[argv.indexOf("-n") + 1] === "kodex-system",
      );
      if (argv.includes("patch")) {
        const next = patched(
          deployment,
          JSON.parse(argv[argv.indexOf("-p") + 1]),
        );
        if (argv.includes("--dry-run=server")) return JSON.stringify(next);
        deployment = next;
        deployment.metadata.resourceVersion = String(
          Number(deployment.metadata.resourceVersion) + 1,
        );
        patches++;
        if (unknown) throw new Error("ACK_LOST");
        return "deployment.apps/staff-control-center";
      }
      if (argv.includes("rollout")) return "rolled out";
      return JSON.stringify(
        argv.includes("deployments")
          ? { items: [deployment, neighbor] }
          : deployment,
      );
    };
    const io = {
      run,
      inspectSource: (path) => ({
        revision:
          path === value.source.path
            ? value.source.revision
            : value.previousSource.revision,
      }),
      verifyCache: (source, path, runtime) => ({
        ...value.cache,
        source: source.path,
        revision: source.revision,
        path,
        image: runtime,
        identity: path.split("/").at(-2),
      }),
    };
    const planPath = `${root}/plan.json`,
      evidence = `${root}/evidence.jsonl`;
    assert.equal(
      main(
        [
          "plan",
          "--context",
          "default",
          "--source",
          value.source.path,
          "--revision",
          value.source.revision,
          "--cache",
          value.cache.path,
          "--output",
          planPath,
          "--k3s-sudo",
        ],
        io,
      ).status,
      "PLANNED",
    );
    const apply = () =>
      main(
        [
          "apply",
          "--context",
          "default",
          "--plan",
          planPath,
          "--evidence",
          evidence,
          "--confirm",
          "APPLY-STAGING-FRONTEND-DEPENDENCIES",
          "--k3s-sudo",
        ],
        io,
      );
    deployment.metadata.resourceVersion = "11";
    assert.throws(apply, /FRONTEND_PRECONDITION_CHANGED/);
    deployment.metadata.resourceVersion = "10";
    neighbor.spec.unchanged = false;
    assert.throws(apply, /FRONTEND_PLAN_BOUNDARY_CHANGED/);
    neighbor.spec.unchanged = true;
    const savedPlan = readFileSync(planPath, "utf8"),
      tampered = JSON.parse(savedPlan);
    tampered.target.patch.push({
      op: "replace",
      path: "/spec/template/spec/containers/0/image",
      value: "foreign",
    });
    writeFileSync(planPath, JSON.stringify(tampered));
    assert.throws(apply, /FRONTEND_PLAN_CHANGED/);
    writeFileSync(planPath, savedPlan);
    assert.equal(patches, 0);
    unknown = true;
    assert.equal(
      main(
        [
          "apply",
          "--context",
          "default",
          "--plan",
          planPath,
          "--evidence",
          evidence,
          "--confirm",
          "APPLY-STAGING-FRONTEND-DEPENDENCIES",
          "--k3s-sudo",
        ],
        io,
      ).status,
      "UNKNOWN",
    );
    assert.equal(patches, 1);
    assert.equal(
      main(
        ["observe", "--context", "default", "--plan", planPath, "--k3s-sudo"],
        io,
      ).status,
      "PASS",
    );
    assert.equal(patches, 1);
    assert.throws(() =>
      main(
        [
          "apply",
          "--context",
          "default",
          "--plan",
          planPath,
          "--evidence",
          `${root}/different.jsonl`,
          "--confirm",
          "APPLY-STAGING-FRONTEND-DEPENDENCIES",
          "--k3s-sudo",
        ],
        io,
      ),
    );
    const rollback = `${root}/rollback.json`;
    assert.equal(
      main(
        [
          "rollback-plan",
          "--context",
          "default",
          "--plan",
          planPath,
          "--output",
          rollback,
          "--k3s-sudo",
        ],
        io,
      ).status,
      "PLANNED",
    );
    unknown = false;
    assert.equal(
      main(
        [
          "apply",
          "--context",
          "default",
          "--plan",
          rollback,
          "--evidence",
          `${root}/rollback.jsonl`,
          "--confirm",
          "APPLY-STAGING-FRONTEND-DEPENDENCIES",
          "--k3s-sudo",
        ],
        io,
      ).status,
      "PASS",
    );
    assert.equal(
      frontendDependencyState(deployment).source.root,
      value.previousSource.path,
    );
    assert.equal(
      frontendDependencyState(deployment).cache.path,
      value.deployment.spec.template.spec.volumes[1].hostPath.path,
    );
    assert.equal(patches, 2);
    assert.ok(calls.every(([command]) => command === "sudo"));
    assert.match(readFileSync(evidence, "utf8"), /UNKNOWN/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("cache verification связывает manifests, readonly receipt и exact container runtime", () => {
  const root = mkdtempSync(join(tmpdir(), "frontend-cache-"));
  let readonlyDirectory;
  try {
    const sourceRoot = `${root}/source`,
      frontend = `${sourceRoot}/services/staff/control-center`,
      tools = `${sourceRoot}/tools/dev`;
    mkdirSync(`${frontend}/public/config`, { recursive: true });
    mkdirSync(`${frontend}/node_modules`, { recursive: true });
    mkdirSync(tools, { recursive: true });
    writeFileSync(
      `${sourceRoot}/.gitignore`,
      "node_modules/\npublic/config/\n",
    );
    for (const file of ["package.json", "package-lock.json"])
      writeFileSync(`${frontend}/${file}`, "{}\n");
    writeFileSync(`${frontend}/Dockerfile`, `FROM ${image} AS build\n`);
    writeFileSync(`${tools}/frontend-cache-identity.sh`, "fixture");
    const git = (args) =>
      execFileSync("git", ["-C", sourceRoot, ...args], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "pipe"],
      }).trim();
    git(["init"]);
    git(["remote", "add", "origin", "https://github.com/codex-k8s/kodex.git"]);
    git(["add", "."]);
    git([
      "-c",
      "user.name=fixture",
      "-c",
      "user.email=fixture@example.invalid",
      "commit",
      "-m",
      "Fixture",
    ]);
    const source = { path: sourceRoot, revision: git(["rev-parse", "HEAD"]) },
      identity = "3".repeat(64),
      cache = `${root}/cache/frontend-v1/${identity}/node_modules`;
    mkdirSync(cache, { recursive: true });
    writeFileSync(`${cache}/.kodex-cache-identity`, identity, { mode: 0o444 });
    for (const file of ["package.json", "package-lock.json"])
      writeFileSync(`${cache}/../${file}`, "{}\n", { mode: 0o444 });
    chmodSync(cache, 0o555);
    readonlyDirectory = cache;
    const calls = [],
      execute = (command, args) => {
        calls.push([command, args]);
        if (args[0] === "info") return '["name=seccomp,profile=builtin"]';
        return args.at(-1) === "/identity.sh" ? identity : "";
      };
    assert.equal(
      verifyFrontendCache(source, cache, image, execute).identity,
      identity,
    );
    assert.equal(calls.length, 3);
    assert.ok(
      calls
        .slice(1)
        .every(
          ([command, args]) =>
            command === "docker" &&
            args.includes("--pull=never") &&
            args[args.indexOf("--user") + 1] ===
              `${process.getuid()}:${process.getgid()}` &&
            args.includes("none") &&
            args.includes("--read-only"),
        ),
    );
    assert.throws(
      () =>
        verifyFrontendCache(source, cache, image, (_command, args) =>
          args[0] === "info" ? "[]" : "wrong",
        ),
      /FRONTEND_CACHE_RUNTIME_MISMATCH/,
    );
    for (const raw of ['["name=rootless"]', '["rootless"]']) {
      verifyFrontendCache(source, cache, image, (_command, args) => {
        if (args[0] === "info") return raw;
        assert.equal(args[args.indexOf("--user") + 1], "0:0");
        return args.at(-1) === "/identity.sh" ? identity : "";
      });
    }
    for (const raw of ["invalid", "null", "{}", "[false]"])
      assert.throws(
        () => verifyFrontendCache(source, cache, image, () => raw),
        /FRONTEND_DOCKER_SECURITY_INVALID/,
      );
    chmodSync(`${cache}/../package.json`, 0o644);
    writeFileSync(`${cache}/../package.json`, '{"different":true}');
    chmodSync(`${cache}/../package.json`, 0o444);
    assert.throws(
      () => verifyFrontendCache(source, cache, image, execute),
      /FRONTEND_CACHE_MANIFEST_MISMATCH/,
    );
  } finally {
    if (readonlyDirectory) chmodSync(readonlyDirectory, 0o755);
    rmSync(root, { recursive: true, force: true });
  }
});
