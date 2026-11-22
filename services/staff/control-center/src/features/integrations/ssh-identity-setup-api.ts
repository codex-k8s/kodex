import {
  beginSshIdentitySetup,
  cancelSshIdentitySetup,
  getSshIdentitySetup,
  getSshIdentitySetupByBeginKey,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  SshIdentitySetup,
  SshIdentitySetupAdmission,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { assertOwnerRequest } from "@/shared/api/owner-lifetime";

export type SSHSetupContext = Readonly<{
  connectionRef: string;
  connectionVersion: number;
  bindingVersion: number;
  policy: "INITIATOR_REQUIRED" | "PROJECT_SHARED";
}>;
export type SSHUploadCapability = Extract<
  SshIdentitySetupAdmission,
  { reconcileOnly: false }
>["upload"];

const invalidSSHSetupResponse = "SSH identity setup response is invalid";
const invalidSSHUploadResponse = "SSH identity upload requires owner readback";
const setupReference = /^ssu_[a-f0-9]{32}$/;
const reference = /^[A-Za-z0-9_-]{8,96}$/;
const reasons = new Set([
  "PREPARED",
  "ATTEMPTING",
  "MATERIAL_UNKNOWN",
  "MISSING_COMPLETION",
  "ACK_REJECTED",
  "COMPLETED",
  "CANCELLED",
  "EXPIRED",
  "STALE_OWNER",
  "MATERIAL_RECORDED_NO_ATTACHMENT",
  "QUARANTINED",
  "SETUP_EXPIRED",
  "SETUP_EXPIRED_WITH_MATERIAL_UNRESOLVED",
  "OWNER_REVOKED",
  "OWNER_REPLACED",
]);
const states = new Set([
  "PREPARED",
  "ATTEMPTING",
  "UNKNOWN",
  "UNREPORTED",
  "ACK_REJECTED",
  "COMPLETED",
  "CANCELLED",
  "EXPIRED",
  "QUARANTINED",
]);

function safeVersion(value: number): boolean {
  return Number.isSafeInteger(value) && value > 0;
}

function knownSSHPolicy(value: unknown): boolean {
  return value === "INITIATOR_REQUIRED" || value === "PROJECT_SHARED";
}

// Safe metadata проверяется до применения к UI; успешный HTTP не доказывает attachment или SSH health.
export function checkedSSHSetup(
  value: SshIdentitySetup | undefined,
  connectionRef: string,
  setupRef?: string,
): SshIdentitySetup {
  if (
    !value ||
    Object.keys(value).sort().join(",") !==
      "bindingGeneration,bindingVersion,connectionRef,connectionVersion,expiresAt,identityRef,identityRevision,policy,reasonCode,ref,state,targetRef,version" ||
    !setupReference.test(value.ref) ||
    value.connectionRef !== connectionRef ||
    (setupRef !== undefined && value.ref !== setupRef) ||
    !reference.test(value.identityRef) ||
    !reference.test(value.targetRef) ||
    ![value.version, value.connectionVersion, value.identityRevision].every(
      safeVersion,
    ) ||
    !knownSSHPolicy(value.policy) ||
    !states.has(value.state) ||
    !reasons.has(value.reasonCode) ||
    typeof value.expiresAt !== "string" ||
    !Number.isFinite(Date.parse(value.expiresAt))
  )
    throw new Error(invalidSSHSetupResponse);
  if (value.policy === "INITIATOR_REQUIRED") {
    if (
      !safeVersion(value.bindingVersion) ||
      !safeVersion(value.bindingGeneration)
    )
      throw new Error(invalidSSHSetupResponse);
  } else if (value.bindingVersion !== 0 || value.bindingGeneration !== 0)
    throw new Error(invalidSSHSetupResponse);
  return value;
}

function checkedContext(context: SSHSetupContext): void {
  if (
    !reference.test(context.connectionRef) ||
    !safeVersion(context.connectionVersion) ||
    !Number.isSafeInteger(context.bindingVersion) ||
    context.bindingVersion < 0 ||
    !["INITIATOR_REQUIRED", "PROJECT_SHARED"].includes(context.policy)
  )
    throw new Error("SSH identity setup context is invalid");
}

// Только server-returned capability; browser TLS проверяет тот же hostname, отдельный SNI override отсутствует.
export function checkedSSHUploadCapability(
  value: SSHUploadCapability | undefined,
): SSHUploadCapability {
  if (
    !value ||
    Object.keys(value).sort().join(",") !== "origin,permit,tlsServerName" ||
    typeof value.origin !== "string" ||
    value.origin.length > 512 ||
    typeof value.tlsServerName !== "string" ||
    !/^[A-Za-z0-9_-]{43}$/.test(value.permit)
  )
    throw new Error(invalidSSHSetupResponse);
  let url: URL;
  try {
    url = new URL(value.origin);
  } catch {
    throw new Error(invalidSSHSetupResponse);
  }
  const host = url.hostname.startsWith("[")
    ? url.hostname.slice(1, -1)
    : url.hostname;
  if (
    url.protocol !== "https:" ||
    url.origin !== value.origin ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash ||
    host !== value.tlsServerName ||
    value.origin.endsWith(":")
  )
    throw new Error(invalidSSHSetupResponse);
  return value;
}

export async function beginSSHSetup(
  context: SSHSetupContext,
  contentSHA256: string,
  contentBytes: number,
  key: string,
  signal: AbortSignal,
): Promise<SshIdentitySetupAdmission> {
  checkedContext(context);
  if (
    !/^[a-f0-9]{64}$/.test(contentSHA256) ||
    !Number.isSafeInteger(contentBytes) ||
    contentBytes < 1 ||
    contentBytes > 16384
  )
    throw new Error("SSH identity material bounds are invalid");
  const version =
    context.policy === "PROJECT_SHARED"
      ? context.connectionVersion
      : context.bindingVersion;
  const result = await mutate(
    (headers) =>
      beginSshIdentitySetup({
        path: { connectionRef: context.connectionRef },
        headers: { ...headers, "If-Match": `"${String(version)}"` },
        body: {
          expectedConnectionVersion: context.connectionVersion,
          contentSHA256,
          contentBytes,
        },
        signal: requestSignal(signal),
      }),
    undefined,
    key,
  );
  assertOwnerRequest(signal);
  const value = result.data;
  const view = checkedSSHSetup(value.setup, context.connectionRef);
  if (
    view.connectionVersion !== context.connectionVersion ||
    view.policy !== context.policy ||
    typeof value.reconcileOnly !== "boolean"
  )
    throw new Error(invalidSSHSetupResponse);
  if (value.reconcileOnly) {
    if (Object.keys(value).sort().join(",") !== "reconcileOnly,setup")
      throw new Error(invalidSSHSetupResponse);
  } else {
    if (
      Object.keys(value).sort().join(",") !== "reconcileOnly,setup,upload" ||
      view.state !== "PREPARED" ||
      view.version !== 1
    )
      throw new Error(invalidSSHSetupResponse);
    checkedSSHUploadCapability(value.upload);
  }
  return value;
}

export async function readSSHSetup(
  connectionRef: string,
  setupRef: string,
  signal: AbortSignal,
): Promise<SshIdentitySetup> {
  const result = await unwrap(
    getSshIdentitySetup({
      path: { connectionRef, setupRef },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  return checkedSSHSetup(result.data, connectionRef, setupRef);
}

export async function recoverSSHSetup(
  connectionRef: string,
  key: string,
  signal: AbortSignal,
): Promise<SshIdentitySetup> {
  const result = await unwrap(
    getSshIdentitySetupByBeginKey({
      path: { connectionRef },
      query: { beginIdempotencyKey: key },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  return checkedSSHSetup(result.data, connectionRef);
}

export async function cancelSSHSetup(
  value: SshIdentitySetup,
  signal: AbortSignal,
): Promise<SshIdentitySetup> {
  const result = await mutate(
    (headers) =>
      cancelSshIdentitySetup({
        path: { connectionRef: value.connectionRef, setupRef: value.ref },
        headers: { ...headers, "If-Match": etag(value.version) },
        body: {},
        signal: requestSignal(signal),
      }),
    value.version,
  );
  assertOwnerRequest(signal);
  const changed = checkedSSHSetup(result.data, value.connectionRef, value.ref);
  if (!["CANCELLED", "EXPIRED", "UNREPORTED"].includes(changed.state))
    throw new Error(invalidSSHSetupResponse);
  return changed;
}

// Direct purpose upload — одна попытка без API proxy, Cookie, redirect или повторного POST после неизвестного ответа.
export async function uploadSSHMaterial(
  capability: SSHUploadCapability,
  setupRef: string,
  material: Uint8Array | undefined,
  signal: AbortSignal,
): Promise<void> {
  checkedSSHUploadCapability(capability);
  if (
    !setupReference.test(setupRef) ||
    (material !== undefined &&
      (material.byteLength < 1 || material.byteLength > 16384))
  )
    throw new Error("SSH identity material bounds are invalid");
  const owned = material === undefined ? undefined : new Uint8Array(material);
  try {
    assertOwnerRequest(signal);
    const response = await fetch(
      `${capability.origin}/v1/ssh-identities/${setupRef}/${owned ? "material" : "observe"}`,
      {
        method: "POST",
        mode: "cors",
        credentials: "omit",
        cache: "no-store",
        redirect: "error",
        referrerPolicy: "no-referrer",
        headers: {
          Authorization: `Bearer ${capability.permit}`,
          ...(owned ? { "Content-Type": "application/octet-stream" } : {}),
        },
        body: owned,
        signal: AbortSignal.any([signal, AbortSignal.timeout(30_000)]),
      },
    );
    // Даже корректный reply сообщает лишь receipt; модель всегда заново читает CP owner.
    const reader = response.body?.getReader();
    if (!reader) throw new Error(invalidSSHUploadResponse);
    let length = 0;
    try {
      for (;;) {
        const part = await reader.read();
        if (part.done) break;
        length += part.value.byteLength;
        if (length > 1024) {
          await reader.cancel();
          throw new Error(invalidSSHUploadResponse);
        }
      }
    } finally {
      reader.releaseLock();
    }
    assertOwnerRequest(signal);
    if (![200, 202, 409].includes(response.status))
      throw new Error(invalidSSHUploadResponse);
  } finally {
    owned?.fill(0);
  }
}
