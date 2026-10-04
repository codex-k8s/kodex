import {
  assertActiveRuntimeResourceIdentity,
  runtimeResourceOwnerBoundary,
} from "@/features/runtime/active-resource-owner";
import { requestSignal } from "@/shared/api/client";
import {
  createRuntimeSecret as createRuntimeSecretRequest,
  listRuntimeSecrets,
  listSystemRuntimeSecrets,
  getRuntimeSecret,
  revealRuntimeSecret as revealRuntimeSecretRequest,
  revokeRuntimeSecret as revokeRuntimeSecretRequest,
  rotateRuntimeSecret as rotateRuntimeSecretRequest,
} from "@/shared/api/generated/openapi/sdk.gen";
import {
  csrfToken,
  idempotencyKey,
  mutate,
  type MutationHeaders,
} from "@/shared/api/mutation";
import { AppProblem, asProblem, unwrap } from "@/shared/api/problem";

import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretPage,
  RuntimeSecretReveal,
  RuntimeSecretRotateInput,
} from "./model";
import { normalizeSecretPage } from "./model";
import {
  runtimeResourceAddressScope,
  runtimeResourceAddressFromIdentity,
  type RuntimeResourceAddress,
} from "@/features/runtime/resource-scope";

export async function readRuntimeSecret(
  secretRef: string,
  scope: RuntimeResourceAddress,
  signal: AbortSignal,
): Promise<RuntimeSecret> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const result = (
    await unwrap(
      getRuntimeSecret({
        path: { secretRef },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  const secret = normalizeSecretPage({ items: [result] }).items[0];
  if (!secret || secret.ref !== secretRef)
    throw new Error("Runtime secret link scope mismatch");
  assertActiveRuntimeResourceIdentity(scope, secret);
  owner.assert(secret);
  return secret;
}

function mutationHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
} {
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

function versionedHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "If-Match": string;
  "X-CSRF-Token": string;
} {
  if (!headers["If-Match"])
    throw new Error("Runtime secret version header is unavailable");
  return { ...mutationHeaders(headers), "If-Match": headers["If-Match"] };
}

export async function loadRuntimeSecretPage(
  scope: RuntimeResourceAddress,
  query: string,
  pageToken?: string,
  signal: AbortSignal = requestSignal(),
  pageSize = 20,
): Promise<RuntimeSecretPage> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const resolved = runtimeResourceAddressScope(scope);
  if (resolved.kind === "ORGANIZATION") {
    const page = (
      await unwrap(
        listSystemRuntimeSecrets({
          query: {
            pageSize,
            ...(query.trim() ? { query: query.trim() } : {}),
            ...(pageToken ? { pageToken } : {}),
          },
          signal,
        }),
      )
    ).data;
    owner.assertCurrent();
    for (const item of page.items) owner.assert(item);
    return page;
  }
  const projectRef = resolved.projectRef;
  const page = (
    await unwrap(
      listRuntimeSecrets({
        path: { projectRef },
        query: {
          pageSize,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
        },
        signal,
      }),
    )
  ).data;
  owner.assertCurrent();
  for (const item of page.items) owner.assert(item);
  return page;
}

export async function createRuntimeSecret(
  projectRef: string,
  input: RuntimeSecretCreateInput,
): Promise<RuntimeSecret> {
  const owner = runtimeResourceOwnerBoundary(projectRef);
  const result = (
    await mutate((headers) =>
      createRuntimeSecretRequest({
        path: { projectRef },
        body: input,
        headers: mutationHeaders(headers),
        signal: requestSignal(),
      }),
    )
  ).data;
  owner.assert(result);
  return result;
}

export async function rotateRuntimeSecret(
  secret: RuntimeSecret,
  input: RuntimeSecretRotateInput,
): Promise<RuntimeSecret> {
  const owner = runtimeResourceOwnerBoundary(
    runtimeResourceAddressFromIdentity(secret),
  );
  owner.assert(secret);
  const result = (
    await mutate(
      (headers) =>
        rotateRuntimeSecretRequest({
          path: { secretRef: secret.ref },
          body: input,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      secret.version,
    )
  ).data;
  owner.assert(result);
  return result;
}

export async function revokeRuntimeSecret(
  secret: RuntimeSecret,
): Promise<RuntimeSecret> {
  const owner = runtimeResourceOwnerBoundary(
    runtimeResourceAddressFromIdentity(secret),
  );
  owner.assert(secret);
  const result = (
    await mutate(
      (headers) =>
        revokeRuntimeSecretRequest({
          path: { secretRef: secret.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      secret.version,
    )
  ).data;
  owner.assert(result);
  return result;
}

export async function revealRuntimeSecret(
  secretRef: string,
  scope: RuntimeResourceAddress,
  organizationRef: string,
): Promise<RuntimeSecretReveal> {
  const owner = runtimeResourceOwnerBoundary(scope);
  if (owner.organizationRef !== organizationRef)
    throw new Error("Secret reveal organization anchor mismatch");
  const signal = requestSignal();
  const secret = await readRuntimeSecret(secretRef, scope, signal);
  assertActiveRuntimeResourceIdentity(scope, secret, organizationRef);
  const resolved = runtimeResourceAddressScope(scope);
  const result = await revealRuntimeSecretRequest({
    path: { secretRef },
    cache: "no-store",
    headers: {
      "Idempotency-Key": idempotencyKey(),
      "X-CSRF-Token": csrfToken(),
      ...(resolved.kind === "PROJECT"
        ? { "X-Kodex-Project-ID": resolved.projectRef }
        : {}),
    },
    signal,
  });
  const readback = await unwrap<RuntimeSecretReveal>(Promise.resolve(result));
  try {
    owner.assertCurrent();
  } catch (error) {
    readback.data.value = "";
    throw error;
  }
  if (result.response?.headers.get("Cache-Control") !== "no-store") {
    readback.data.value = "";
    throw new AppProblem({
      status: 502,
      code: "SECRET_REVEAL_CACHE_POLICY_INVALID",
      retryable: false,
      kind: "unavailable",
    });
  }
  return readback.data;
}

export function normalizeRuntimeSecretProblem(error: unknown): AppProblem {
  return asProblem(error);
}
