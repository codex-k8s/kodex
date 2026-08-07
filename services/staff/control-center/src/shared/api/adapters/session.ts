import {
  createClient,
  createConfig,
} from "@/shared/api/generated/openapi/client";
import {
  createOwnerSession,
  deleteOwnerSession,
  listProjects,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { ResourcePage } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap, type ApiReadback } from "@/shared/api/problem";
import { runtimeConfig } from "@/shared/config/runtime";
import { newIdempotencyKey } from "@/shared/lib/identity";
import { csrfToken } from "@/shared/lib/identity";

export async function admitOwnerSession(
  bearer: string,
): Promise<ApiReadback<void>> {
  if (!bearer) throw new Error("OIDC bearer is unavailable");
  const oneUseClient = createClient(
    createConfig({
      auth: () => bearer,
      baseUrl: runtimeConfig().apiBaseUrl,
      credentials: "include",
    }),
  );
  const readback = await unwrap(
    createOwnerSession({
      client: oneUseClient,
      headers: { "Idempotency-Key": newIdempotencyKey() },
      signal: requestSignal(),
    }),
  );
  if (!readback.etag) throw new Error("Owner session response ETag is missing");
  return readback;
}

export async function probeOwnerSession(): Promise<ResourcePage> {
  return (
    await unwrap(
      listProjects({ query: { pageSize: 1 }, signal: requestSignal() }),
    )
  ).data;
}

export async function revokeOwnerSession(sessionEtag: string): Promise<void> {
  await unwrap(
    deleteOwnerSession({
      headers: {
        "X-CSRF-Token": csrfToken(),
        "Idempotency-Key": newIdempotencyKey(),
        "If-Match": sessionEtag,
      },
      signal: requestSignal(),
    }),
  );
}
