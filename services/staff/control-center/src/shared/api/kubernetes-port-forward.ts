import {
  listManagedKubernetesPortForwardConnections,
  getManagedKubernetesPortForwardConnection,
  openManagedKubernetesPortForwardConnection,
  closeManagedKubernetesPortForwardConnection,
} from "./generated/openapi/sdk.gen";
import type {
  KubernetesPortForwardConnectionView as Connection,
  KubernetesPortForwardConnectionPage as Page,
  KubernetesPortForwardConnectionReadback as Readback,
  KubernetesPortForwardConnectionClosed as MutationResult,
  KubernetesStreamInputView as Input,
} from "./generated/openapi/types.gen";
import { hasExactKeys, isRecord } from "@/shared/lib/exact-object";
import {
  validPortForwardCoordinates,
  encodePortForwardInput,
} from "@/shared/lib/kubernetes-port-forward-frame";
import { requestSignal } from "./client";
import { etag, mutate } from "./mutation";
import { unwrap } from "./problem";
import {
  parseKubernetesInputView,
  parseKubernetesInputSubmission,
  sameKubernetesInputStream,
  submitKubernetesStreamInput,
  KubernetesInputProtocolError,
  validKubernetesStreamReason,
} from "./kubernetes-stream-input";
const live = new Set(["PREPARED", "OPENING", "OPEN"]);
const states = new Set([...live, "CLOSED", "UNKNOWN", "CANCELLED", "EXPIRED"]);
const reasons = new Set([
  "",
  "KUBERNETES_RECONCILE_REQUIRED",
  "KUBERNETES_PORT_FORWARD_CAPACITY_EXHAUSTED",
  "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED",
]);
function exact(
  value: unknown,
  required: string[],
  optional: string[] = [],
): value is Record<string, unknown> {
  return (
    isRecord(value) &&
    hasExactKeys(value, [
      ...required,
      ...optional.filter((key) => Object.hasOwn(value, key)),
    ])
  );
}
function integer(
  value: unknown,
  min = 1,
  max = Number.MAX_SAFE_INTEGER,
): value is number {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= min &&
    value <= max
  );
}
function fail(): never {
  throw new KubernetesInputProtocolError();
}
function parent(selected: Input) {
  if (selected.operation !== "kubernetes.port-forward") fail();
  return {
    invocationRef: selected.invocationRef,
    streamRef: selected.streamRef,
  };
}
export function parsePortForwardConnection(
  value: unknown,
  selected: Input,
): Connection {
  if (
    !exact(value, [
      "connectionRef",
      "invocationRef",
      "streamRef",
      "streamGeneration",
      "generation",
      "slot",
      "state",
      "inputVersion",
      "localWriteClosed",
      "readEndObserved",
      "nativeJoined",
      "hardDeadline",
      "reasonCode",
    ]) ||
    !validPortForwardCoordinates(value as unknown as Connection) ||
    value.invocationRef !== selected.invocationRef ||
    value.streamRef !== selected.streamRef ||
    value.streamGeneration !== selected.generation ||
    !integer(value.inputVersion, selected.version) ||
    typeof value.state !== "string" ||
    !states.has(value.state) ||
    typeof value.localWriteClosed !== "boolean" ||
    typeof value.readEndObserved !== "boolean" ||
    typeof value.nativeJoined !== "boolean" ||
    (value.nativeJoined && live.has(value.state)) ||
    value.hardDeadline !== selected.hardDeadline ||
    !validKubernetesStreamReason(value.reasonCode)
  )
    fail();
  return value as unknown as Connection;
}
function readback(value: unknown, selected: Input): Readback {
  if (!exact(value, ["connection"], ["submission"])) fail();
  const connection = parsePortForwardConnection(value.connection, selected);
  const result: Readback = { connection };
  if (Object.hasOwn(value, "submission")) {
    result.submission = parseKubernetesInputSubmission(
      value.submission,
      selected,
    );
    if (
      result.submission.channel !== "PORT_FORWARD" ||
      result.submission.endOfInput ||
      result.submission.byteCount < 51
    )
      fail();
  }
  return result;
}
function mutation(value: unknown, selected: Input): MutationResult {
  if (!exact(value, ["connection", "input"], ["submission"])) fail();
  const input = parseKubernetesInputView(value.input, selected);
  if (
    !sameKubernetesInputStream(input, selected) ||
    input.version < selected.version
  )
    fail();
  const result = readback(
    {
      connection: value.connection,
      ...(Object.hasOwn(value, "submission")
        ? { submission: value.submission }
        : {}),
    },
    selected,
  );
  if (result.connection.inputVersion !== input.version) fail();
  return { ...result, input };
}
export function parsePortForwardPage(
  value: unknown,
  selected: Input,
  pageSize = 16,
): Page {
  parent(selected);
  if (
    !exact(value, ["items", "nextPageToken", "profile"]) ||
    !Array.isArray(value.items) ||
    value.items.length > pageSize ||
    typeof value.nextPageToken !== "string" ||
    !exact(value.profile, [
      "protocol",
      "maximumActiveConnections",
      "activeConnections",
      "admissionReasonCode",
      "ports",
      "timeoutSeconds",
    ])
  )
    fail();
  const p = value.profile;
  if (
    !["", "PORT_FORWARD_SPDY_V1", "PORT_FORWARD_WEBSOCKET_V4"].includes(
      p.protocol as string,
    ) ||
    p.maximumActiveConnections !== 16 ||
    !integer(p.activeConnections, 0, 16) ||
    typeof p.admissionReasonCode !== "string" ||
    !reasons.has(p.admissionReasonCode) ||
    (p.protocol === "PORT_FORWARD_WEBSOCKET_V4" &&
      p.admissionReasonCode !==
        "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED") ||
    (p.protocol === "" &&
      p.admissionReasonCode !== "KUBERNETES_RECONCILE_REQUIRED") ||
    (p.protocol === "PORT_FORWARD_SPDY_V1" &&
      p.admissionReasonCode ===
        "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED") ||
    !integer(p.timeoutSeconds, 1, 300) ||
    !Array.isArray(p.ports) ||
    !p.ports.length ||
    p.ports.length > 16
  )
    fail();
  const local = new Set<number>();
  for (const port of p.ports) {
    if (
      !exact(port, ["port", "targetPort"]) ||
      !integer(port.port, 1, 65535) ||
      !integer(port.targetPort, 1, 65535) ||
      local.has(port.port)
    )
      fail();
    local.add(port.port);
  }
  const items = value.items.map((item) =>
    parsePortForwardConnection(item, selected),
  );
  const seen = new Set<string>();
  let previous = 0;
  for (const item of items) {
    if (
      item.slot >= p.ports.length ||
      item.generation <= previous ||
      seen.has(item.connectionRef)
    )
      fail();
    seen.add(item.connectionRef);
    previous = item.generation;
  }

  if (
    value.nextPageToken !== "" &&
    (items.length !== pageSize ||
      value.nextPageToken !== items.at(-1)?.connectionRef)
  )
    fail();
  return {
    items,
    nextPageToken: value.nextPageToken,
    profile: p as unknown as Page["profile"],
  };
}
export async function listPortForwardConnections(
  selected: Input,
  signal: AbortSignal,
  pageToken = "",
): Promise<Page> {
  if (pageToken && !/^kpf_[A-Za-z0-9_-]{24}$/u.test(pageToken)) fail();
  return parsePortForwardPage(
    (
      await unwrap(
        listManagedKubernetesPortForwardConnections({
          path: parent(selected),
          query: { pageSize: 16, ...(pageToken ? { pageToken } : {}) },
          signal: requestSignal(signal),
        }),
      )
    ).data,
    selected,
  );
}
export async function getPortForwardConnection(
  selected: Input,
  signal: AbortSignal,
  selector: { connectionRef: string } | { openIdempotencyKey: string },
): Promise<Readback> {
  if (
    "connectionRef" in selector
      ? !/^kpf_[A-Za-z0-9_-]{24}$/u.test(selector.connectionRef)
      : !/^[A-Za-z0-9._:-]{8,128}$/u.test(selector.openIdempotencyKey)
  )
    fail();
  const result = readback(
    (
      await unwrap(
        getManagedKubernetesPortForwardConnection({
          path: parent(selected),
          query: selector,
          signal: requestSignal(signal),
        }),
      )
    ).data,
    selected,
  );
  if (
    "connectionRef" in selector
      ? result.connection.connectionRef !== selector.connectionRef
      : !result.submission || result.submission.byteCount !== 51
  )
    fail();
  return result;
}
export async function openPortForwardConnection(
  selected: Input,
  slot: number,
  signal: AbortSignal,
  key: string,
): Promise<MutationResult> {
  if (!integer(slot, 0, 15)) fail();
  const result = mutation(
    (
      await mutate(
        (headers) =>
          openManagedKubernetesPortForwardConnection({
            path: parent(selected),
            body: { slot },
            headers: { ...headers, "If-Match": etag(selected.version) },
            signal: requestSignal(signal),
          }),
        selected.version,
        key,
      )
    ).data,
    selected,
  );
  if (
    result.connection.slot !== slot ||
    !result.submission ||
    result.submission.byteCount !== 51
  )
    fail();
  return result;
}
export async function closePortForwardConnection(
  selected: Input,
  connection: Connection,
  signal: AbortSignal,
  key: string,
): Promise<MutationResult> {
  parsePortForwardConnection(connection, selected);
  const result = mutation(
    (
      await mutate(
        (headers) =>
          closeManagedKubernetesPortForwardConnection({
            path: {
              ...parent(selected),
              connectionRef: connection.connectionRef,
            },
            body: {},
            headers: { ...headers, "If-Match": etag(selected.version) },
            signal: requestSignal(signal),
          }),
        selected.version,
        key,
      )
    ).data,
    selected,
  );
  if (
    result.connection.connectionRef !== connection.connectionRef ||
    live.has(result.connection.state) ||
    (result.submission && result.submission.byteCount !== 51)
  )
    fail();
  return result;
}
// Оригинальный payload очищается также при отказе; OPEN/CLOSE нельзя передать через этот DATA producer.
export async function sendPortForwardInput(
  selected: Input,
  connection: Connection,
  bytes: Uint8Array,
  halfClose: boolean,
  signal: AbortSignal,
  key: string,
) {
  try {
    parsePortForwardConnection(connection, selected);
    if (connection.state !== "OPEN" || connection.localWriteClosed) fail();
    return await submitKubernetesStreamInput(
      selected,
      "PORT_FORWARD",
      encodePortForwardInput(connection, bytes, halfClose),
      false,
      signal,
      key,
    );
  } finally {
    bytes.fill(0);
  }
}
