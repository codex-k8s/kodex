#!/usr/bin/env node
import { pathToFileURL } from "node:url";
import { bootstrapOwnerRuntimeSession } from "./owner-session-client.mjs";
import {
  createOwnerRuntimeHTTPS,
  readOwnerRuntimeCA,
} from "./owner-runtime-https.mjs";
import { createKubernetesFileOwnerClient } from "./managed-kubernetes-file-client.mjs";
import {
  validKubernetesFileInvocation,
  validKubernetesFileSource,
} from "../../services/staff/control-center/src/shared/lib/kubernetes-file-input.ts";

const commandFailure =
  "Kubernetes file input command failed; no preparation or input replay was performed";
// Файл загружается штатным artifact upload до нового turn/RR. Эта команда читает только owner-derived progress.
export function parseKubernetesFileArguments(argv) {
  const [command, ...pairs] = argv;
  if (
    ![
      "login",
      "list",
      "get",
      "cleanup-list",
      "cleanup-get",
      "cleanup-repair",
    ].includes(command) ||
    pairs.length % 2 !== 0
  )
    throw Error(commandFailure);
  const allowed = new Set([
    "--origin",
    "--session-file",
    "--ca-file",
    ...(command === "login" ? ["--token-file"] : ["--invocation-ref"]),
    ...(["get", "cleanup-get", "cleanup-repair"].includes(command)
      ? ["--source-ref"]
      : []),
    ...(["cleanup-get", "cleanup-repair"].includes(command)
      ? ["--idempotency-key"]
      : []),
    ...(command === "cleanup-repair" ? ["--version"] : []),
  ]);
  const result = { command };
  for (let index = 0; index < pairs.length; index += 2) {
    const key = pairs[index],
      value = pairs[index + 1];
    if (
      !allowed.has(key) ||
      Object.hasOwn(result, key.slice(2)) ||
      typeof value !== "string" ||
      !value ||
      value.length > 4096 ||
      /[\p{Cc}\p{Cs}]/u.test(value)
    )
      throw Error(commandFailure);
    result[key.slice(2)] = value;
  }
  for (const key of [
    "origin",
    "session-file",
    ...(command === "login" ? ["token-file"] : ["invocation-ref"]),
    ...(["get", "cleanup-get", "cleanup-repair"].includes(command)
      ? ["source-ref"]
      : []),
    ...(command === "cleanup-repair" ? ["version", "idempotency-key"] : []),
  ])
    if (!result[key]) throw Error(commandFailure);
  if (
    (command !== "login" &&
      !validKubernetesFileInvocation(result["invocation-ref"])) ||
    (["get", "cleanup-get", "cleanup-repair"].includes(command) &&
      !validKubernetesFileSource(result["source-ref"])) ||
    (result["idempotency-key"] !== undefined &&
      !/^[A-Za-z0-9_.:-]{8,128}$/u.test(result["idempotency-key"])) ||
    (command === "cleanup-repair" &&
      (!/^[1-9][0-9]*$/u.test(result.version) ||
        !Number.isSafeInteger(Number(result.version))))
  )
    throw Error(commandFailure);
  return result;
}
// Bootstrap и metadata используют тот же protected session и TLS transport; teardown всегда положительно join-ится.
export async function runKubernetesFileCLI(argv) {
  const args = parseKubernetesFileArguments(argv),
    ca = readOwnerRuntimeCA(args["ca-file"]),
    transport = createOwnerRuntimeHTTPS(ca ? { ca } : {}),
    controller = new AbortController(),
    stop = () => controller.abort();
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  try {
    const signal = AbortSignal.any([
      controller.signal,
      AbortSignal.timeout(30000),
    ]);
    if (args.command === "login") {
      await bootstrapOwnerRuntimeSession({
        origin: args.origin,
        tokenFile: args["token-file"],
        storagePath: args["session-file"],
        fetchAPI: transport.fetchAPI,
        signal,
      });
      process.stdout.write('{"sessionCreated":true}\n');
    } else {
      const client = createKubernetesFileOwnerClient({
        origin: args.origin,
        storagePath: args["session-file"],
        fetchAPI: transport.fetchAPI,
      });
      const value =
        args.command === "cleanup-list"
          ? {
              projection: "CLEANUP_ONLY",
              items: await client.listCleanup(args["invocation-ref"], signal),
            }
          : args.command === "cleanup-get"
            ? await client.getCleanup(
                args["invocation-ref"],
                args["source-ref"],
                signal,
                args["idempotency-key"] ?? "",
              )
            : args.command === "cleanup-repair"
              ? await client.repairCleanup(
                  args["invocation-ref"],
                  args["source-ref"],
                  Number(args.version),
                  args["idempotency-key"],
                  signal,
                )
              : args.command === "list"
                ? { items: await client.list(args["invocation-ref"], signal) }
                : await client.get(
                    args["invocation-ref"],
                    args["source-ref"],
                    signal,
                  );
      process.stdout.write(JSON.stringify(value) + "\n");
    }
    return 0;
  } finally {
    process.off("SIGINT", stop);
    process.off("SIGTERM", stop);
    try {
      await transport.close(AbortSignal.timeout(5000));
    } finally {
      ca?.fill(0);
    }
  }
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  try {
    process.exitCode = await runKubernetesFileCLI(process.argv.slice(2));
  } catch {
    process.stderr.write(commandFailure + "\n");
    process.exitCode = 2;
  }
}
