import {
  getManagedKubernetesCaptureStatus,
  listManagedKubernetesCaptures,
  repairManagedKubernetesCapture,
  repairManagedKubernetesStagingCleanup,
} from "./generated/openapi/sdk.gen";
import type {
  KubernetesCaptureSummary,
  KubernetesCaptureReadback,
  KubernetesCaptureStatus,
  KubernetesCapturePage,
} from "./generated/openapi/types.gen";
import { hasExactKeys, isRecord } from "@/shared/lib/exact-object";
import { requestSignal } from "./client";
import { assertOwnerRequest } from "./owner-lifetime";
import { mutate, etag } from "./mutation";
import { unwrap } from "./problem";

export interface KubernetesCaptureContext {
  invocationRef: string;
  runRef: string;
  nodeRef: string;
}

// History не зависит от Tool/Connection permission; каждую страницу разрешает owner.
export async function listKubernetesCaptures(
  context: Pick<KubernetesCaptureContext, "runRef" | "nodeRef">,
  signal: AbortSignal,
  pageToken = "",
): Promise<KubernetesCapturePage> {
  if (
    !ref(context.runRef, "run") ||
    !ref(context.nodeRef, "nod") ||
    (pageToken !== "" && !ref(pageToken, "koc"))
  )
    throw new KubernetesCaptureProtocolError();
  assertOwnerRequest(signal);
  const response = await unwrap(
    listManagedKubernetesCaptures({
      path: context,
      query: { pageSize: 20, ...(pageToken ? { pageToken } : {}) },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  return parseKubernetesCapturePage(response.data, context, pageToken);
}

// Полная safe page проверяется до отображения; locator не заменяет exact run/node.
export function parseKubernetesCapturePage(
  value: unknown,
  context: Pick<KubernetesCaptureContext, "runRef" | "nodeRef">,
  pageToken = "",
): KubernetesCapturePage {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ["items", "nextPageToken"]) ||
    !Array.isArray(value.items) ||
    value.items.length > 20 ||
    typeof value.nextPageToken !== "string" ||
    (value.nextPageToken !== "" && !ref(value.nextPageToken, "koc"))
  )
    throw new KubernetesCaptureProtocolError();
  const items = value.items.map((item): KubernetesCaptureStatus => {
    if (!isRecord(item) || !ref(item.invocationRef, "inv"))
      throw new KubernetesCaptureProtocolError();
    const summary = parseKubernetesCaptureSummary(item.summary);
    return parseKubernetesCaptureReadback(
      { view: item },
      { ...context, invocationRef: item.invocationRef },
      summary.captureRef,
    ).view;
  });
  if (
    new Set(items.map((v) => v.summary.captureRef)).size !== items.length ||
    (value.nextPageToken !== "" &&
      (items.length !== 20 ||
        items.at(-1)?.summary.captureRef !== value.nextPageToken ||
        value.nextPageToken <= pageToken))
  )
    throw new KubernetesCaptureProtocolError();
  return { items, nextPageToken: value.nextPageToken };
}
const states = new Set([
  "READY",
  "CLAIMED",
  "COMPLETE",
  "UNKNOWN",
  "REJECTED",
  "WAITING_OWNER",
  "SUPERSEDED",
]);
const reasons = new Set([
  "",
  "CAPTURE_ELIGIBILITY_CHANGED",
  "CAPTURE_SOURCE_INVALID",
  "CAPTURE_STORAGE_UNKNOWN",
  "CAPTURE_RETRY_EXHAUSTED",
  "CAPTURE_RETENTION_EXPIRED",
  "CAPTURE_CONTENT_REJECTED",
  "CAPTURE_SUPERSEDED",
]);
export class KubernetesCaptureProtocolError extends Error {
  constructor() {
    super("Kubernetes capture metadata is invalid");
    this.name = "KubernetesCaptureProtocolError";
  }
}
function ref(v: unknown, prefix: string): v is string {
  return (
    typeof v === "string" &&
    v.startsWith(prefix + "_") &&
    /^[A-Za-z0-9_-]{12,96}$/u.test(v)
  );
}
function integer(v: unknown, min = 0): v is number {
  return typeof v === "number" && Number.isSafeInteger(v) && v >= min;
}
function timestamp(v: unknown): v is string {
  return (
    typeof v === "string" &&
    /^\d{4}-\d\d-\d\dT/u.test(v) &&
    Number.isFinite(Date.parse(v))
  );
}

// Ссылки дают discovery, а не authority на repair, artifact bytes или native execution.
export function parseKubernetesCaptureSummary(
  v: unknown,
): KubernetesCaptureSummary {
  if (
    !isRecord(v) ||
    !hasExactKeys(v, [
      "captureRef",
      "streamRef",
      "captureGeneration",
      "version",
      "state",
      "reasonCode",
    ]) ||
    !ref(v.captureRef, "koc") ||
    !ref(v.streamRef, "kst") ||
    !integer(v.captureGeneration, 1) ||
    !integer(v.version, 1) ||
    typeof v.state !== "string" ||
    !states.has(v.state) ||
    typeof v.reasonCode !== "string" ||
    !reasons.has(v.reasonCode)
  )
    throw new KubernetesCaptureProtocolError();
  return v as KubernetesCaptureSummary;
}
export function parseKubernetesCaptureReadback(
  value: unknown,
  context: KubernetesCaptureContext,
  captureRef: string,
): KubernetesCaptureReadback {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "view",
      ...(Object.hasOwn(value, "repairReceipt") ? ["repairReceipt"] : []),
      ...(Object.hasOwn(value, "cleanupReceipt") ? ["cleanupReceipt"] : []),
    ]) ||
    !isRecord(value.view)
  )
    throw new KubernetesCaptureProtocolError();
  const v = value.view;
  if (
    !hasExactKeys(v, [
      "summary",
      "invocationRef",
      "runRef",
      "nodeRef",
      "operation",
      "kind",
      "attemptCount",
      "repairGeneration",
      "sealedAt",
      "artifacts",
      ...(Object.hasOwn(v, "retainUntil") ? ["retainUntil"] : []),
      ...(Object.hasOwn(v, "cleanup") ? ["cleanup"] : []),
    ])
  )
    throw new KubernetesCaptureProtocolError();
  const summary = parseKubernetesCaptureSummary(v.summary);
  if (
    summary.captureRef !== captureRef ||
    !ref(context.invocationRef, "inv") ||
    !ref(context.runRef, "run") ||
    !ref(context.nodeRef, "nod") ||
    v.invocationRef !== context.invocationRef ||
    v.runRef !== context.runRef ||
    v.nodeRef !== context.nodeRef ||
    typeof v.operation !== "string" ||
    !/^kubernetes\.[a-z.-]{1,84}$/u.test(v.operation) ||
    !integer(v.attemptCount) ||
    !integer(v.repairGeneration) ||
    !timestamp(v.sealedAt) ||
    (v.retainUntil !== undefined &&
      (!timestamp(v.retainUntil) ||
        Date.parse(v.retainUntil) <= Date.parse(v.sealedAt))) ||
    !Array.isArray(v.artifacts) ||
    v.artifacts.length > 5 ||
    typeof v.kind !== "string" ||
    !["NONE", "EMPTY", "FULL", "PARTIAL"].includes(v.kind) ||
    (summary.state === "COMPLETE") !== (v.kind !== "NONE") ||
    ((v.kind === "NONE" || v.kind === "EMPTY") && v.artifacts.length !== 0) ||
    (v.kind === "FULL" && v.artifacts.length === 0)
  )
    throw new KubernetesCaptureProtocolError();
  const seen = new Set<string>();
  for (const a of v.artifacts) {
    if (
      !isRecord(a) ||
      !hasExactKeys(a, [
        "channel",
        "artifactRef",
        "revisionRef",
        "digest",
        "sizeBytes",
        "captureGeneration",
        "partial",
      ]) ||
      typeof a.channel !== "string" ||
      !["STDOUT", "STDERR", "CONTROL", "PORT_FORWARD", "FILE_CONTENT"].includes(
        a.channel,
      ) ||
      seen.has(a.channel) ||
      !ref(a.artifactRef, "art") ||
      !ref(a.revisionRef, "arv") ||
      typeof a.digest !== "string" ||
      !/^sha256:[a-f0-9]{64}$/u.test(a.digest) ||
      !integer(a.sizeBytes) ||
      a.sizeBytes > 536870912 ||
      a.captureGeneration !== summary.captureGeneration ||
      typeof a.partial !== "boolean" ||
      (v.kind === "FULL" && a.partial)
    )
      throw new KubernetesCaptureProtocolError();
    seen.add(a.channel);
  }
  if (value.repairReceipt !== undefined) {
    const r = value.repairReceipt;
    if (
      !isRecord(r) ||
      !hasExactKeys(r, [
        "captureRef",
        "repairGeneration",
        "version",
        "attemptCount",
        "acceptedAt",
      ]) ||
      r.captureRef !== captureRef ||
      !integer(r.repairGeneration, 1) ||
      r.repairGeneration > v.repairGeneration ||
      !integer(r.version, 2) ||
      r.version > summary.version ||
      !integer(r.attemptCount) ||
      r.attemptCount > v.attemptCount ||
      !timestamp(r.acceptedAt) ||
      Date.parse(r.acceptedAt) < Date.parse(v.sealedAt)
    )
      throw new KubernetesCaptureProtocolError();
  }
  if (v.cleanup !== undefined) {
    const c = v.cleanup;
    if (
      !isRecord(c) ||
      !hasExactKeys(c, [
        "version",
        "repairGeneration",
        "outstandingCount",
        "outstandingBytes",
        "exhaustedCount",
        "eligibleExhaustedCount",
      ]) ||
      !integer(c.version, 1) ||
      !integer(c.repairGeneration) ||
      !integer(c.outstandingCount) ||
      !integer(c.outstandingBytes) ||
      !integer(c.exhaustedCount) ||
      c.exhaustedCount > c.outstandingCount ||
      !integer(c.eligibleExhaustedCount) ||
      c.eligibleExhaustedCount > c.exhaustedCount
    )
      throw new KubernetesCaptureProtocolError();
  }
  if (value.cleanupReceipt !== undefined) {
    const r = value.cleanupReceipt,
      c = v.cleanup;
    if (
      !isRecord(r) ||
      !isRecord(c) ||
      !hasExactKeys(r, [
        "captureRef",
        "version",
        "repairGeneration",
        "repairedCount",
        "remainingExhaustedCount",
        "acceptedAt",
      ]) ||
      r.captureRef !== captureRef ||
      !integer(r.version, 2) ||
      !integer(c.version, 1) ||
      r.version > c.version ||
      !integer(r.repairGeneration, 1) ||
      !integer(c.repairGeneration) ||
      r.repairGeneration > c.repairGeneration ||
      !integer(r.repairedCount, 1) ||
      r.repairedCount > 100 ||
      !integer(r.remainingExhaustedCount) ||
      !timestamp(r.acceptedAt) ||
      Date.parse(r.acceptedAt) < Date.parse(v.sealedAt)
    )
      throw new KubernetesCaptureProtocolError();
  }
  return value as unknown as KubernetesCaptureReadback;
}
export async function readKubernetesCapture(
  context: KubernetesCaptureContext,
  captureRef: string,
  signal: AbortSignal,
  key = "",
  cleanupKey = "",
): Promise<KubernetesCaptureReadback> {
  assertOwnerRequest(signal);
  const response = await unwrap(
    getManagedKubernetesCaptureStatus({
      path: { captureRef },
      query: {
        ...(key ? { repairIdempotencyKey: key } : {}),
        ...(cleanupKey ? { cleanupIdempotencyKey: cleanupKey } : {}),
      },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  return parseKubernetesCaptureReadback(response.data, context, captureRef);
}
// Один POST. Неизвестный результат читается тем же key и никогда не повторяет native либо payload.
export async function repairKubernetesCapture(
  context: KubernetesCaptureContext,
  selected: KubernetesCaptureStatus,
  signal: AbortSignal,
  key: string,
): Promise<KubernetesCaptureReadback> {
  assertOwnerRequest(signal);
  parseKubernetesCaptureReadback(
    { view: selected },
    context,
    selected.summary.captureRef,
  );
  if (selected.summary.state !== "WAITING_OWNER")
    throw new KubernetesCaptureProtocolError();
  const response = await mutate(
    (headers) =>
      repairManagedKubernetesCapture({
        path: { captureRef: selected.summary.captureRef },
        body: {},
        headers: { ...headers, "If-Match": etag(selected.summary.version) },
        signal: requestSignal(signal),
      }),
    selected.summary.version,
    key,
  );
  assertOwnerRequest(signal);
  const result = parseKubernetesCaptureReadback(
    response.data,
    context,
    selected.summary.captureRef,
  );
  if (!result.repairReceipt) throw new KubernetesCaptureProtocolError();
  return result;
}

// Один новый cleanup cycle; payload и native execution не отправляются повторно.
export async function repairKubernetesStagingCleanup(
  context: KubernetesCaptureContext,
  selected: KubernetesCaptureStatus,
  signal: AbortSignal,
  key: string,
): Promise<KubernetesCaptureReadback> {
  assertOwnerRequest(signal);
  parseKubernetesCaptureReadback(
    { view: selected },
    context,
    selected.summary.captureRef,
  );
  const cleanup = selected.cleanup;
  if (!cleanup || cleanup.eligibleExhaustedCount < 1)
    throw new KubernetesCaptureProtocolError();
  const response = await mutate(
    (headers) =>
      repairManagedKubernetesStagingCleanup({
        path: { captureRef: selected.summary.captureRef },
        body: {},
        headers: { ...headers, "If-Match": etag(cleanup.version) },
        signal: requestSignal(signal),
      }),
    cleanup.version,
    key,
  );
  assertOwnerRequest(signal);
  const result = parseKubernetesCaptureReadback(
    response.data,
    context,
    selected.summary.captureRef,
  );
  if (!result.cleanupReceipt) throw new KubernetesCaptureProtocolError();
  return result;
}
