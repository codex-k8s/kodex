import { requestSignal } from "@/shared/api/client";
import { client } from "@/shared/api/generated/openapi/client.gen";
import type { Problem } from "@/shared/api/generated/openapi/types.gen";
import { csrfToken, etag, idempotencyKey, mutate } from "@/shared/api/mutation";
import { AppProblem, asProblem, unwrap } from "@/shared/api/problem";

import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretPage,
  RuntimeSecretReveal,
  RuntimeSecretRotateInput,
} from "./model";

type ApiErrors = { default: Problem };
type RuntimeSecretPageResponses = { 200: RuntimeSecretPage };
type RuntimeSecretCreateResponses = { 201: RuntimeSecret };
type RuntimeSecretResponses = { 200: RuntimeSecret };
type RuntimeSecretRevealResponses = { 200: RuntimeSecretReveal };

export async function loadRuntimeSecretPage(
  projectRef: string,
  query: string,
  pageToken?: string,
  signal: AbortSignal = requestSignal(),
): Promise<RuntimeSecretPage> {
  return (
    await unwrap(
      client.get<RuntimeSecretPageResponses, ApiErrors>({
        url: "/api/v1/projects/{projectRef}/runtime-secrets",
        path: { projectRef },
        query: {
          pageSize: 40,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
        },
        signal,
      }),
    )
  ).data;
}

export async function createRuntimeSecret(
  projectRef: string,
  input: RuntimeSecretCreateInput,
): Promise<RuntimeSecret> {
  return (
    await mutate((headers) =>
      client.post<RuntimeSecretCreateResponses, ApiErrors>({
        url: "/api/v1/projects/{projectRef}/runtime-secrets",
        path: { projectRef },
        body: input,
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function rotateRuntimeSecret(
  secret: RuntimeSecret,
  input: RuntimeSecretRotateInput,
): Promise<RuntimeSecret> {
  return (
    await mutate(
      (headers) =>
        client.post<RuntimeSecretResponses, ApiErrors>({
          url: "/api/v1/runtime-secrets/{secretRef}/rotations",
          path: { secretRef: secret.ref },
          body: input,
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": headers["Idempotency-Key"],
            "If-Match": headers["If-Match"] ?? etag(secret.version),
            "X-CSRF-Token": headers["X-CSRF-Token"],
          },
          signal: requestSignal(),
        }),
      secret.version,
    )
  ).data;
}

export async function revokeRuntimeSecret(
  secret: RuntimeSecret,
): Promise<RuntimeSecret> {
  return (
    await mutate(
      (headers) =>
        client.delete<RuntimeSecretResponses, ApiErrors>({
          url: "/api/v1/runtime-secrets/{secretRef}",
          path: { secretRef: secret.ref },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "If-Match": headers["If-Match"] ?? etag(secret.version),
            "X-CSRF-Token": headers["X-CSRF-Token"],
          },
          signal: requestSignal(),
        }),
      secret.version,
    )
  ).data;
}

export async function revealRuntimeSecret(
  secretRef: string,
): Promise<RuntimeSecretReveal> {
  const result = await client.post<RuntimeSecretRevealResponses, ApiErrors>({
    url: "/api/v1/runtime-secrets/{secretRef}/reveal",
    path: { secretRef },
    cache: "no-store",
    headers: {
      "Idempotency-Key": idempotencyKey(),
      "X-CSRF-Token": csrfToken(),
    },
    signal: requestSignal(),
  });
  const readback = await unwrap<RuntimeSecretReveal>(Promise.resolve(result));
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
