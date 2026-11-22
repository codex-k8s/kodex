import {
  getManagedKubernetesStreamInput,
  listManagedKubernetesStreamInputs,
  stopManagedKubernetesStream,
  submitManagedKubernetesStreamInput,
} from "./generated/openapi/sdk.gen";
import type {
  KubernetesStreamInputChannel,
  KubernetesStreamInputPage,
  KubernetesStreamInputReadback,
  KubernetesStreamInputSubmission,
  KubernetesStreamInputView,
} from "./generated/openapi/types.gen";
import { validPortForwardInput } from "@/shared/lib/kubernetes-port-forward-frame";
import { hasExactKeys, isRecord } from "@/shared/lib/exact-object";
import { requestSignal } from "./client";
import { assertOwnerRequest } from "./owner-lifetime";
import { etag, mutate } from "./mutation";
import { unwrap } from "./problem";

export interface KubernetesInputContext {
  runRef: string;
  nodeRef: string;
}
export type KubernetesPublicInputChannel =
  | "STDIN"
  | "TERMINAL_RESIZE"
  | "PORT_FORWARD";
export class KubernetesInputProtocolError extends Error {
  constructor() {
    super("Kubernetes stream input metadata or payload is invalid");
    this.name = "KubernetesInputProtocolError";
  }
}
const references = /^[A-Za-z0-9_-]{12,96}$/u;
const digest = /^[a-f0-9]{64}$/u;
const channels = new Set([
  "STDIN",
  "TERMINAL_RESIZE",
  "PORT_FORWARD",
  "FILE_CONTENT",
  "CONTROL",
]);
const states = new Set([
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
const streamOperations = new Set([
  "kubernetes.watch",
  "kubernetes.watch.all-namespaces",
  "kubernetes.logs",
  "kubernetes.exec",
  "kubernetes.attach",
  "kubernetes.debug",
  "kubernetes.cp.upload",
  "kubernetes.cp.download",
  "kubernetes.port-forward",
]);
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
  "KUBERNETES_STAGING_QUOTA_EXCEEDED",
]);
export function validKubernetesStreamReason(value: unknown): value is string {
  return typeof value === "string" && reasons.has(value);
}
function reference(value: unknown, prefix: string): value is string {
  return (
    typeof value === "string" &&
    references.test(value) &&
    value.startsWith(`${prefix}_`)
  );
}
function integer(
  value: unknown,
  min: number,
  max = Number.MAX_SAFE_INTEGER,
): value is number {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= min &&
    value <= max
  );
}
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
function dnsLabel(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$/u.test(value)
  );
}
function dnsName(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length <= 253 &&
    value.split(".").every(dnsLabel)
  );
}
function token(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length <= 2048 &&
    !/[\p{Cc}\uD800-\uDFFF]/u.test(value)
  );
}
export function kubernetesInputContextValid(
  context: KubernetesInputContext,
): boolean {
  return reference(context.runRef, "run") && reference(context.nodeRef, "nod");
}
export function parseKubernetesInputSubmission(
  value: unknown,
  view: KubernetesStreamInputView,
): KubernetesStreamInputSubmission {
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
    !reference(value.ref, "kif") ||
    value.invocationRef !== view.invocationRef ||
    value.streamRef !== view.streamRef ||
    value.generation !== view.generation ||
    !integer(value.sequence, 1) ||
    typeof value.channel !== "string" ||
    !view.inputChannels.includes(
      value.channel as KubernetesStreamInputChannel,
    ) ||
    typeof value.payloadDigest !== "string" ||
    !digest.test(value.payloadDigest) ||
    !integer(value.byteCount, 0, view.maximumFrameBytes) ||
    typeof value.endOfInput !== "boolean" ||
    (value.byteCount === 0) !== value.endOfInput ||
    typeof value.state !== "string" ||
    !submissionStates.has(value.state)
  )
    throw new KubernetesInputProtocolError();
  return value as unknown as KubernetesStreamInputSubmission;
}
function validInputResource(
  value: unknown,
  operation: unknown,
  namespace: unknown,
  pod: unknown,
  container: unknown,
): boolean {
  if (
    !exact(value, [
      "group",
      "version",
      "resource",
      "namespace",
      "name",
      "uid",
      "resourceVersion",
      "subresource",
      "namespaced",
      "allNamespaces",
    ]) ||
    typeof value.group !== "string" ||
    (value.group !== "" && !dnsName(value.group)) ||
    typeof value.version !== "string" ||
    !/^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$/u.test(value.version) ||
    typeof value.resource !== "string" ||
    !/^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$/u.test(value.resource) ||
    typeof value.namespaced !== "boolean" ||
    typeof value.allNamespaces !== "boolean" ||
    typeof value.namespace !== "string" ||
    typeof value.name !== "string" ||
    typeof value.uid !== "string" ||
    typeof value.resourceVersion !== "string" ||
    value.subresource !== ""
  )
    return false;
  if (
    value.namespaced && !value.allNamespaces
      ? !dnsLabel(value.namespace)
      : value.namespace !== ""
  )
    return false;
  if (
    operation === "kubernetes.watch" ||
    operation === "kubernetes.watch.all-namespaces"
  )
    return (
      pod === "" &&
      container === "" &&
      value.name === "" &&
      value.uid === "" &&
      value.resourceVersion === "" &&
      value.allNamespaces ===
        (operation === "kubernetes.watch.all-namespaces") &&
      (!value.allNamespaces || value.namespaced) &&
      namespace ===
        (value.allNamespaces
          ? "@all-namespaces"
          : value.namespaced
            ? value.namespace
            : "@cluster")
    );
  return (
    value.group === "" &&
    value.version === "v1" &&
    value.resource === "pods" &&
    value.namespaced &&
    !value.allNamespaces &&
    value.namespace === namespace &&
    value.name === pod &&
    dnsName(pod) &&
    (dnsLabel(container) ||
      (operation === "kubernetes.logs" && container === "")) &&
    /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/u.test(
      value.uid,
    ) &&
    value.uid !== "00000000-0000-0000-0000-000000000000" &&
    value.resourceVersion !== "" &&
    new TextEncoder().encode(value.resourceVersion).length <= 256 &&
    !/[\s\p{Cc}\uD800-\uDFFF]/u.test(value.resourceVersion)
  );
}
export function parseKubernetesInputView(
  value: unknown,
  context: KubernetesInputContext,
): KubernetesStreamInputView {
  if (
    !kubernetesInputContextValid(context) ||
    !exact(
      value,
      [
        "invocationRef",
        "streamRef",
        "runRef",
        "nodeRef",
        "generation",
        "operation",
        "namespace",
        "podName",
        "container",
        "state",
        "version",
        "inputChannels",
        "closedInputChannels",
        "maximumFrameBytes",
        "maximumTotalBytes",
        "maximumStagedBytes",
        "maximumStagedObjects",
        "acknowledgedSequence",
        "hardDeadline",
        "reasonCode",
        "resource",
        "targetRef",
        "targetRevision",
        "connectionRef",
      ],
      ["pending"],
    ) ||
    !reference(value.invocationRef, "inv") ||
    !reference(value.streamRef, "kst") ||
    value.runRef !== context.runRef ||
    value.nodeRef !== context.nodeRef ||
    !integer(value.generation, 1) ||
    !integer(value.version, 1) ||
    typeof value.operation !== "string" ||
    !streamOperations.has(value.operation) ||
    !validInputResource(
      value.resource,
      value.operation,
      value.namespace,
      value.podName,
      value.container,
    ) ||
    typeof value.targetRef !== "string" ||
    value.targetRef === "" ||
    new TextEncoder().encode(value.targetRef).length > 96 ||
    /[\s\p{Cc}\uD800-\uDFFF]/u.test(value.targetRef) ||
    !integer(value.targetRevision, 1) ||
    !reference(value.connectionRef, "int") ||
    typeof value.state !== "string" ||
    !states.has(value.state) ||
    !integer(value.maximumFrameBytes, 1, 32768) ||
    !integer(value.maximumTotalBytes, value.maximumFrameBytes, 268435456) ||
    !integer(value.maximumStagedBytes, 1, 1073741824) ||
    !integer(value.maximumStagedObjects, 1, 32768) ||
    !integer(value.acknowledgedSequence, 0) ||
    typeof value.hardDeadline !== "string" ||
    value.hardDeadline.length > 64 ||
    !Number.isFinite(Date.parse(value.hardDeadline)) ||
    typeof value.reasonCode !== "string" ||
    !reasons.has(value.reasonCode) ||
    !Array.isArray(value.inputChannels) ||
    value.inputChannels.length > 7 ||
    !value.inputChannels.every(
      (entry) => typeof entry === "string" && channels.has(entry),
    ) ||
    new Set(value.inputChannels).size !== value.inputChannels.length ||
    (["kubernetes.watch", "kubernetes.watch.all-namespaces"].includes(
      value.operation,
    ) &&
      value.inputChannels.length !== 0) ||
    !Array.isArray(value.closedInputChannels) ||
    value.closedInputChannels.length > 7 ||
    !value.closedInputChannels.every((entry) =>
      (value.inputChannels as unknown[]).includes(entry),
    ) ||
    new Set(value.closedInputChannels).size !== value.closedInputChannels.length
  )
    throw new KubernetesInputProtocolError();
  const result = value as unknown as KubernetesStreamInputView;
  if (Object.hasOwn(value, "pending"))
    parseKubernetesInputSubmission(value.pending, result);
  return result;
}
export function sameKubernetesInputStream(
  a: KubernetesStreamInputView,
  b: KubernetesStreamInputView,
): boolean {
  return (
    (
      [
        "invocationRef",
        "streamRef",
        "runRef",
        "nodeRef",
        "generation",
        "operation",
        "namespace",
        "podName",
        "container",
        "hardDeadline",
        "maximumFrameBytes",
        "maximumTotalBytes",
        "maximumStagedBytes",
        "maximumStagedObjects",
        "targetRef",
        "targetRevision",
        "connectionRef",
      ] as const
    ).every((key) => a[key] === b[key]) &&
    (Object.keys(a.resource) as (keyof typeof a.resource)[]).every(
      (key) => a.resource[key] === b.resource[key],
    ) &&
    a.inputChannels.length === b.inputChannels.length &&
    a.inputChannels.every((entry, index) => entry === b.inputChannels[index])
  );
}
function parseReadback(
  value: unknown,
  selected: KubernetesStreamInputView,
): KubernetesStreamInputReadback {
  if (!exact(value, ["view"], ["submission"]))
    throw new KubernetesInputProtocolError();
  const view = parseKubernetesInputView(value.view, selected);
  if (
    !sameKubernetesInputStream(view, selected) ||
    view.version < selected.version ||
    view.acknowledgedSequence < selected.acknowledgedSequence
  )
    throw new KubernetesInputProtocolError();
  const result: KubernetesStreamInputReadback = { view };
  if (Object.hasOwn(value, "submission"))
    result.submission = parseKubernetesInputSubmission(value.submission, view);
  return result;
}
export async function listKubernetesStreamInputs(
  context: KubernetesInputContext,
  signal: AbortSignal,
  pageToken = "",
): Promise<KubernetesStreamInputPage> {
  if (!kubernetesInputContextValid(context) || !token(pageToken))
    throw new KubernetesInputProtocolError();
  const response = await unwrap(
    listManagedKubernetesStreamInputs({
      path: context,
      query: { pageSize: 20, ...(pageToken ? { pageToken } : {}) },
      signal: requestSignal(signal),
    }),
  );
  const value: unknown = response.data;
  if (
    !exact(value, ["items", "nextPageToken"]) ||
    !Array.isArray(value.items) ||
    value.items.length > 20 ||
    !token(value.nextPageToken)
  )
    throw new KubernetesInputProtocolError();
  const items = value.items.map((entry) =>
    parseKubernetesInputView(entry, context),
  );
  if (new Set(items.map((entry) => entry.streamRef)).size !== items.length)
    throw new KubernetesInputProtocolError();
  return { items, nextPageToken: value.nextPageToken };
}
export async function readKubernetesStreamInput(
  selected: KubernetesStreamInputView,
  signal: AbortSignal,
  submissionIdempotencyKey?: string,
): Promise<KubernetesStreamInputReadback> {
  if (
    submissionIdempotencyKey !== undefined &&
    !/^[A-Za-z0-9._:-]{8,128}$/u.test(submissionIdempotencyKey)
  )
    throw new KubernetesInputProtocolError();
  const response = await unwrap(
    getManagedKubernetesStreamInput({
      path: {
        invocationRef: selected.invocationRef,
        streamRef: selected.streamRef,
      },
      query: submissionIdempotencyKey ? { submissionIdempotencyKey } : {},
      signal: requestSignal(signal),
    }),
  );
  const result = parseReadback(response.data, selected);
  if (!submissionIdempotencyKey && result.submission)
    throw new KubernetesInputProtocolError();
  return result;
}
function base64(bytes: Uint8Array): string {
  let text = "";
  for (let offset = 0; offset < bytes.length; offset += 8192)
    text += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
  return btoa(text);
}
export async function kubernetesInputDigest(
  bytes: Uint8Array,
): Promise<string> {
  const owned = Uint8Array.from(bytes);
  try {
    const hash = await crypto.subtle.digest("SHA-256", owned);
    return Array.from(new Uint8Array(hash), (entry) =>
      entry.toString(16).padStart(2, "0"),
    ).join("");
  } finally {
    owned.fill(0);
  }
}
// Payload принадлежит этому вызову сразу, включая ошибку. Ни write retry, ни storage/console echo здесь нет.
export async function submitKubernetesStreamInput(
  selected: KubernetesStreamInputView,
  channel: KubernetesPublicInputChannel,
  payload: Uint8Array,
  endOfInput: boolean,
  signal: AbortSignal,
  key: string,
): Promise<KubernetesStreamInputReadback> {
  try {
    if (
      payload.length > Math.min(32768, selected.maximumFrameBytes) ||
      !selected.inputChannels.includes(channel) ||
      selected.closedInputChannels.includes(channel) ||
      !["STDIN", "TERMINAL_RESIZE", "PORT_FORWARD"].includes(channel) ||
      (payload.length === 0) !== endOfInput ||
      (channel === "TERMINAL_RESIZE" && endOfInput) ||
      (channel === "PORT_FORWARD" &&
        (selected.operation !== "kubernetes.port-forward" ||
          !validPortForwardInput(payload, endOfInput))) ||
      !/^[A-Za-z0-9._:-]{8,128}$/u.test(key)
    )
      throw new KubernetesInputProtocolError();
    assertOwnerRequest(signal);
    const payloadDigest = await kubernetesInputDigest(payload);
    assertOwnerRequest(signal);
    const response = await mutate(
      (headers) =>
        submitManagedKubernetesStreamInput({
          path: {
            invocationRef: selected.invocationRef,
            streamRef: selected.streamRef,
          },
          body: {
            channel,
            payloadBase64: base64(payload),
            payloadDigest,
            endOfInput,
          },
          headers: { ...headers, "If-Match": etag(selected.version) },
          signal: requestSignal(signal),
        }),
      selected.version,
      key,
    );
    const result = parseReadback(response.data, selected);
    if (
      !result.submission ||
      result.submission.channel !== channel ||
      result.submission.payloadDigest !== payloadDigest ||
      result.submission.byteCount !== payload.length ||
      result.submission.endOfInput !== endOfInput
    )
      throw new KubernetesInputProtocolError();
    return result;
  } finally {
    payload.fill(0);
  }
}
export async function stopKubernetesStream(
  selected: KubernetesStreamInputView,
  signal: AbortSignal,
  key: string,
): Promise<KubernetesStreamInputView> {
  const response = await mutate(
    (headers) =>
      stopManagedKubernetesStream({
        path: {
          invocationRef: selected.invocationRef,
          streamRef: selected.streamRef,
        },
        body: {},
        headers: { ...headers, "If-Match": etag(selected.version) },
        signal: requestSignal(signal),
      }),
    selected.version,
    key,
  );
  const result = parseReadback({ view: response.data }, selected).view;
  if (result.state === "PREPARED" || result.state === "ACTIVE")
    throw new KubernetesInputProtocolError();
  return result;
}
