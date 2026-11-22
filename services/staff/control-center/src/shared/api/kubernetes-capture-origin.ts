import type { KubernetesArtifactCaptureOrigin } from "./generated/openapi/types.gen";

const invalidCaptureOrdinal = "Invalid Kubernetes capture ordinal";
const invalidCaptureEvidence = "Invalid Kubernetes artifact capture evidence";

/** Отклоняет null, массивы и scalar до доступа к полям внешней проекции. */
function captureObject(value: unknown): boolean {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

const streamReasons = new Set([
  "REMOTE_EXIT",
  "CLEAN_EOF",
  "REMOTE_FAILURE",
  "CANCELED",
  "REVOKED",
  "EXPIRED",
  "TRANSPORT_LOST",
  "OUTPUT_LOST",
  "AUTHORITY_UNAVAILABLE",
  "LIMIT_EXCEEDED",
  "HANDSHAKE_FAILED",
  "OWNER_STATE_LOST",
]);

/** Проверяет сохранённое описание результата; права на скачивание остаются у owner. */
export function assertArtifactCaptureOrigin(
  origin: KubernetesArtifactCaptureOrigin | undefined,
): KubernetesArtifactCaptureOrigin | undefined {
  if (origin === undefined) return undefined;
  const ordinal = (value: string): bigint => {
    if (!/^(0|[1-9][0-9]{0,18})$/u.test(value))
      throw new Error(invalidCaptureOrdinal);
    const parsed = BigInt(value);
    if (parsed > 9223372036854775807n) throw new Error(invalidCaptureOrdinal);
    return parsed;
  };
  if (
    !captureObject(origin) ||
    !["FULL", "PARTIAL"].includes(origin.kind) ||
    ![
      "kubernetes.exec",
      "kubernetes.attach",
      "kubernetes.debug",
      "kubernetes.logs",
      "kubernetes.watch",
      "kubernetes.watch.all-namespaces",
      "kubernetes.cp.upload",
      "kubernetes.cp.download",
      "kubernetes.port-forward",
    ].includes(origin.operation) ||
    !captureObject(origin.resource) ||
    typeof origin.resource.version !== "string" ||
    !origin.resource.version ||
    typeof origin.resource.resource !== "string" ||
    !origin.resource.resource ||
    typeof origin.resource.namespaced !== "boolean" ||
    typeof origin.resource.allNamespaces !== "boolean" ||
    ![
      origin.streamGeneration,
      origin.captureGeneration,
      origin.targetRevision,
    ].every((value) => Number.isSafeInteger(value) && value > 0) ||
    ![
      origin.invocationRef,
      origin.streamRef,
      origin.targetRef,
      origin.connectionRef,
    ].every(
      (value) =>
        typeof value === "string" &&
        value.length > 0 &&
        new TextEncoder().encode(value).length <= 96 &&
        !/\s|\p{Cc}/u.test(value),
    ) ||
    !["STDOUT", "STDERR", "CONTROL", "PORT_FORWARD", "FILE_CONTENT"].includes(
      origin.channel,
    ) ||
    !/^[a-f0-9]{64}$/u.test(origin.sourceDigest) ||
    !Number.isFinite(Date.parse(origin.sealedAt)) ||
    typeof origin.nativeTerminalKnown !== "boolean" ||
    typeof origin.transportJoined !== "boolean" ||
    !["COMPLETED", "CANCELED", "REVOKED", "EXPIRED", "LOST"].includes(
      origin.ownerState,
    ) ||
    ordinal(origin.acknowledgedPrefix) > ordinal(origin.outputWatermark) ||
    ordinal(origin.droppedBytes) > 268435456n
  )
    throw new Error("Invalid Kubernetes artifact capture origin");
  if (!origin.nativeTerminalKnown) {
    if (
      origin.transportJoined ||
      origin.localState !== "UNKNOWN" ||
      origin.remoteOutcome !== "UNKNOWN" ||
      origin.exitCode !== undefined ||
      origin.reasonCode !== ""
    )
      throw new Error(invalidCaptureEvidence);
  } else if (
    !["COMPLETED", "CANCELED", "REVOKED", "EXPIRED", "LOST"].includes(
      origin.localState,
    ) ||
    !["SUCCEEDED", "FAILED", "UNKNOWN"].includes(origin.remoteOutcome) ||
    !streamReasons.has(origin.reasonCode) ||
    (origin.exitCode !== undefined &&
      (!Number.isInteger(origin.exitCode) ||
        origin.exitCode < 0 ||
        origin.remoteOutcome === "UNKNOWN" ||
        (origin.remoteOutcome === "SUCCEEDED" && origin.exitCode !== 0) ||
        (origin.remoteOutcome === "FAILED" && origin.exitCode === 0)))
  )
    throw new Error(invalidCaptureEvidence);
  if (
    origin.kind === "FULL" &&
    (!origin.nativeTerminalKnown ||
      !origin.transportJoined ||
      origin.ownerState !== "COMPLETED" ||
      origin.localState !== "COMPLETED" ||
      origin.acknowledgedPrefix !== origin.outputWatermark ||
      origin.droppedBytes !== "0")
  )
    throw new Error("Invalid Kubernetes complete capture claim");
  return origin;
}
