#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  resources,
  sessionStoreSecret,
  storeName,
  storeNamespace,
  storeSecretName,
  validateStoreDependencies,
} from "../release/proxy-session-store.mjs";

const assert = (value, code) => {
  if (!value) throw new Error(code);
};

export function classifyPresence(present, total) {
  if (present === 0) return "absent";
  if (present === total) return "complete";
  return "partial";
}

function main(argv) {
  const options = {};
  while (argv.length) {
    const key = argv.shift();
    assert(
      ["--context", "--mode"].includes(key) &&
        !Object.hasOwn(options, key) &&
        argv.length,
      "INVALID_ARGUMENT",
    );
    options[key] = argv.shift();
  }
  const context = options["--context"];
  const mode = options["--mode"];
  assert(
    /^k3d-[a-z0-9-]+$/.test(context) && !/prod/i.test(context),
    "LOCAL_CONTEXT_REQUIRED",
  );
  assert(["apply", "readback"].includes(mode), "INVALID_MODE");

  const kubectl = (args, input) =>
    execFileSync(
      "kubectl",
      ["--context", context, "-n", storeNamespace, ...args],
      {
        encoding: "utf8",
        input,
        timeout: 300_000,
        maxBuffer: 64 << 20,
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
      "json",
    ]);
    return raw.trim() ? JSON.parse(raw) : null;
  };

  assert(
    execFileSync("kubectl", ["config", "current-context"], {
      encoding: "utf8",
    }).trim() === context,
    "CURRENT_CONTEXT_MISMATCH",
  );
  const namespace = get("namespace", storeNamespace);
  assert(
    namespace.metadata?.labels?.["kodex.dev/environment"] === "staging" &&
      namespace.metadata?.labels?.["kodex.dev/local-profile"] === "hot-reload",
    "LOCAL_NAMESPACE_REQUIRED",
  );

  const desired = [
    sessionStoreSecret,
    ...resources().map((resource) => () => resource),
  ];
  const descriptors = [
    { kind: "Secret", name: storeSecretName },
    ...resources().map((resource) => ({
      kind: resource.kind,
      name: resource.metadata.name,
    })),
  ];
  const existing = descriptors.map(({ kind, name }) => get(kind, name, true));
  const presence = classifyPresence(
    existing.filter(Boolean).length,
    descriptors.length,
  );
  assert(presence !== "partial", "PARTIAL_INSTALL_REQUIRES_EXPLICIT_RECOVERY");
  if (presence === "absent") {
    assert(mode === "apply", "DEPENDENCY_ABSENT");
    for (const create of desired)
      kubectl(["create", "-f", "-"], JSON.stringify(create()));
  }

  for (const certificate of ["proxy-session-ca", "proxy-session-store-tls"])
    kubectl([
      "wait",
      "--for=condition=Ready",
      `certificate/${certificate}`,
      "--timeout=5m",
    ]);
  // OnDelete преднамеренно запрещает неявный rollout этого stateful workload,
  // поэтому readiness проверяется по авторитетному счётчику StatefulSet.
  kubectl([
    "wait",
    "--for=jsonpath={.status.readyReplicas}=1",
    `statefulset/${storeName}`,
    "--timeout=5m",
  ]);

  const secret = get("secret", storeSecretName);
  assert(
    secret.immutable === true &&
      secret.metadata?.labels?.["kodex.io/owner"] === "management-surfaces" &&
      JSON.stringify(Object.keys(secret.data ?? {}).sort()) ===
        JSON.stringify([
          "backup-password",
          "probe-password",
          "proxy-password",
          "users.acl",
        ]),
    "SECRET_PROFILE_DRIFT",
  );
  validateStoreDependencies(
    resources().map((resource) => get(resource.kind, resource.metadata.name)),
  );
  const workload = get("statefulset", storeName);
  assert(
    workload.spec?.replicas === 1 &&
      workload.status?.readyReplicas === 1 &&
      workload.status?.observedGeneration === workload.metadata?.generation,
    "STORE_NOT_READY",
  );
  process.stdout.write(
    `Local proxy session store reconciliation completed: ${mode}\n`,
  );
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    const code = error instanceof Error ? error.message : "UNKNOWN_FAILURE";
    process.stderr.write(
      `Local proxy session store reconciliation failed: ${code}\n`,
    );
    process.exitCode = 1;
  }
}
