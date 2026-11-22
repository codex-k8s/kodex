import { createHash } from "node:crypto";
import { performance } from "node:perf_hooks";
import { createOwnerRuntimeSessionClient } from "./owner-session-client.mjs";
import { boundedResponseBody } from "./runtime-workspace-acceptance.mjs";
import { validPortForwardInput } from "../../services/staff/control-center/src/shared/lib/kubernetes-port-forward-frame.ts";

const inputStates = new Set([
  "PREPARED",
  "ACTIVE",
  "COMPLETED",
  "CANCELED",
  "REVOKED",
  "EXPIRED",
  "LOST",
]);
const submissionStates = new Set([
  "QUEUED",
  "CLAIMED",
  "ACKNOWLEDGED",
  "UNKNOWN",
  "REJECTED",
  "CANCELLED",
]);
const connectionStates = new Set([
  "PREPARED",
  "OPENING",
  "OPEN",
  "CLOSED",
  "UNKNOWN",
  "CANCELLED",
  "EXPIRED",
]);
const liveConnections = new Set(["PREPARED", "OPENING", "OPEN"]);
const reasons = new Set([
  "",
  "KUBERNETES_STREAM_CANCELED",
  "KUBERNETES_GRANT_REVOKED",
  "KUBERNETES_LEASE_EXPIRED",
  "KUBERNETES_SIZE_LIMIT",
  "KUBERNETES_STREAM_LOST",
  "KUBERNETES_REMOTE_FAILURE",
  "KUBERNETES_ACCESS_DENIED",
  "KUBERNETES_IDENTITY_CHANGED",
  "KUBERNETES_SCOPE_DENIED",
  "KUBERNETES_CONFLICT",
  "KUBERNETES_NOT_FOUND",
  "KUBERNETES_TRANSPORT_UNKNOWN",
  "KUBERNETES_TARGET_UNAVAILABLE",
  "KUBERNETES_RESPONSE_INVALID",
  "KUBERNETES_RESOURCE_UNAVAILABLE",
  "KUBERNETES_PROTECTED_RESOURCE",
  "KUBERNETES_TARGET_STORAGE_POLICY_REQUIRED",
  "KUBERNETES_CONTROLLER_POLICY_REQUIRED",
  "KUBERNETES_CONTROLLER_RUNTIME_UNAVAILABLE",
  "KUBERNETES_TIMEOUT",
  "KUBERNETES_PIN_UNAVAILABLE",
  "KUBERNETES_PROTOCOL_UNSUPPORTED",
  "KUBERNETES_RECONCILE_REQUIRED",
  "KUBERNETES_COMMAND_UNSUPPORTED",
  "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED",
  "KUBERNETES_PORT_FORWARD_CAPACITY_EXHAUSTED",
]);
const knownProblems = new Set([
  "INVALID_REQUEST",
  "INVALID_ARGUMENT",
  "NOT_FOUND",
  "FORBIDDEN",
  "UNAUTHORIZED",
  "VERSION_MISMATCH",
  "CONFLICT",
  "IDEMPOTENCY_REUSE",
  "UNAVAILABLE",
  "RATE_LIMITED",
]);
const resourceKeys = [
  "group",
  "version",
  "resource",
  "subresource",
  "namespace",
  "name",
  "uid",
  "resourceVersion",
  "namespaced",
  "allNamespaces",
];
const immutableInput = [
  "invocationRef",
  "streamRef",
  "runRef",
  "nodeRef",
  "generation",
  "operation",
  "namespace",
  "podName",
  "container",
  "maximumFrameBytes",
  "maximumTotalBytes",
  "hardDeadline",
  "targetRef",
  "targetRevision",
  "connectionRef",
];
const inputKeys = [
  ...immutableInput,
  "state",
  "version",
  "inputChannels",
  "closedInputChannels",
  "acknowledgedSequence",
  "reasonCode",
  "resource",
  "maximumStagedBytes",
  "maximumStagedObjects",
];
const protocolMessage = "Kubernetes port-forward owner protocol is invalid";

export class PortForwardClientError extends Error {
  constructor(code = "PROTOCOL_INVALID", status = 0) {
    super(
      code === "PROTOCOL_INVALID"
        ? protocolMessage
        : "Kubernetes port-forward owner request failed",
    );
    this.name = "PortForwardClientError";
    this.code = code;
    this.status = status;
  }
}
function invalid() {
  throw new PortForwardClientError();
}
function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function exact(value, required, optional = []) {
  return (
    record(value) &&
    required.every((key) => Object.hasOwn(value, key)) &&
    Object.keys(value).every(
      (key) => required.includes(key) || optional.includes(key),
    )
  );
}
function integer(value, min = 1, max = Number.MAX_SAFE_INTEGER) {
  return Number.isSafeInteger(value) && value >= min && value <= max;
}
function ref(value, prefix) {
  return (
    typeof value === "string" &&
    value.startsWith(prefix + "_") &&
    /^[A-Za-z0-9_-]{12,96}$/u.test(value)
  );
}
function connectionRef(value) {
  return typeof value === "string" && /^kpf_[A-Za-z0-9_-]{24}$/u.test(value);
}
function key(value) {
  return typeof value === "string" && /^[A-Za-z0-9._:-]{8,128}$/u.test(value);
}
function digest(value) {
  return typeof value === "string" && /^[a-f0-9]{64}$/u.test(value);
}
function label(value) {
  return (
    typeof value === "string" &&
    /^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$/u.test(value)
  );
}
function dns(value) {
  return (
    typeof value === "string" &&
    value.length <= 253 &&
    value.split(".").every(label)
  );
}
function timestamp(value) {
  if (
    typeof value !== "string" ||
    value.length > 64 ||
    !/^\d{4}-\d{2}-\d{2}T.*Z$/u.test(value)
  )
    invalid();
  const time = Date.parse(value);
  if (!Number.isFinite(time)) invalid();
  return time;
}
function sameInput(a, b) {
  return (
    immutableInput.every((field) => a[field] === b[field]) &&
    resourceKeys.every((field) => a.resource[field] === b.resource[field]) &&
    a.inputChannels.length === b.inputChannels.length &&
    a.inputChannels.every((item, index) => item === b.inputChannels[index])
  );
}
function channels(value) {
  return (
    Array.isArray(value) &&
    value.length <= 1 &&
    value.every((channel) => channel === "PORT_FORWARD") &&
    new Set(value).size === value.length
  );
}

// Только metadata исходного PF вызова; opaque refs не заменяют owner eligibility.
export function parsePortForwardInputView(value, context, previous) {
  if (
    !exact(value, inputKeys, ["pending"]) ||
    !ref(value.invocationRef, "inv") ||
    !ref(value.streamRef, "kst") ||
    value.runRef !== context.runRef ||
    value.nodeRef !== context.nodeRef ||
    !ref(value.runRef, "run") ||
    !ref(value.nodeRef, "nod") ||
    value.operation !== "kubernetes.port-forward" ||
    !integer(value.generation) ||
    !integer(value.version) ||
    !label(value.namespace) ||
    !dns(value.podName) ||
    !label(value.container) ||
    !ref(value.connectionRef, "int") ||
    typeof value.targetRef !== "string" ||
    value.targetRef === "" ||
    Buffer.byteLength(value.targetRef) > 96 ||
    /[\s\p{Cc}\p{Cs}]/u.test(value.targetRef) ||
    !integer(value.targetRevision) ||
    !inputStates.has(value.state) ||
    !integer(value.maximumFrameBytes, 51, 32768) ||
    !integer(value.maximumTotalBytes, value.maximumFrameBytes, 268435456) ||
    !integer(value.maximumStagedBytes, 1, 1073741824) ||
    !integer(value.maximumStagedObjects, 1, 32768) ||
    !integer(value.acknowledgedSequence, 0) ||
    !channels(value.inputChannels) ||
    !channels(value.closedInputChannels) ||
    !value.closedInputChannels.every((channel) =>
      value.inputChannels.includes(channel),
    ) ||
    !reasons.has(value.reasonCode)
  )
    invalid();
  timestamp(value.hardDeadline);
  const r = value.resource;
  if (
    !exact(r, resourceKeys) ||
    r.group !== "" ||
    r.version !== "v1" ||
    r.resource !== "pods" ||
    r.subresource !== "" ||
    r.namespace !== value.namespace ||
    r.name !== value.podName ||
    r.namespaced !== true ||
    r.allNamespaces !== false ||
    typeof r.uid !== "string" ||
    !/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/u.test(
      r.uid,
    ) ||
    r.uid === "00000000-0000-0000-0000-000000000000" ||
    typeof r.resourceVersion !== "string" ||
    r.resourceVersion === "" ||
    Buffer.byteLength(r.resourceVersion) > 256 ||
    /[\s\p{Cc}\p{Cs}]/u.test(r.resourceVersion)
  )
    invalid();
  if (
    previous &&
    (!sameInput(value, previous) ||
      value.version < previous.version ||
      value.acknowledgedSequence < previous.acknowledgedSequence)
  )
    invalid();
  if (Object.hasOwn(value, "pending"))
    parsePortForwardSubmission(value.pending, value);
  return value;
}
export function parsePortForwardSubmission(value, input) {
  if (
    !exact(value, [
      "ref",
      "invocationRef",
      "streamRef",
      "generation",
      "sequence",
      "channel",
      "payloadDigest",
      "byteCount",
      "endOfInput",
      "state",
    ]) ||
    !ref(value.ref, "kif") ||
    value.invocationRef !== input.invocationRef ||
    value.streamRef !== input.streamRef ||
    value.generation !== input.generation ||
    !integer(value.sequence) ||
    value.channel !== "PORT_FORWARD" ||
    !digest(value.payloadDigest) ||
    !integer(value.byteCount, 51, input.maximumFrameBytes) ||
    value.endOfInput !== false ||
    !submissionStates.has(value.state)
  )
    invalid();
  return value;
}
function readback(value, selected) {
  if (!exact(value, ["view"], ["submission"])) invalid();
  const view = parsePortForwardInputView(value.view, selected, selected);
  return {
    view,
    ...(Object.hasOwn(value, "submission")
      ? { submission: parsePortForwardSubmission(value.submission, view) }
      : {}),
  };
}
export function parsePortForwardConnection(value, input) {
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
    !connectionRef(value.connectionRef) ||
    value.invocationRef !== input.invocationRef ||
    value.streamRef !== input.streamRef ||
    value.streamGeneration !== input.generation ||
    !integer(value.generation) ||
    !integer(value.slot, 0, 15) ||
    !connectionStates.has(value.state) ||
    !integer(value.inputVersion, input.version) ||
    typeof value.localWriteClosed !== "boolean" ||
    typeof value.readEndObserved !== "boolean" ||
    typeof value.nativeJoined !== "boolean" ||
    (value.nativeJoined && liveConnections.has(value.state)) ||
    value.hardDeadline !== input.hardDeadline ||
    !reasons.has(value.reasonCode)
  )
    invalid();
  return value;
}
// Профиль получает только owner-derived mapping; client не принимает remote URL/port override.
export function parsePortForwardProfile(value) {
  if (
    !exact(value, [
      "protocol",
      "maximumActiveConnections",
      "activeConnections",
      "admissionReasonCode",
      "ports",
      "timeoutSeconds",
    ]) ||
    !["", "PORT_FORWARD_SPDY_V1", "PORT_FORWARD_WEBSOCKET_V4"].includes(
      value.protocol,
    ) ||
    value.maximumActiveConnections !== 16 ||
    !integer(value.activeConnections, 0, 16) ||
    !reasons.has(value.admissionReasonCode) ||
    !integer(value.timeoutSeconds, 1, 300) ||
    !Array.isArray(value.ports) ||
    value.ports.length < 1 ||
    value.ports.length > 16 ||
    !value.ports.every(
      (item) =>
        exact(item, ["port", "targetPort"]) &&
        integer(item.port, 1, 65535) &&
        integer(item.targetPort, 1, 65535),
    ) ||
    new Set(value.ports.map((item) => item.port)).size !== value.ports.length
  )
    invalid();
  return value;
}
function connectionReadback(value, input) {
  if (!exact(value, ["connection"], ["submission"])) invalid();
  return {
    connection: parsePortForwardConnection(value.connection, input),
    ...(Object.hasOwn(value, "submission")
      ? { submission: parsePortForwardSubmission(value.submission, input) }
      : {}),
  };
}
function connectionMutation(value, input) {
  if (!exact(value, ["connection", "input"], ["submission"])) invalid();
  const next = parsePortForwardInputView(value.input, input, input),
    result = connectionReadback(
      {
        connection: value.connection,
        ...(Object.hasOwn(value, "submission")
          ? { submission: value.submission }
          : {}),
      },
      input,
    );
  if (result.connection.inputVersion !== next.version) invalid();
  return { ...result, input: next };
}

// Frame выдаётся только с exact bigint cursor и current disclosure deadline; public read не создаёт ACK.
export function parsePortForwardOutput(value, input, after, serverNow) {
  if (
    !exact(
      value,
      [
        "invocationRef",
        "streamRef",
        "generation",
        "afterSequence",
        "outputWatermark",
        "droppedBytes",
        "status",
        "authorizedUntil",
        "nativeTerminalKnown",
        "transportJoined",
      ],
      ["frame"],
    ) ||
    value.invocationRef !== input.invocationRef ||
    value.streamRef !== input.streamRef ||
    value.generation !== String(input.generation) ||
    value.afterSequence !== String(after) ||
    typeof value.nativeTerminalKnown !== "boolean" ||
    typeof value.transportJoined !== "boolean" ||
    (value.transportJoined && !value.nativeTerminalKnown) ||
    !["WAITING", "FRAME", "INCOMPLETE", "ENDED"].includes(value.status)
  )
    invalid();
  const decimal = (text) => {
    if (typeof text !== "string" || !/^(0|[1-9][0-9]{0,18})$/u.test(text))
      invalid();
    const n = BigInt(text);
    if (n > 9223372036854775807n) invalid();
    return n;
  };
  const watermark = decimal(value.outputWatermark),
    dropped = decimal(value.droppedBytes),
    until = timestamp(value.authorizedUntil);
  if (
    watermark < after ||
    dropped > BigInt(input.maximumTotalBytes) ||
    until <= serverNow ||
    until > serverNow + 5000
  )
    invalid();
  if (
    value.status === "ENDED" &&
    (watermark !== after ||
      dropped !== 0n ||
      !value.nativeTerminalKnown ||
      !value.transportJoined)
  )
    invalid();
  if (value.status !== "FRAME") {
    if (Object.hasOwn(value, "frame")) invalid();
    return { ...value, watermark, dropped, deadline: until };
  }
  const f = value.frame;
  if (
    !exact(f, [
      "stageRef",
      "sequence",
      "channel",
      "payloadDigest",
      "payloadBase64",
    ]) ||
    !ref(f.stageRef, "kof") ||
    decimal(f.sequence) !== after + 1n ||
    after + 1n > watermark ||
    f.channel !== "PORT_FORWARD" ||
    !digest(f.payloadDigest) ||
    typeof f.payloadBase64 !== "string" ||
    f.payloadBase64.length > 43692
  )
    invalid();
  const payload = Buffer.from(f.payloadBase64, "base64");
  if (
    payload.length < 51 ||
    payload.length > input.maximumFrameBytes ||
    payload.toString("base64") !== f.payloadBase64 ||
    createHash("sha256").update(payload).digest("hex") !== f.payloadDigest
  ) {
    payload.fill(0);
    invalid();
  }
  const result = {
    status: value.status,
    sequence: after + 1n,
    watermark,
    dropped,
    deadline: until,
    nativeTerminalKnown: value.nativeTerminalKnown,
    transportJoined: value.transportJoined,
    payload,
  };
  Object.defineProperty(result, "payload", { enumerable: false });
  return result;
}

async function responseJSON(response, maximum = 65536) {
  const raw = await boundedResponseBody(response, maximum);
  try {
    return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(raw));
  } catch {
    invalid();
  } finally {
    raw.fill(0);
  }
}
// Production factory всегда использует прежний session client; fetchAPI — только transport test seam, не CLI flag.
export function createPortForwardOwnerClient({
  origin,
  storagePath,
  storage,
  fetchAPI,
}) {
  const session = createOwnerRuntimeSessionClient({
    origin,
    storagePath,
    storage,
    fetchAPI,
  });
  let clock;
  async function request(path, options = {}, expectedStatus = 200) {
    let response;
    try {
      response = await session.request(path, options);
    } catch {
      throw new PortForwardClientError("REQUEST_UNKNOWN");
    }
    const type = response.headers.get("content-type")?.split(";")[0].trim();
    const value = await responseJSON(response);
    if (!response.ok) {
      if (
        type !== "application/problem+json" ||
        !record(value) ||
        !knownProblems.has(value.code)
      )
        throw new PortForwardClientError("REQUEST_UNKNOWN", response.status);
      throw new PortForwardClientError(value.code, response.status);
    }
    if (
      response.status !== expectedStatus ||
      type !== "application/json" ||
      !response.headers
        .get("cache-control")
        ?.split(",")
        .some((part) => part.trim() === "no-store")
    )
      invalid();
    return value;
  }
  const root = (input) => {
    if (
      !input ||
      !ref(input.invocationRef, "inv") ||
      !ref(input.streamRef, "kst")
    )
      invalid();
    return `/api/v1/integration-invocations/${input.invocationRef}/kubernetes-streams/${input.streamRef}`;
  };
  const headers = (input, idempotency) => {
    if (!integer(input.version) || !key(idempotency)) invalid();
    return {
      "Content-Type": "application/json",
      "If-Match": `"${input.version}"`,
      "Idempotency-Key": idempotency,
    };
  };
  const client = {
    // Clock берётся через существующий authenticated session read; RTT добавляется целиком для conservative upper bound.
    async synchronizeClock(signal) {
      const start = performance.now();
      let response;
      try {
        response = await session.observe("/api/v1/session", { signal });
      } catch {
        throw new PortForwardClientError("REQUEST_UNKNOWN");
      }
      const value = await responseJSON(response, 16384),
        end = performance.now();
      if (
        response.status !== 200 ||
        !exact(value, [
          "generation",
          "version",
          "sessionRevision",
          "renewalMode",
          "serverTime",
          "expiresAt",
          "accessExpiresAt",
          "absoluteExpiresAt",
          "renewAfter",
        ])
      )
        invalid();
      const time = timestamp(value.serverTime);
      clock = { upper: time + (end - start), at: end };
    },
    serverNow() {
      if (!clock) invalid();
      const elapsed = performance.now() - clock.at;
      if (elapsed < 0) invalid();
      return clock.upper + elapsed;
    },
    async listInputs(context, signal, pageToken = "") {
      if (
        !context ||
        !ref(context.runRef, "run") ||
        !ref(context.nodeRef, "nod") ||
        typeof pageToken !== "string" ||
        pageToken.length > 2048
      )
        invalid();
      const value = await request(
        `/api/v1/runs/${context.runRef}/nodes/${context.nodeRef}/kubernetes-stream-inputs` +
          (pageToken ? "?pageToken=" + encodeURIComponent(pageToken) : ""),
        { signal },
      );
      if (
        !exact(value, ["items", "nextPageToken"]) ||
        !Array.isArray(value.items) ||
        value.items.length > 50 ||
        typeof value.nextPageToken !== "string" ||
        value.nextPageToken.length > 2048
      )
        invalid();
      // Другие семейства не входят в этот purpose consumer; explicit ref разрешается после discovery.
      const items = value.items
        .filter((item) => item?.operation === "kubernetes.port-forward")
        .map((item) => parsePortForwardInputView(item, context));
      if (new Set(items.map((item) => item.streamRef)).size !== items.length)
        invalid();
      return { items, nextPageToken: value.nextPageToken };
    },
    async listConnections(input, signal, pageToken = "") {
      if (pageToken !== "" && !connectionRef(pageToken)) invalid();
      const value = await request(
        root(input) +
          "/port-forward-connections" +
          (pageToken ? "?pageToken=" + encodeURIComponent(pageToken) : ""),
        { signal },
      );
      if (
        !exact(value, ["items", "nextPageToken", "profile"]) ||
        !Array.isArray(value.items) ||
        value.items.length > 32 ||
        (value.nextPageToken !== "" && !connectionRef(value.nextPageToken))
      )
        invalid();
      const items = value.items.map((item) =>
        parsePortForwardConnection(item, input),
      );
      if (
        new Set(items.map((item) => item.connectionRef)).size !== items.length
      )
        invalid();
      return {
        items,
        nextPageToken: value.nextPageToken,
        profile: parsePortForwardProfile(value.profile),
      };
    },
    async getInput(context, signal) {
      const value = await request(root(context) + "/input", { signal });
      if (!exact(value, ["view"])) invalid();
      return parsePortForwardInputView(value.view, context);
    },
    async readInput(input, signal, submissionKey) {
      const path =
        root(input) +
        "/input" +
        (submissionKey
          ? `?submissionIdempotencyKey=${encodeURIComponent(submissionKey)}`
          : "");
      if (submissionKey && !key(submissionKey)) invalid();
      const value = readback(await request(path, { signal }), input);
      if (!submissionKey && value.submission) invalid();
      return value;
    },
    async getConnection(input, selector, signal) {
      const entries = Object.entries(selector);
      if (entries.length !== 1) invalid();
      const [name, value] = entries[0];
      if (
        name === "connectionRef"
          ? !connectionRef(value)
          : name !== "openIdempotencyKey" || !key(value)
      )
        invalid();
      const result = connectionReadback(
        await request(
          root(input) +
            `/port-forward-connection?${name}=${encodeURIComponent(value)}`,
          { signal },
        ),
        input,
      );
      if (
        name === "connectionRef"
          ? result.connection.connectionRef !== value
          : !result.submission || result.submission.byteCount !== 51
      )
        invalid();
      return result;
    },
    async open(input, slot, idempotency, signal) {
      if (!integer(slot, 0, 15)) invalid();
      const result = connectionMutation(
        await request(
          root(input) + "/port-forward-connections",
          {
            method: "POST",
            headers: headers(input, idempotency),
            body: JSON.stringify({ slot }),
            signal,
          },
          201,
        ),
        input,
      );
      if (
        result.connection.slot !== slot ||
        !result.submission ||
        result.submission.byteCount !== 51
      )
        invalid();
      return result;
    },
    async close(input, connection, idempotency, signal) {
      parsePortForwardConnection(connection, input);
      const result = connectionMutation(
        await request(
          root(input) +
            `/port-forward-connections/${connection.connectionRef}/close`,
          {
            method: "POST",
            headers: headers(input, idempotency),
            body: "{}",
            signal,
          },
        ),
        input,
      );
      if (
        result.connection.connectionRef !== connection.connectionRef ||
        liveConnections.has(result.connection.state) ||
        (result.submission && result.submission.byteCount !== 51)
      )
        invalid();
      return result;
    },
    // Caller передаёт exclusive bytes; они очищаются и при protocol failure, и после неизвестного HTTP исхода.
    async submit(input, payload, idempotency, signal) {
      try {
        if (
          !input.inputChannels.includes("PORT_FORWARD") ||
          input.closedInputChannels.includes("PORT_FORWARD") ||
          payload.length > input.maximumFrameBytes ||
          !validPortForwardInput(payload, false)
        )
          invalid();
        const payloadDigest = createHash("sha256")
          .update(payload)
          .digest("hex");
        const result = readback(
          await request(
            root(input) + "/input",
            {
              method: "POST",
              headers: headers(input, idempotency),
              body: JSON.stringify({
                channel: "PORT_FORWARD",
                payloadBase64: Buffer.from(
                  payload.buffer,
                  payload.byteOffset,
                  payload.byteLength,
                ).toString("base64"),
                payloadDigest,
                endOfInput: false,
              }),
              signal,
            },
            201,
          ),
          input,
        );
        if (
          !result.submission ||
          result.submission.payloadDigest !== payloadDigest ||
          result.submission.byteCount !== payload.length ||
          result.submission.endOfInput
        )
          invalid();
        return result;
      } finally {
        payload.fill(0);
      }
    },
    async readOutput(input, after, signal) {
      if (
        typeof after !== "bigint" ||
        after < 0n ||
        after > 9223372036854775807n
      )
        invalid();
      return parsePortForwardOutput(
        await request(
          root(input) +
            `/output?generation=${input.generation}&afterSequence=${after}`,
          { signal },
        ),
        input,
        after,
        client.serverNow(),
      );
    },
  };
  return client;
}
