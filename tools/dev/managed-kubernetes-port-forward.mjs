#!/usr/bin/env node
import { pathToFileURL } from "node:url";
import { bootstrapOwnerRuntimeSession } from "./owner-session-client.mjs";
import {
  createOwnerRuntimeHTTPS,
  readOwnerRuntimeCA,
} from "./owner-runtime-https.mjs";
import { createPortForwardOwnerClient } from "./managed-kubernetes-port-forward-client.mjs";
import { createPortForwardBridge } from "./managed-kubernetes-port-forward-bridge.mjs";

const commandFailure =
  "Kubernetes port-forward command failed; no automatic mutation replay was performed";
// CLI получает только owner session/private token file и safe locators. Local mapping не изменяет approved targetPort.
export function parsePortForwardArguments(argv) {
  const [command, ...args] = argv;
  if (
    !["login", "list", "profile", "forward"].includes(command) ||
    args.length % 2 !== 0
  )
    throw Error(commandFailure);
  const selected = { command, mappings: [] };
  const allowed = new Set([
    "--origin",
    "--session-file",
    "--ca-file",
    ...(command === "login" ? ["--token-file"] : ["--run-ref", "--node-ref"]),
    ...(command === "list" ? ["--page-token"] : []),
    ...(["forward", "profile"].includes(command)
      ? ["--invocation-ref", "--stream-ref"]
      : []),
    ...(command === "forward" ? ["--map"] : []),
  ]);
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index],
      value = args[index + 1];
    if (
      !allowed.has(key) ||
      typeof value !== "string" ||
      !value ||
      value.length > 4096 ||
      /[\p{Cc}\p{Cs}]/u.test(value)
    )
      throw Error(commandFailure);
    if (key === "--map") {
      if (!/^(?:[0-9]|1[0-5]):(?:0|[1-9][0-9]{0,4})$/u.test(value))
        throw Error(commandFailure);
      const [slot, localPort] = value.split(":").map(Number);
      if (
        localPort > 65535 ||
        selected.mappings.length >= 16 ||
        selected.mappings.some((m) => m.slot === slot)
      )
        throw Error(commandFailure);
      selected.mappings.push({ slot, localPort });
    } else {
      const name = key.slice(2);
      if (Object.hasOwn(selected, name)) throw Error(commandFailure);
      selected[name] = value;
    }
  }
  for (const name of [
    "origin",
    "session-file",
    ...(command === "login" ? ["token-file"] : ["run-ref", "node-ref"]),
    ...(["forward", "profile"].includes(command)
      ? ["invocation-ref", "stream-ref"]
      : []),
  ])
    if (!selected[name]) throw Error(commandFailure);
  if (command === "forward" && !selected.mappings.length)
    throw Error(commandFailure);
  return selected;
}
// Direct entrypoint использует existing session authorization и реальный loopback listener; no kubeconfig, raw TCP в browser либо callback-only success.
export async function runPortForwardCLI(argv) {
  const args = parsePortForwardArguments(argv),
    ca = readOwnerRuntimeCA(args["ca-file"]),
    transport = createOwnerRuntimeHTTPS(ca ? { ca } : {});
  let bridge,
    joined = false;
  const scope = new AbortController(),
    stop = () => scope.abort();
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  try {
    if (args.command === "login") {
      await bootstrapOwnerRuntimeSession({
        origin: args.origin,
        tokenFile: args["token-file"],
        storagePath: args["session-file"],
        fetchAPI: transport.fetchAPI,
        signal: AbortSignal.any([scope.signal, AbortSignal.timeout(30000)]),
      });
      process.stdout.write('{"sessionCreated":true}\n');
      return 0;
    }
    const client = createPortForwardOwnerClient({
        origin: args.origin,
        storagePath: args["session-file"],
        fetchAPI: transport.fetchAPI,
      }),
      context = { runRef: args["run-ref"], nodeRef: args["node-ref"] };
    if (args.command === "list") {
      const result = await client.listInputs(
        context,
        AbortSignal.any([scope.signal, AbortSignal.timeout(30000)]),
        args["page-token"],
      );
      process.stdout.write(JSON.stringify(result) + "\n");
      return 0;
    }
    context.invocationRef = args["invocation-ref"];
    context.streamRef = args["stream-ref"];
    const input = await client.getInput(
      context,
      AbortSignal.any([scope.signal, AbortSignal.timeout(30000)]),
    );
    if (args.command === "profile") {
      const { profile } = await client.listConnections(
        input,
        AbortSignal.any([scope.signal, AbortSignal.timeout(30000)]),
      );
      process.stdout.write(
        JSON.stringify({
          invocationRef: input.invocationRef,
          streamRef: input.streamRef,
          resource: input.resource,
          targetRef: input.targetRef,
          targetRevision: input.targetRevision,
          connectionRef: input.connectionRef,
          hardDeadline: input.hardDeadline,
          profile,
        }) + "\n",
      );
      return 0;
    }
    bridge = await createPortForwardBridge({
      client,
      input,
      mappings: args.mappings,
      signal: scope.signal,
    });
    process.stdout.write(
      JSON.stringify({
        listening: bridge.addresses,
        invocationRef: input.invocationRef,
        streamRef: input.streamRef,
        generation: input.generation,
      }) + "\n",
    );
    const result = await bridge.joined();
    joined = true;
    process.stdout.write(
      JSON.stringify({
        deliveryComplete: result.deliveryComplete,
        nativeConnectionsJoined: result.nativeConnectionsJoined,
        afterSequence: result.afterSequence,
      }) + "\n",
    );
    return result.deliveryComplete && result.nativeConnectionsJoined ? 0 : 2;
  } finally {
    process.off("SIGINT", stop);
    process.off("SIGTERM", stop);
    if (bridge && !joined) await bridge.stop();
    await transport.close(AbortSignal.timeout(5000));
    ca?.fill(0);
  }
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  try {
    process.exitCode = await runPortForwardCLI(process.argv.slice(2));
  } catch {
    process.stderr.write(commandFailure + "\n");
    process.exitCode = 2;
  }
}
