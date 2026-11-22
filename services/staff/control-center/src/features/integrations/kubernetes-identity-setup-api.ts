import {
  beginManagedKubernetesIdentitySetup,
  cancelManagedKubernetesIdentitySetup,
  getManagedKubernetesIdentitySetup,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  KubernetesIdentitySetupAdmission,
  KubernetesIdentitySetupView,
  KubernetesIdentitySetupRequirements,
  PersonalIntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { personalCredentialDelivery } from "./credential-delivery";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { isRecord } from "@/shared/lib/exact-object";

export interface KubernetesSetupContext {
  connectionRef: string;
  connectionVersion: number;
  expectedVersion: number;
  policy: "INITIATOR_REQUIRED" | "PROJECT_SHARED";
  available: boolean;
  enabled: boolean;
  requirements?: KubernetesIdentitySetupRequirements;
}
export type KubernetesSetupSelector =
  | { setupRef: string }
  | { beginIdempotencyKey: string };
export function isPersonalKubernetesSetup(
  connection: PersonalIntegrationConnection,
): boolean {
  const delivery = personalCredentialDelivery(connection);
  return (
    connection.policy === "INITIATOR_REQUIRED" &&
    delivery?.kind === "BROKER_IDENTITY" &&
    delivery.brokerPurpose === "KUBERNETES_TARGET"
  );
}
const invalid = "Kubernetes identity setup response is invalid";
const refPattern = /^[A-Za-z0-9_-]{8,96}$/;
const digestPattern = /^[a-f0-9]{64}$/;
const states = new Set([
  "PREPARED",
  "ATTEMPTING",
  "COMPLETED",
  "CANCELLED",
  "EXPIRED",
  "UNKNOWN",
  "ACK_REJECTED",
]);
const reasons = new Set([
  "",
  "SETUP_EXPIRED_PENDING_CLOSURE",
  "SETUP_COMPLETION_REQUIRED",
  "SETUP_ACK_REJECTED",
  "SETUP_ACK_CONFLICT",
]);
export function sameKubernetesSetupIdentity(
  a: KubernetesIdentitySetupView,
  b: KubernetesIdentitySetupView,
): boolean {
  return (
    [
      "setupRef",
      "connectionRef",
      "connectionVersion",
      "bindingGeneration",
      "identityRef",
      "identityRevision",
      "targetRef",
      "expiresAt",
      "credentialPolicy",
      "bindingVersion",
      "canonicalContext",
    ] as const
  ).every((key) => a[key] === b[key]);
}
function natural(value: unknown, zero = false): value is number {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= (zero ? 0 : 1)
  );
}
function exact(
  value: unknown,
  keys: string[],
): value is Record<string, unknown> {
  return (
    isRecord(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}
export function kubernetesSetupContextValid(
  context: KubernetesSetupContext,
): boolean {
  return (
    refPattern.test(context.connectionRef) &&
    natural(context.connectionVersion) &&
    natural(context.expectedVersion, true) &&
    ["INITIATOR_REQUIRED", "PROJECT_SHARED"].includes(context.policy) &&
    (context.policy !== "PROJECT_SHARED" ||
      context.expectedVersion === context.connectionVersion)
  );
}
// Requirements описывают pinned target для локального экспорта; authority остаётся у Begin.
export function kubernetesSetupRequirements(
  context: Pick<
    KubernetesSetupContext,
    "connectionVersion" | "policy" | "requirements"
  >,
): KubernetesIdentitySetupRequirements | undefined {
  const value: unknown = context.requirements;
  if (
    !exact(value, [
      "targetRef",
      "targetRevision",
      "canonicalContext",
      "connectionVersion",
      "credentialPolicy",
    ]) ||
    typeof value.targetRef !== "string" ||
    !refPattern.test(value.targetRef) ||
    !natural(value.targetRevision) ||
    value.connectionVersion !== context.connectionVersion ||
    value.credentialPolicy !== context.policy ||
    !canonicalContext(value.canonicalContext)
  )
    return undefined;
  return value as unknown as KubernetesIdentitySetupRequirements;
}
function canonicalContext(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    new TextEncoder().encode(value).length <= 256 &&
    !/[\p{White_Space}\p{Cc}\uD800-\uDFFF]/u.test(value)
  );
}
// Safe metadata проверяется отдельно от admission: GET никогда не возвращает permit.
export function kubernetesSetupView(
  value: unknown,
  context: KubernetesSetupContext,
): KubernetesIdentitySetupView {
  if (
    !exact(value, [
      "setupRef",
      "connectionRef",
      "connectionVersion",
      "bindingGeneration",
      "identityRef",
      "identityRevision",
      "targetRef",
      "state",
      "expiresAt",
      "reasonCode",
      "version",
      "credentialPolicy",
      "bindingVersion",
      "canonicalContext",
    ]) ||
    !kubernetesSetupContextValid(context) ||
    value.connectionRef !== context.connectionRef ||
    value.connectionVersion !== context.connectionVersion ||
    value.credentialPolicy !== context.policy ||
    typeof value.setupRef !== "string" ||
    !value.setupRef.startsWith("ksu_") ||
    !refPattern.test(value.setupRef) ||
    typeof value.identityRef !== "string" ||
    !refPattern.test(value.identityRef) ||
    typeof value.targetRef !== "string" ||
    !refPattern.test(value.targetRef) ||
    !canonicalContext(value.canonicalContext) ||
    !natural(value.identityRevision) ||
    !natural(value.version) ||
    !natural(value.bindingGeneration, context.policy === "PROJECT_SHARED") ||
    !natural(value.bindingVersion, context.policy === "PROJECT_SHARED") ||
    (context.policy === "PROJECT_SHARED" &&
      (value.bindingGeneration !== 0 || value.bindingVersion !== 0)) ||
    typeof value.state !== "string" ||
    !states.has(value.state) ||
    typeof value.reasonCode !== "string" ||
    !reasons.has(value.reasonCode) ||
    typeof value.expiresAt !== "string" ||
    !Number.isFinite(Date.parse(value.expiresAt))
  )
    throw new Error(invalid);
  return value as unknown as KubernetesIdentitySetupView;
}
function byteString(value: unknown, maximum: number): value is string {
  if (
    typeof value !== "string" ||
    !value ||
    value.length > maximum ||
    value.length % 4 !== 0 ||
    !/^[A-Za-z0-9+/]*={0,2}$/.test(value)
  )
    return false;
  try {
    return btoa(atob(value)) === value;
  } catch {
    return false;
  }
}
// Admission сохраняется в memory до явного скачивания. Никакие поля ответа не
// превращаются в endpoint для browser upload; материал читает только owner CLI.
export function kubernetesSetupAdmission(
  value: unknown,
  context: KubernetesSetupContext,
): KubernetesIdentitySetupAdmission {
  if (!isRecord(value) || typeof value.reconcileOnly !== "boolean")
    throw new Error(invalid);
  const setup = kubernetesSetupView(value.setup, context);
  const requirements = kubernetesSetupRequirements(context);
  if (
    !requirements ||
    setup.targetRef !== requirements.targetRef ||
    setup.canonicalContext !== requirements.canonicalContext
  )
    throw new Error(invalid);
  if (value.reconcileOnly) {
    if (!exact(value, ["setup", "reconcileOnly"])) throw new Error(invalid);
  } else {
    if (
      !exact(value, ["setup", "reconcileOnly", "uploadPermit", "endpoint"]) ||
      setup.state !== "PREPARED" ||
      !byteString(value.uploadPermit, 44) ||
      atob(value.uploadPermit).length !== 32 ||
      !exact(value.endpoint, [
        "serverOrigin",
        "tlsServerName",
        "certificateAuthorityPem",
        "certificateAuthoritySHA256",
        "addressIPs",
      ])
    )
      throw new Error(invalid);
    assertKubernetesUploadEndpoint(value.endpoint);
  }
  return value as unknown as KubernetesIdentitySetupAdmission;
}
export async function beginKubernetesSetup(
  context: KubernetesSetupContext,
  contentSHA256: string,
  contentBytes: number,
  key: string,
  signal: AbortSignal,
) {
  if (
    !kubernetesSetupContextValid(context) ||
    !kubernetesSetupRequirements(context) ||
    !digestPattern.test(contentSHA256) ||
    !natural(contentBytes) ||
    contentBytes > 1 << 20
  )
    throw new Error("Kubernetes setup metadata is invalid");
  const response = await mutate(
    (headers) =>
      beginManagedKubernetesIdentitySetup({
        path: { connectionRef: context.connectionRef },
        headers: { ...headers },
        body: {
          expectedVersion: context.expectedVersion,
          expectedConnectionVersion: context.connectionVersion,
          contentSHA256,
          contentBytes,
        },
        signal: requestSignal(signal),
      }),
    undefined,
    key,
  );
  return kubernetesSetupAdmission(response.data, context);
}
export async function getKubernetesSetup(
  context: KubernetesSetupContext,
  selector: KubernetesSetupSelector,
  signal: AbortSignal,
) {
  if (
    !kubernetesSetupContextValid(context) ||
    !(
      ("setupRef" in selector &&
        exact(selector, ["setupRef"]) &&
        typeof selector.setupRef === "string" &&
        selector.setupRef.startsWith("ksu_") &&
        refPattern.test(selector.setupRef)) ||
      ("beginIdempotencyKey" in selector &&
        exact(selector, ["beginIdempotencyKey"]) &&
        typeof selector.beginIdempotencyKey === "string" &&
        /^[^\r\n\0]{8,128}$/.test(selector.beginIdempotencyKey))
    )
  )
    throw new Error("Kubernetes setup selector is invalid");
  const response = await unwrap(
    getManagedKubernetesIdentitySetup({
      path: { connectionRef: context.connectionRef },
      query: selector,
      signal: requestSignal(signal),
    }),
  );
  const view = kubernetesSetupView(response.data, context);
  if (
    ("setupRef" in selector && view.setupRef !== selector.setupRef) ||
    response.etag !== etag(view.version)
  )
    throw new Error(invalid);
  return view;
}
export async function cancelKubernetesSetup(
  context: KubernetesSetupContext,
  setup: KubernetesIdentitySetupView,
  key: string,
  signal: AbortSignal,
) {
  kubernetesSetupView(setup, context);
  const response = await mutate(
    (headers) =>
      cancelManagedKubernetesIdentitySetup({
        path: {
          connectionRef: context.connectionRef,
          setupRef: setup.setupRef,
        },
        headers: { ...headers, "If-Match": etag(setup.version) },
        body: {},
        signal: requestSignal(signal),
      }),
    setup.version,
    key,
  );
  const view = kubernetesSetupView(response.data, context);
  if (
    !sameKubernetesSetupIdentity(view, setup) ||
    view.version < setup.version ||
    response.etag !== etag(view.version)
  )
    throw new Error(invalid);
  return view;
}

// Проверяет только metadata TLS-допуска; browser не подключается к endpoint.
export function assertKubernetesUploadEndpoint(endpoint: unknown): void {
  if (
    !exact(endpoint, [
      "serverOrigin",
      "tlsServerName",
      "certificateAuthorityPem",
      "certificateAuthoritySHA256",
      "addressIPs",
    ])
  )
    throw new Error(invalid);
  if (
    typeof endpoint.serverOrigin !== "string" ||
    endpoint.serverOrigin.length > 2048 ||
    !/^https:\/\/[^/?#\\\s]+$/.test(endpoint.serverOrigin) ||
    typeof endpoint.tlsServerName !== "string" ||
    !/^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$/.test(
      endpoint.tlsServerName,
    ) ||
    !byteString(endpoint.certificateAuthorityPem, 131072) ||
    typeof endpoint.certificateAuthoritySHA256 !== "string" ||
    !digestPattern.test(endpoint.certificateAuthoritySHA256) ||
    !Array.isArray(endpoint.addressIPs) ||
    !endpoint.addressIPs.length ||
    endpoint.addressIPs.length > 16 ||
    endpoint.addressIPs.some(
      (ip) =>
        typeof ip !== "string" ||
        !ip ||
        ip.length > 45 ||
        !/^[0-9a-fA-F:.]+$/.test(ip),
    )
  )
    throw new Error(invalid);
  let origin: URL;
  try {
    origin = new URL(endpoint.serverOrigin);
  } catch {
    throw new Error(invalid);
  }
  if (
    origin.protocol !== "https:" ||
    origin.username ||
    origin.password ||
    origin.pathname !== "/" ||
    origin.search ||
    origin.hash ||
    endpoint.serverOrigin.endsWith("/")
  )
    throw new Error(invalid);
}
