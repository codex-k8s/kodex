import {
  getManagedKubernetesTool,
  listManagedKubernetesTools,
  stopManagedKubernetesTool,
} from "./generated/openapi/sdk.gen";
import type {
  KubernetesToolView,
  KubernetesToolPage,
} from "./generated/openapi/types.gen";
import { parseKubernetesCaptureSummary } from "./kubernetes-capture";
import { hasExactKeys, isRecord } from "@/shared/lib/exact-object";
import { requestSignal } from "./client";
import { assertOwnerRequest } from "./owner-lifetime";
import { etag, mutate } from "./mutation";
import { unwrap } from "./problem";

export interface KubernetesToolContext {
  runRef: string;
  nodeRef: string;
}
const states = new Set([
  "READY",
  "WAITING_APPROVAL",
  "RUNNING",
  "SUCCEEDED",
  "FAILED",
  "REJECTED",
  "CANCELLED",
  "UNKNOWN_OUTCOME",
]);
const live = new Set(["READY", "WAITING_APPROVAL", "RUNNING"]);
const fields = [
  "invocationRef",
  "runRef",
  "nodeRef",
  "connectionRef",
  "targetRef",
  "targetRevision",
  "operation",
  "state",
  "version",
  "waitingForGate",
  "safeReasonCode",
  "captureStatuses",
];
export class KubernetesToolProtocolError extends Error {
  constructor() {
    super("Kubernetes tool metadata is invalid");
    this.name = "KubernetesToolProtocolError";
  }
}
function ref(value: unknown, prefix: string): value is string {
  return (
    typeof value === "string" &&
    value.startsWith(prefix + "_") &&
    /^[A-Za-z0-9_-]{12,96}$/u.test(value)
  );
}
export function kubernetesToolIsActive(view: KubernetesToolView): boolean {
  return live.has(view.state);
}
export function parseKubernetesTool(
  value: unknown,
  context: KubernetesToolContext,
): KubernetesToolView {
  if (
    !ref(context.runRef, "run") ||
    !ref(context.nodeRef, "nod") ||
    !isRecord(value) ||
    !hasExactKeys(value, fields) ||
    !ref(value.invocationRef, "inv") ||
    value.runRef !== context.runRef ||
    value.nodeRef !== context.nodeRef ||
    !ref(value.connectionRef, "int") ||
    typeof value.targetRef !== "string" ||
    !/^[A-Za-z0-9_-]{1,96}$/u.test(value.targetRef) ||
    !Number.isSafeInteger(value.targetRevision) ||
    (value.targetRevision as number) < 1 ||
    !Number.isSafeInteger(value.version) ||
    (value.version as number) < 1 ||
    typeof value.operation !== "string" ||
    !/^kubernetes\.[a-z][a-z.-]{0,84}$/u.test(value.operation) ||
    typeof value.state !== "string" ||
    !states.has(value.state) ||
    typeof value.waitingForGate !== "boolean" ||
    !["NONE", "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED"].includes(
      value.safeReasonCode as string,
    ) ||
    (value.safeReasonCode === "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED" &&
      (value.operation !== "kubernetes.port-forward" ||
        value.state !== "FAILED")) ||
    (!live.has(value.state) && value.waitingForGate)
  )
    throw new KubernetesToolProtocolError();
  if (
    !Array.isArray(value.captureStatuses) ||
    value.captureStatuses.length > 256
  )
    throw new KubernetesToolProtocolError();
  const captures = value.captureStatuses.map(parseKubernetesCaptureSummary);
  if (new Set(captures.map((item) => item.captureRef)).size !== captures.length)
    throw new KubernetesToolProtocolError();
  return value as unknown as KubernetesToolView;
}
export async function listKubernetesTools(
  context: KubernetesToolContext,
  signal: AbortSignal,
  pageToken = "",
): Promise<KubernetesToolPage> {
  if (
    !ref(context.runRef, "run") ||
    !ref(context.nodeRef, "nod") ||
    (pageToken !== "" && !ref(pageToken, "inv"))
  )
    throw new KubernetesToolProtocolError();
  assertOwnerRequest(signal);
  const response = await unwrap(
    listManagedKubernetesTools({
      path: context,
      query: { pageSize: 20, ...(pageToken ? { pageToken } : {}) },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  const value: unknown = response.data;
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ["items", "nextPageToken"]) ||
    !Array.isArray(value.items) ||
    value.items.length > 20 ||
    typeof value.nextPageToken !== "string" ||
    (value.nextPageToken !== "" && !ref(value.nextPageToken, "inv"))
  )
    throw new KubernetesToolProtocolError();
  const items = value.items.map((item) => parseKubernetesTool(item, context));
  if (new Set(items.map((item) => item.invocationRef)).size !== items.length)
    throw new KubernetesToolProtocolError();
  return { items, nextPageToken: value.nextPageToken };
}
export async function readKubernetesTool(
  selected: KubernetesToolView,
  signal: AbortSignal,
): Promise<KubernetesToolView> {
  assertOwnerRequest(signal);
  const response = await unwrap(
    getManagedKubernetesTool({
      path: { invocationRef: selected.invocationRef },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  const view = parseKubernetesTool(response.data, selected);
  if (view.invocationRef !== selected.invocationRef)
    throw new KubernetesToolProtocolError();
  return view;
}
// Один Stop без автоматического retry. Потерянный ответ сверяется отдельным чтением.
export async function stopKubernetesTool(
  selected: KubernetesToolView,
  signal: AbortSignal,
  key: string,
): Promise<KubernetesToolView> {
  assertOwnerRequest(signal);
  const response = await mutate(
    (headers) =>
      stopManagedKubernetesTool({
        path: { invocationRef: selected.invocationRef },
        body: {},
        headers: { ...headers, "If-Match": etag(selected.version) },
        signal: requestSignal(signal),
      }),
    selected.version,
    key,
  );
  assertOwnerRequest(signal);
  const view = parseKubernetesTool(response.data, selected);
  if (
    view.invocationRef !== selected.invocationRef ||
    kubernetesToolIsActive(view)
  )
    throw new KubernetesToolProtocolError();
  return view;
}
