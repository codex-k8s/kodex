import {
  beginManagedKubernetesResourceMaterial,
  getManagedKubernetesResourceMaterial,
  cancelManagedKubernetesResourceMaterial,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  KubernetesResourceMaterialAdmission,
  KubernetesResourceMaterialBeginInput,
  KubernetesResourceMaterialView,
} from "@/shared/api/generated/openapi/types.gen";
import {
  assertKubernetesUploadEndpoint,
  kubernetesSetupContextValid,
  type KubernetesSetupContext,
} from "./kubernetes-identity-setup-api";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { isRecord } from "@/shared/lib/exact-object";

export type KubernetesMaterialContext = KubernetesSetupContext;
export type KubernetesMaterialMetadata = Omit<
  KubernetesResourceMaterialBeginInput,
  "expectedVersion" | "expectedConnectionVersion"
>;
export type KubernetesMaterialSelector =
  | { materialRef: string }
  | { beginIdempotencyKey: string };
const invalid = "Kubernetes resource material response is invalid";
const reference = /^[A-Za-z0-9_-]{8,96}$/;
const dnsName = /^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$/;
const states = new Set([
  "PREPARED",
  "ATTEMPTING",
  "AVAILABLE",
  "UNKNOWN",
  "ACK_REJECTED",
  "CANCELLED",
  "EXPIRED",
  "CONSUMED",
]);
const reasons = new Set([
  "",
  "MATERIAL_EXPIRED_PENDING_CLOSURE",
  "MATERIAL_COMPLETION_REQUIRED",
  "MATERIAL_ACK_REJECTED",
  "MATERIAL_ACK_CONFLICT",
  "MATERIAL_SOURCE_CHANGED",
  "MATERIAL_CANCELLED",
  "MATERIAL_CONSUMED",
  "MATERIAL_UNAVAILABLE",
  "MATERIAL_OBSERVATION_ABSENT",
]);
function exact(
  value: unknown,
  keys: readonly string[],
): value is Record<string, unknown> {
  return (
    isRecord(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}
function natural(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
export function materialDestinationValid(
  value: Pick<
    KubernetesMaterialMetadata,
    | "operation"
    | "namespace"
    | "name"
    | "generateName"
    | "secretType"
    | "patchType"
  >,
): boolean {
  const namespaced = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(
    value.namespace,
  );
  const name =
    value.name.length <= 253 &&
    dnsName.test(value.name) &&
    value.name
      .split(".")
      .every((part) => /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(part));
  const prefix = value.generateName.replace(/-$/, "a");
  if (
    !namespaced ||
    !value.secretType ||
    new TextEncoder().encode(value.secretType).length > 253 ||
    /[\0\r\n]/.test(value.secretType) ||
    value.secretType === "kubernetes.io/service-account-token"
  )
    return false;
  if (value.operation === "kubernetes.create")
    return (
      !!value.name !== !!value.generateName &&
      (value.name
        ? name
        : value.generateName.length <= 253 &&
          dnsName.test(prefix) &&
          prefix
            .split(".")
            .every((part) => /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(part))) &&
      value.patchType === ""
    );
  return (
    name &&
    value.generateName === "" &&
    (value.operation === "kubernetes.patch"
      ? ["JSON", "MERGE", "STRATEGIC"].includes(value.patchType)
      : ["kubernetes.replace", "kubernetes.apply", "kubernetes.edit"].includes(
          value.operation,
        ) && value.patchType === "")
  );
}
export function kubernetesMaterialView(
  value: unknown,
  context: KubernetesMaterialContext,
): KubernetesResourceMaterialView {
  if (
    !exact(value, [
      "materialRef",
      "connectionRef",
      "connectionVersion",
      "identityRef",
      "identityRevision",
      "targetRef",
      "namespace",
      "name",
      "generateName",
      "operation",
      "secretType",
      "patchType",
      "state",
      "expiresAt",
      "version",
      "reasonCode",
      "credentialPolicy",
      "bindingVersion",
    ]) ||
    !kubernetesSetupContextValid(context) ||
    value.connectionRef !== context.connectionRef ||
    value.connectionVersion !== context.connectionVersion ||
    value.credentialPolicy !== context.policy ||
    typeof value.materialRef !== "string" ||
    !value.materialRef.startsWith("krs_") ||
    !reference.test(value.materialRef) ||
    typeof value.identityRef !== "string" ||
    !reference.test(value.identityRef) ||
    typeof value.targetRef !== "string" ||
    !reference.test(value.targetRef) ||
    !natural(value.identityRevision) ||
    !natural(value.version) ||
    (context.policy === "PROJECT_SHARED"
      ? value.bindingVersion !== 0
      : !natural(value.bindingVersion)) ||
    typeof value.state !== "string" ||
    !states.has(value.state) ||
    typeof value.reasonCode !== "string" ||
    !reasons.has(value.reasonCode) ||
    typeof value.expiresAt !== "string" ||
    !Number.isFinite(Date.parse(value.expiresAt)) ||
    [
      "operation",
      "namespace",
      "name",
      "generateName",
      "secretType",
      "patchType",
    ].some((key) => typeof value[key] !== "string")
  )
    throw new Error(invalid);
  const view = value as unknown as KubernetesResourceMaterialView;
  if (!materialDestinationValid(view)) throw new Error(invalid);
  return view;
}
// Expiry меняется с upload deadline на immutable use deadline после AVAILABLE.
// Это не новый permit; остальные owner pins и destination остаются неизменными.
export function sameKubernetesMaterial(
  a: KubernetesResourceMaterialView,
  b: KubernetesResourceMaterialView,
): boolean {
  return (
    [
      "materialRef",
      "connectionRef",
      "connectionVersion",
      "identityRef",
      "identityRevision",
      "targetRef",
      "namespace",
      "name",
      "generateName",
      "operation",
      "secretType",
      "patchType",
      "credentialPolicy",
      "bindingVersion",
    ] as const
  ).every((key) => a[key] === b[key]);
}
export function kubernetesMaterialAdmission(
  value: unknown,
  context: KubernetesMaterialContext,
): KubernetesResourceMaterialAdmission {
  if (!isRecord(value) || typeof value.reconcileOnly !== "boolean")
    throw new Error(invalid);
  const view = kubernetesMaterialView(value.material, context);
  if (value.reconcileOnly) {
    if (!exact(value, ["material", "reconcileOnly"])) throw new Error(invalid);
  } else {
    if (
      !exact(value, [
        "material",
        "reconcileOnly",
        "uploadPermit",
        "endpoint",
      ]) ||
      view.state !== "PREPARED" ||
      typeof value.uploadPermit !== "string" ||
      !/^[A-Za-z0-9+/]{43}=$/.test(value.uploadPermit)
    )
      throw new Error(invalid);
    try {
      if (
        atob(value.uploadPermit).length !== 32 ||
        btoa(atob(value.uploadPermit)) !== value.uploadPermit
      )
        throw new Error(invalid);
    } catch {
      throw new Error(invalid);
    }
    assertKubernetesUploadEndpoint(value.endpoint);
  }
  return value as unknown as KubernetesResourceMaterialAdmission;
}
export async function beginKubernetesMaterial(
  context: KubernetesMaterialContext,
  input: KubernetesMaterialMetadata,
  key: string,
  signal: AbortSignal,
) {
  if (
    !kubernetesSetupContextValid(context) ||
    !exact(input, [
      "operation",
      "namespace",
      "name",
      "generateName",
      "secretType",
      "patchType",
      "contentSHA256",
      "contentBytes",
    ]) ||
    !materialDestinationValid(input) ||
    !/^[a-f0-9]{64}$/.test(input.contentSHA256) ||
    !natural(input.contentBytes) ||
    input.contentBytes > 1 << 20
  )
    throw new Error("Kubernetes material metadata is invalid");
  const response = await mutate(
    (headers) =>
      beginManagedKubernetesResourceMaterial({
        path: { connectionRef: context.connectionRef },
        headers: { ...headers },
        body: {
          ...input,
          expectedVersion: context.expectedVersion,
          expectedConnectionVersion: context.connectionVersion,
        },
        signal: requestSignal(signal),
      }),
    undefined,
    key,
  );
  const result = kubernetesMaterialAdmission(response.data, context);
  if (
    (
      [
        "operation",
        "namespace",
        "name",
        "generateName",
        "secretType",
        "patchType",
      ] as const
    ).some((field) => result.material[field] !== input[field])
  )
    throw new Error(invalid);
  return result;
}
export async function getKubernetesMaterial(
  context: KubernetesMaterialContext,
  selector: KubernetesMaterialSelector,
  signal: AbortSignal,
) {
  if (
    !kubernetesSetupContextValid(context) ||
    !("materialRef" in selector
      ? exact(selector, ["materialRef"]) &&
        /^krs_[A-Za-z0-9_-]{8,92}$/.test(selector.materialRef)
      : exact(selector, ["beginIdempotencyKey"]) &&
        /^[^\0\r\n]{8,128}$/.test(selector.beginIdempotencyKey))
  )
    throw new Error("Kubernetes material selector is invalid");
  const response = await unwrap(
    getManagedKubernetesResourceMaterial({
      path: { connectionRef: context.connectionRef },
      query: selector,
      signal: requestSignal(signal),
    }),
  );
  const view = kubernetesMaterialView(response.data, context);
  if (
    ("materialRef" in selector && view.materialRef !== selector.materialRef) ||
    response.etag !== etag(view.version)
  )
    throw new Error(invalid);
  return view;
}
export async function cancelKubernetesMaterial(
  context: KubernetesMaterialContext,
  previous: KubernetesResourceMaterialView,
  key: string,
  signal: AbortSignal,
) {
  kubernetesMaterialView(previous, context);
  const response = await mutate(
    (headers) =>
      cancelManagedKubernetesResourceMaterial({
        path: {
          connectionRef: context.connectionRef,
          materialRef: previous.materialRef,
        },
        headers: { ...headers, "If-Match": etag(previous.version) },
        body: {},
        signal: requestSignal(signal),
      }),
    previous.version,
    key,
  );
  const view = kubernetesMaterialView(response.data, context);
  if (
    !sameKubernetesMaterial(previous, view) ||
    view.version < previous.version ||
    response.etag !== etag(view.version)
  )
    throw new Error(invalid);
  return view;
}
