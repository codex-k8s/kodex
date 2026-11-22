import type {
  KubernetesFileCleanupView,
  KubernetesFileCleanupReadback,
  KubernetesFileInputView,
  KubernetesStagingCleanupStatus,
} from "../api/generated/openapi/types.gen";

const fields = [
  "sourceRef",
  "invocationRef",
  "planRef",
  "childRef",
  "streamRef",
  "state",
  "version",
  "artifactRef",
  "artifactRevisionRef",
  "artifactSHA256",
  "artifactSizeBytes",
  "transferFormat",
  "normalizedSHA256",
  "normalizedSizeBytes",
  "normalizationRevision",
  "stagedBytes",
  "acknowledgedBytes",
  "eofAcknowledged",
  "reason",
  "createdAt",
  "hardDeadline",
  "cleanup",
] as const;
const states = new Set([
  "PREPARING",
  "READY",
  "UNKNOWN",
  "REJECTED",
  "CANCELLED",
  "TRANSFERRED",
  "EXPIRED",
]);
const reasons = new Set([
  "NONE",
  "SOURCE_UNAVAILABLE",
  "SOURCE_CHANGED",
  "PERMISSION_LOST",
  "CONTENT_REJECTED",
  "INPUT_UNKNOWN",
  "CANCELLED",
  "EXPIRED",
]);
export class KubernetesFileProtocolError extends Error {
  constructor() {
    super("Kubernetes file input metadata is invalid");
    this.name = "KubernetesFileProtocolError";
  }
}
function opaque(value: unknown): value is string {
  return typeof value === "string" && /^[A-Za-z0-9_-]{1,96}$/u.test(value);
}
function ref(value: unknown, prefix: string): value is string {
  return opaque(value) && value.startsWith(prefix + "_") && value.length >= 12;
}
function integer(
  value: unknown,
  maximum = 16777216,
  minimum = 0,
): value is number {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= minimum &&
    value <= maximum
  );
}
function timestamp(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length <= 40 &&
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/u.test(
      value,
    ) &&
    Number.isFinite(Date.parse(value))
  );
}
function digest(value: unknown): value is string {
  return typeof value === "string" && /^[a-f0-9]{64}$/u.test(value);
}
// Общая PWA/owner-local CLI projection: source metadata не подтверждает native exit либо разрешение INPUT.
export function parseKubernetesFileInput(
  value: unknown,
  invocationRef: string,
  sourceRef?: string,
): KubernetesFileInputView {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    throw new KubernetesFileProtocolError();
  const v = value as Record<string, unknown>;
  if (
    Object.keys(v).length !== fields.length ||
    !fields.every((field) => Object.hasOwn(v, field)) ||
    !ref(invocationRef, "inv") ||
    v.invocationRef !== invocationRef ||
    !ref(v.sourceRef, "kfs") ||
    (sourceRef !== undefined && v.sourceRef !== sourceRef) ||
    !ref(v.planRef, "kpl") ||
    !ref(v.childRef, "kch") ||
    (v.streamRef !== "" && !ref(v.streamRef, "kst")) ||
    typeof v.state !== "string" ||
    !states.has(v.state) ||
    !integer(v.version, Number.MAX_SAFE_INTEGER, 1) ||
    !opaque(v.artifactRef) ||
    !opaque(v.artifactRevisionRef) ||
    !digest(v.artifactSHA256) ||
    !integer(v.artifactSizeBytes) ||
    typeof v.transferFormat !== "string" ||
    !["FILE", "TAR"].includes(v.transferFormat) ||
    !integer(v.normalizedSizeBytes) ||
    !integer(v.normalizationRevision, 1) ||
    !integer(v.stagedBytes) ||
    !integer(v.acknowledgedBytes) ||
    v.acknowledgedBytes > v.stagedBytes ||
    v.stagedBytes > v.normalizedSizeBytes ||
    typeof v.eofAcknowledged !== "boolean" ||
    typeof v.reason !== "string" ||
    !reasons.has(v.reason) ||
    !timestamp(v.createdAt) ||
    !timestamp(v.hardDeadline) ||
    Date.parse(v.hardDeadline) <= Date.parse(v.createdAt)
  )
    throw new KubernetesFileProtocolError();
  if (v.cleanup !== null) parseKubernetesFileCleanupStatus(v.cleanup);
  if (v.normalizedSHA256 === "") {
    if (
      v.normalizedSizeBytes !== 0 ||
      v.normalizationRevision !== 0 ||
      v.stagedBytes !== 0 ||
      v.acknowledgedBytes !== 0 ||
      v.eofAcknowledged ||
      ["READY", "TRANSFERRED"].includes(v.state)
    )
      throw new KubernetesFileProtocolError();
  } else if (
    !digest(v.normalizedSHA256) ||
    v.normalizedSizeBytes < 1 ||
    v.normalizationRevision !== 1 ||
    (v.eofAcknowledged && v.acknowledgedBytes !== v.normalizedSizeBytes) ||
    (v.state === "TRANSFERRED" && !v.eofAcknowledged)
  )
    throw new KubernetesFileProtocolError();
  return value as KubernetesFileInputView;
}
// Overflow и mixed/duplicate source исключают весь ответ, не только один элемент.
export function parseKubernetesFileInputList(
  value: unknown,
  invocationRef: string,
): KubernetesFileInputView[] {
  if (
    typeof value !== "object" ||
    value === null ||
    Array.isArray(value) ||
    Object.keys(value).length !== 2 ||
    !Object.hasOwn(value, "items") ||
    (value as Record<string, unknown>).projection !== "SOURCE"
  )
    throw new KubernetesFileProtocolError();
  const items = (value as Record<string, unknown>).items;
  if (!Array.isArray(items) || items.length > 256)
    throw new KubernetesFileProtocolError();
  const result = items.map((item) =>
    parseKubernetesFileInput(item, invocationRef),
  );
  if (new Set(result.map((item) => item.sourceRef)).size !== result.length)
    throw new KubernetesFileProtocolError();
  return result;
}

// Locators проверяют только адрес metadata; authority остаётся в owner.
export function validKubernetesFileInvocation(value: unknown): value is string {
  return ref(value, "inv");
}
export function validKubernetesFileSource(value: unknown): value is string {
  return ref(value, "kfs");
}
// Локальные shape guards оставляют общий PWA/Node codec без runtime resolution зависимости bundler.
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function exact(value: Record<string, unknown>, fields: string[]): boolean {
  return (
    Object.keys(value).length === fields.length &&
    fields.every((field) => Object.hasOwn(value, field))
  );
}
// Один физический source object; metadata INPUT stages не создают объекты очистки.
export function parseKubernetesFileCleanupStatus(
  value: unknown,
): KubernetesStagingCleanupStatus {
  if (
    !record(value) ||
    !exact(value, [
      "version",
      "repairGeneration",
      "outstandingCount",
      "outstandingBytes",
      "exhaustedCount",
      "eligibleExhaustedCount",
    ]) ||
    !integer(value.version, Number.MAX_SAFE_INTEGER, 1) ||
    !integer(value.repairGeneration, Number.MAX_SAFE_INTEGER) ||
    !integer(value.outstandingCount, 1) ||
    !integer(value.outstandingBytes) ||
    !integer(value.exhaustedCount, 1) ||
    !integer(value.eligibleExhaustedCount, 1) ||
    value.exhaustedCount > value.outstandingCount ||
    value.eligibleExhaustedCount > value.exhaustedCount
  )
    throw new KubernetesFileProtocolError();
  return value as KubernetesStagingCleanupStatus;
}
// Closure-only metadata остаётся доступной без artifact.view; здесь невозможно представить provenance или bytes.
export function parseKubernetesFileCleanupView(
  value: unknown,
  invocationRef: string,
  sourceRef?: string,
): KubernetesFileCleanupView {
  if (
    !record(value) ||
    !exact(
      value,
      Object.hasOwn(value, "repairAvailable")
        ? ["sourceRef", "invocationRef", "cleanup", "repairAvailable"]
        : ["sourceRef", "invocationRef", "cleanup"],
    ) ||
    (Object.hasOwn(value, "repairAvailable") &&
      typeof value.repairAvailable !== "boolean") ||
    !ref(invocationRef, "inv") ||
    value.invocationRef !== invocationRef ||
    !ref(value.sourceRef, "kfs") ||
    (sourceRef !== undefined && value.sourceRef !== sourceRef)
  )
    throw new KubernetesFileProtocolError();
  parseKubernetesFileCleanupStatus(value.cleanup);
  return value as KubernetesFileCleanupView;
}
export function parseKubernetesFileCleanupList(
  value: unknown,
  invocationRef: string,
): KubernetesFileCleanupView[] {
  if (
    !record(value) ||
    !exact(value, ["projection", "items"]) ||
    value.projection !== "CLEANUP_ONLY" ||
    !Array.isArray(value.items) ||
    value.items.length > 256
  )
    throw new KubernetesFileProtocolError();
  const items = value.items.map((item) =>
    parseKubernetesFileCleanupView(item, invocationRef),
  );
  if (new Set(items.map((item) => item.sourceRef)).size !== items.length)
    throw new KubernetesFileProtocolError();
  return items;
}
export function parseKubernetesFileSourceReadback(
  value: unknown,
  invocationRef: string,
  sourceRef: string,
): KubernetesFileInputView {
  if (
    !record(value) ||
    !exact(value, ["projection", "view"]) ||
    value.projection !== "SOURCE"
  )
    throw new KubernetesFileProtocolError();
  return parseKubernetesFileInput(value.view, invocationRef, sourceRef);
}
// Original-key receipt подтверждает новый cleanup cycle, не DELETE и не успешный native effect.
export function parseKubernetesFileCleanupReadback(
  value: unknown,
  invocationRef: string,
  sourceRef: string,
  requireReceipt = false,
): KubernetesFileCleanupReadback {
  if (
    !record(value) ||
    !exact(value, [
      "projection",
      "cleanupView",
      ...(Object.hasOwn(value, "cleanupReceipt") ? ["cleanupReceipt"] : []),
    ]) ||
    value.projection !== "CLEANUP_ONLY"
  )
    throw new KubernetesFileProtocolError();
  const view = parseKubernetesFileCleanupView(
      value.cleanupView,
      invocationRef,
      sourceRef,
    ),
    r = value.cleanupReceipt;
  if (r !== undefined) {
    if (
      !record(r) ||
      !exact(r, [
        "sourceRef",
        "version",
        "repairGeneration",
        "repairedCount",
        "remainingExhaustedCount",
        "acceptedAt",
      ]) ||
      r.sourceRef !== sourceRef ||
      !integer(r.version, view.cleanup.version, 2) ||
      !integer(r.repairGeneration, view.cleanup.repairGeneration, 1) ||
      r.repairedCount !== 1 ||
      !integer(r.remainingExhaustedCount, 1) ||
      !timestamp(r.acceptedAt) ||
      Date.parse(r.acceptedAt) > Date.now() + 30000
    )
      throw new KubernetesFileProtocolError();
  } else if (requireReceipt) throw new KubernetesFileProtocolError();
  return value as KubernetesFileCleanupReadback;
}
