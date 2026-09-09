#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import {
  closeSync,
  fsyncSync,
  lstatSync,
  mkdtempSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync,
  writeSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";

export const storeNamespace = "kodex-system";
export const storeName = "proxy-session-store";
export const storeSecretName = "proxy-session-store-auth-v1";
export const proxyName = "oauth2-control-center";
export const proxyImage =
  "quay.io/oauth2-proxy/oauth2-proxy:v7.15.3@sha256:10a1165743a192e1940b4708fb9647027185ce11a681a1c5519b442ff7f1f561";
export const sessionStoreArgs = [
  "--session-store-type=redis",
  "--redis-connection-url=rediss://proxy-session-store.kodex-system.svc.cluster.local:6379/0",
  "--redis-username=proxy",
  "--redis-ca-path=/session-store-ca/ca.crt",
];
const resourcesPath = new URL(
  "../../deploy/k8s/base/proxy-session-store/resources.json",
  import.meta.url,
);
const assert = (value, code) => {
  if (!value) throw new Error(code);
};
export const resources = () =>
  JSON.parse(readFileSync(resourcesPath, "utf8")).items;
const sha = (s) => createHash("sha256").update(s).digest("hex");

export function sessionStoreSecret(
  random = () => randomBytes(32).toString("base64url"),
) {
  const proxy = random(),
    probe = random(),
    backup = random();
  for (const value of [proxy, probe, backup])
    assert(/^[A-Za-z0-9_-]{43}$/.test(value), "INVALID_GENERATED_PASSWORD");
  assert(
    new Set([proxy, probe, backup]).size === 3,
    "DISTINCT_CREDENTIALS_REQUIRED",
  );
  return {
    apiVersion: "v1",
    kind: "Secret",
    metadata: {
      name: storeSecretName,
      namespace: storeNamespace,
      labels: { "kodex.io/owner": "management-surfaces" },
    },
    type: "Opaque",
    immutable: true,
    stringData: {
      "proxy-password": proxy,
      "probe-password": probe,
      "backup-password": backup,
      "users.acl": [
        "user default off",
        `user proxy on #${sha(proxy)} ~_kodex_control_center_oauth2-* -@all +ping +hello +get +mget +set +mset +msetnx +getrange +del +exists +pttl +pexpire +eval +evalsha +script|load +client|setinfo +client|setname`,
        `user probe on #${sha(probe)} -@all +ping +info`,
        `user backup on #${sha(backup)} -@all +ping +save`,
        "",
      ].join("\n"),
    },
  };
}

export function sessionStoreProxySpec(deployment) {
  assert(
    deployment?.kind === "Deployment" &&
      deployment.metadata?.name === proxyName &&
      deployment.metadata.namespace === storeNamespace,
    "PROXY_IDENTITY_MISMATCH",
  );
  const result = structuredClone(deployment.spec),
    pod = result.template.spec;
  assert(
    pod.containers?.length === 1 &&
      pod.containers[0].image === proxyImage &&
      result.replicas === 2,
    "EXACT_PROXY_PROFILE_REQUIRED",
  );
  const container = pod.containers[0],
    args = container.args ?? [];
  for (const required of [
    "--cookie-expire=8h",
    "--cookie-refresh=1h",
    "--provider=keycloak-oidc",
    "--cookie-secure=true",
    "--cookie-httponly=true",
  ])
    assert(args.includes(required), "PROXY_AUTH_POLICY_MISMATCH");
  assert(
    !args.some((a) =>
      /^--(?:session-store-type|redis-|client-secret|cookie-secret)(?:=|$)/.test(
        a,
      ),
    ),
    "EXISTING_BACKEND_OR_INLINE_SECRET_REJECTED",
  );
  assert(
    !(container.env ?? []).some(
      (e) => /secret|password/i.test(e.name) && "value" in e,
    ),
    "INLINE_SECRET_REJECTED",
  );
  assert(
    !(container.env ?? []).some((e) =>
      e.name.startsWith("OAUTH2_PROXY_REDIS_"),
    ),
    "EXISTING_REDIS_ENV_REJECTED",
  );
  assert(
    !(pod.volumes ?? []).some((v) => v.name === "session-store-ca"),
    "EXISTING_CA_VOLUME_REJECTED",
  );
  container.args = [...args, ...sessionStoreArgs];
  container.env = [
    ...(container.env ?? []),
    {
      name: "OAUTH2_PROXY_REDIS_PASSWORD",
      valueFrom: {
        secretKeyRef: { name: storeSecretName, key: "proxy-password" },
      },
    },
  ];
  container.volumeMounts = [
    ...(container.volumeMounts ?? []),
    {
      name: "session-store-ca",
      mountPath: "/session-store-ca",
      readOnly: true,
    },
  ];
  pod.volumes = [
    ...(pod.volumes ?? []),
    {
      name: "session-store-ca",
      secret: {
        secretName: "proxy-session-store-tls",
        items: [{ key: "ca.crt", path: "ca.crt" }],
        defaultMode: 292,
      },
    },
  ];
  container.readinessProbe = {
    httpGet: { path: "/ready", port: 4180 },
    periodSeconds: 5,
    timeoutSeconds: 3,
    failureThreshold: 2,
  };
  container.livenessProbe = {
    httpGet: { path: "/ping", port: 4180 },
    periodSeconds: 15,
    timeoutSeconds: 3,
    failureThreshold: 3,
  };
  return result;
}

export function validateStoreDependencies(objects) {
  const subset = (expected, actual) => {
    if (Array.isArray(expected))
      return (
        Array.isArray(actual) &&
        expected.length === actual.length &&
        expected.every((v, i) => subset(v, actual[i]))
      );
    if (expected && typeof expected === "object")
      return (
        actual &&
        typeof actual === "object" &&
        Object.entries(expected).every(([k, v]) => subset(v, actual[k]))
      );
    return expected === actual;
  };
  return resources().map((desired) => {
    const actual = objects.find(
      (r) =>
        r.kind === desired.kind && r.metadata?.name === desired.metadata.name,
    );
    const expectedState = desired.spec ?? desired.data;
    let actualState = actual?.spec ?? actual?.data;
    if (desired.kind === "NetworkPolicy" && actualState) {
      actualState = structuredClone(actualState);
      for (const field of ["ingress", "egress"])
        if (
          Array.isArray(expectedState[field]) &&
          expectedState[field].length === 0 &&
          actualState[field] === undefined
        )
          actualState[field] = [];
    }
    assert(
      actual &&
        actual.metadata.namespace === storeNamespace &&
        subset(expectedState, actualState),
      "DEPENDENCY_PROFILE_DRIFT",
    );
    if (desired.kind === "NetworkPolicy")
      assert(
        fingerprint(actualState) === fingerprint(expectedState),
        "DEPENDENCY_NETWORK_POLICY_DRIFT",
      );
    if (desired.kind === "ConfigMap")
      assert(
        fingerprint(actual.data) === fingerprint(desired.data),
        "DEPENDENCY_CONFIG_DRIFT",
      );
    if (desired.kind === "Certificate")
      assert(
        actual.status?.conditions?.some(
          (c) => c.type === "Ready" && c.status === "True",
        ),
        "DEPENDENCY_CERTIFICATE_NOT_READY",
      );
    return {
      kind: actual.kind,
      name: actual.metadata.name,
      uid: actual.metadata.uid,
      stateSHA256: fingerprint(actualState),
    };
  });
}

function privateRead(path) {
  const s = lstatSync(path);
  assert(
    s.isFile() && s.nlink === 1 && !(s.mode & 0o077) && s.size < 4 << 20,
    "PRIVATE_PLAN_REQUIRED",
  );
  return JSON.parse(readFileSync(path, "utf8"));
}
function main(argv) {
  const command = argv.shift(),
    options = {};
  assert(
    ["plan-install", "install", "plan-cutover", "cutover", "backup"].includes(
      command,
    ),
    "INVALID_COMMAND",
  );
  while (argv.length) {
    const key = argv.shift();
    assert(
      ["--context", "--output", "--plan", "--evidence", "--confirm"].includes(
        key,
      ) &&
        !Object.hasOwn(options, key) &&
        argv.length,
      "INVALID_ARGUMENT",
    );
    options[key] = argv.shift();
  }
  const context = options["--context"];
  assert(context && !/prod/i.test(context), "STAGING_CONTEXT_REQUIRED");
  const kubectl = (args, input, binary = false) =>
    execFileSync(
      "kubectl",
      ["--context", context, "-n", storeNamespace, ...args],
      {
        encoding: binary ? undefined : "utf8",
        input,
        timeout: 30000,
        maxBuffer: 256 << 20,
        stdio: ["pipe", "pipe", "pipe"],
      },
    );
  const get = (kind, name, optional = false) => {
    const raw = kubectl([
      "get",
      kind,
      name,
      ...(optional ? ["--ignore-not-found"] : []),
      "-o",
      optional ? "jsonpath={.metadata}" : "json",
    ]);
    return raw.trim() ? JSON.parse(raw) : null;
  };
  const ns = get("namespace", storeNamespace),
    cluster = get("namespace", "kube-system");
  assert(
    ns.metadata.labels?.["kodex.dev/environment"] === "staging",
    "STAGING_NAMESPACE_REQUIRED",
  );
  const identity = {
    context,
    namespaceUID: ns.metadata.uid,
    clusterUID: cluster.metadata.uid,
  };
  if (command === "backup") {
    assert(
      options["--output"] &&
        options["--evidence"] &&
        options["--confirm"] === "BACKUP-STAGING-PROXY-SESSIONS" &&
        !options["--plan"],
      "BACKUP_CONFIRMATION_REQUIRED",
    );
    const pod = get("pod", `${storeName}-0`);
    assert(
      pod.status.conditions?.some(
        (c) => c.type === "Ready" && c.status === "True",
      ),
      "STORE_NOT_READY",
    );
    const journal = openSync(options["--evidence"], "wx", 0o600);
    const record = (event) => {
      writeSync(
        journal,
        JSON.stringify({
          ...identity,
          at: new Date().toISOString(),
          podUID: pod.metadata.uid,
          ...event,
        }) + "\n",
      );
      fsyncSync(journal);
    };
    let output,
      dispatched = false;
    try {
      output = openSync(options["--output"], "wx", 0o600);
      record({ status: "INTENT", kind: "backup" });
      dispatched = true;
      const data = kubectl(
        [
          "exec",
          `${storeName}-0`,
          "-c",
          "valkey",
          "--",
          "sh",
          "/config/ops.sh",
          "backup",
        ],
        undefined,
        true,
      );
      assert(
        data.subarray(0, 5).toString() === "REDIS" &&
          get("pod", `${storeName}-0`).metadata.uid === pod.metadata.uid,
        "BACKUP_READBACK_INVALID",
      );
      writeSync(output, data);
      fsyncSync(output);
      record({
        status: "PASS",
        bytes: data.length,
        sha256: sha(data),
        restoration: "REAUTHENTICATION_REQUIRED_NO_STALE_SESSION_RESTORE",
      });
    } catch {
      record({
        status: dispatched ? "UNKNOWN" : "FAIL",
        code: "BACKUP_OPERATION_FAILED",
      });
      throw new Error("BACKUP_OPERATION_FAILED");
    } finally {
      if (output !== undefined) closeSync(output);
      closeSync(journal);
    }
    return;
  }
  const install = command.endsWith("install");
  let operation;
  if (install) {
    const targets = [
      { kind: "Secret", metadata: { name: storeSecretName } },
      ...resources(),
    ];
    // Создание не является обновлением: частичный/неизвестный исход требует readback.
    for (const r of targets)
      assert(
        !get(r.kind, r.metadata.name, true),
        "RESOURCE_ALREADY_EXISTS_READBACK_REQUIRED",
      );
    operation = {
      kind: "install",
      resourcesSHA256: fingerprint(resources()),
      targets: targets.map((r) => ({ kind: r.kind, name: r.metadata.name })),
    };
  } else {
    const store = get("statefulset", storeName),
      dep = get("deployment", proxyName);
    const secret = {
      metadata: JSON.parse(
        kubectl([
          "get",
          "secret",
          storeSecretName,
          "-o",
          "jsonpath={.metadata}",
        ]),
      ),
      immutable:
        kubectl([
          "get",
          "secret",
          storeSecretName,
          "-o",
          "jsonpath={.immutable}",
        ]).trim() === "true",
    };
    assert(
      store.status.readyReplicas === 1 &&
        store.spec.replicas === 1 &&
        secret.immutable &&
        secret.metadata.labels?.["kodex.io/owner"] === "management-surfaces",
      "OWNED_STORE_READY_REQUIRED",
    );
    // Значения Secret используются только kubelet; в план попадает UID.
    const dependencies = validateStoreDependencies(
      resources().map((r) => get(r.kind, r.metadata.name)),
    );
    assert(
      store.status.observedGeneration === store.metadata.generation,
      "DEPENDENCY_OBSERVATION_STALE",
    );
    const after = sessionStoreProxySpec(dep);
    operation = {
      kind: "cutover",
      dependencies,
      uid: dep.metadata.uid,
      resourceVersion: dep.metadata.resourceVersion,
      secretUID: secret.metadata.uid,
      storeUID: store.metadata.uid,
      beforeSpecSHA256: fingerprint(dep.spec),
      afterSpecSHA256: fingerprint(after),
      patch: [
        { op: "test", path: "/metadata/uid", value: dep.metadata.uid },
        {
          op: "test",
          path: "/metadata/resourceVersion",
          value: dep.metadata.resourceVersion,
        },
        { op: "test", path: "/spec", value: dep.spec },
        { op: "replace", path: "/spec", value: after },
      ],
    };
  }
  const plan = { version: 1, ...identity, ...operation };
  if (command.startsWith("plan-")) {
    assert(
      options["--output"] &&
        !options["--confirm"] &&
        !options["--plan"] &&
        !options["--evidence"],
      "READ_ONLY_PLAN_REQUIRED",
    );
    writeFileSync(options["--output"], JSON.stringify(plan) + "\n", {
      flag: "wx",
      mode: 0o600,
    });
    process.stdout.write(
      JSON.stringify({
        status: "PLANNED",
        kind: operation.kind,
        planSHA256: fingerprint(plan),
      }) + "\n",
    );
    return;
  }
  assert(
    options["--plan"] &&
      options["--evidence"] &&
      !options["--output"] &&
      options["--confirm"] ===
        (install
          ? "INSTALL-STAGING-PROXY-SESSION-STORE"
          : "CUTOVER-STAGING-PROXY-SESSIONS-REAUTH") &&
      fingerprint(privateRead(options["--plan"])) === fingerprint(plan),
    "EXACT_PLAN_CONFIRMATION_REQUIRED",
  );
  const fd = openSync(options["--evidence"], "wx", 0o600);
  const record = (r) => {
    writeSync(
      fd,
      JSON.stringify({ at: new Date().toISOString(), ...identity, ...r }) +
        "\n",
    );
    fsyncSync(fd);
  };
  let dispatched = false;
  try {
    record({
      status: "STARTED",
      kind: operation.kind,
      planSHA256: fingerprint(plan),
    });
    if (install) {
      for (const r of [sessionStoreSecret(), ...resources()]) {
        record({ status: "INTENT", kind: r.kind, name: r.metadata.name });
        dispatched = true;
        kubectl(["create", "-f", "-"], JSON.stringify(r));
        const metadata = JSON.parse(
          kubectl([
            "get",
            r.kind,
            r.metadata.name,
            "-o",
            "jsonpath={.metadata}",
          ]),
        );
        record({
          status: "CREATED",
          kind: r.kind,
          name: r.metadata.name,
          uid: metadata.uid,
        });
      }
      record({ status: "INSTALLED", readiness: "NOT_RUN" });
    } else {
      record({ status: "INTENT", afterSpecSHA256: operation.afterSpecSHA256 });
      dispatched = true;
      // Host wrapper может закрыть /dev/stdin при повторном sudo. Собственный
      // private file не раскрывает полный Deployment spec через argv.
      const patchDirectory = mkdtempSync(join(tmpdir(), "kodex-proxy-patch-"));
      try {
        assert(
          !(lstatSync(patchDirectory).mode & 0o077),
          "PRIVATE_PATCH_DIRECTORY_REQUIRED",
        );
        const patchPath = join(patchDirectory, "patch.json");
        writeFileSync(patchPath, JSON.stringify(operation.patch), {
          flag: "wx",
          mode: 0o600,
        });
        assert(
          !(lstatSync(patchPath).mode & 0o077),
          "PRIVATE_PATCH_FILE_REQUIRED",
        );
        kubectl([
          "patch",
          "deployment",
          proxyName,
          "--type=json",
          `--patch-file=${patchPath}`,
        ]);
      } finally {
        rmSync(patchDirectory, { recursive: true, force: true });
      }
      const after = get("deployment", proxyName);
      assert(
        after.metadata.uid === operation.uid &&
          fingerprint(after.spec) === operation.afterSpecSHA256,
        "CUTOVER_READBACK_MISMATCH",
      );
      record({
        status: "APPLIED",
        afterSpecSHA256: operation.afterSpecSHA256,
        rollout: "NOT_RUN",
        reauthentication: "REQUIRED",
      });
    }
  } catch {
    record({
      status: dispatched ? "UNKNOWN" : "FAIL",
      code: "PROXY_SESSION_STORE_OPERATION_FAILED",
    });
    throw new Error("PROXY_SESSION_STORE_OPERATION_FAILED");
  } finally {
    closeSync(fd);
  }
}
if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    main(process.argv.slice(2));
  } catch {
    process.stderr.write(
      "Proxy session store operation failed; inspect authoritative state before retry\n",
    );
    process.exitCode = 1;
  }
}
