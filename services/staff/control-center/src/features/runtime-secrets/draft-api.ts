import {
  assertActiveRuntimeResourceIdentity,
  runtimeResourceOwnerBoundary,
} from "@/features/runtime/active-resource-owner";
import { requestSignal } from "@/shared/api/client";
import {
  createRuntimeSecretDraft,
  createSystemRuntimeSecretDraft,
  saveRuntimeSecretDraft,
  getRuntimeSecretDraft,
  validateRuntimeSecretDraft,
  discardRuntimeSecretDraft,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RuntimeSecretDraft,
  RuntimeSecretCreateInput,
  RuntimeSecretRotateInput,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { AppProblem, asProblem, unwrap } from "@/shared/api/problem";
import {
  runtimeResourceAddressFromIdentity,
  runtimeResourceAddressScope,
  type RuntimeResourceAddress,
} from "@/features/runtime/resource-scope";

export type { RuntimeSecretDraft };

export function safeDraftProblem(error: unknown): AppProblem {
  const problem = asProblem(error);
  return new AppProblem({
    status: problem.status,
    code: problem.code,
    kind: problem.kind,
    retryable: problem.retryable,
  });
}

export function checkedDraft(
  draft: RuntimeSecretDraft | null | undefined,
  scope: RuntimeResourceAddress,
  expected?: { ref?: string; secretRef?: string },
): RuntimeSecretDraft {
  if (
    !draft ||
    !draft.ref ||
    !draft.secretRef ||
    (expected?.ref && draft.ref !== expected.ref) ||
    (expected?.secretRef && draft.secretRef !== expected.secretRef) ||
    ![draft.version, draft.generation, draft.secretVersion].every(
      (value) => Number.isSafeInteger(value) && value > 0,
    ) ||
    !Number.isSafeInteger(draft.publishedRevision) ||
    draft.publishedRevision < 0 ||
    !["STRING", "JSON", "BINARY"].includes(draft.valueType) ||
    ![
      "PREPARING",
      "DRAFT",
      "VALID",
      "PUBLISHING",
      "PUBLISHED",
      "DISCARDED",
      "EXPIRED",
      "FAILED",
    ].includes(draft.state) ||
    ![draft.createdAt, draft.updatedAt, draft.expiresAt].every(
      (value) =>
        typeof value === "string" && Number.isFinite(Date.parse(value)),
    ) ||
    typeof draft.name !== "string" ||
    typeof draft.description !== "string"
  )
    throw new Error("Runtime secret draft receipt is invalid");
  assertActiveRuntimeResourceIdentity(scope, draft);
  // В состояние формы попадает только закрытый набор безопасных метаданных.
  return {
    ref: draft.ref,
    version: draft.version,
    generation: draft.generation,
    projectRef: draft.projectRef,
    scopeKind: draft.scopeKind,
    organizationRef: draft.organizationRef,
    secretRef: draft.secretRef,
    secretVersion: draft.secretVersion,
    name: draft.name,
    description: draft.description,
    valueType: draft.valueType,
    state: draft.state,
    publishedRevision: draft.publishedRevision,
    createdAt: draft.createdAt,
    updatedAt: draft.updatedAt,
    expiresAt: draft.expiresAt,
  };
}

function versioned(headers: MutationHeaders) {
  if (!headers["If-Match"]) throw new Error("Draft version is unavailable");
  return { ...headers, "If-Match": headers["If-Match"] };
}

export async function createSecretDraft(
  scope: RuntimeResourceAddress,
  input: RuntimeSecretCreateInput,
  key: string,
): Promise<RuntimeSecretDraft> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const resolved = runtimeResourceAddressScope(scope);
  if (resolved.kind === "ORGANIZATION") {
    const result = await mutate(
      (headers) =>
        createSystemRuntimeSecretDraft({
          body: input,
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
          },
          signal: requestSignal(),
        }),
      undefined,
      key,
    );
    owner.assert(result.data);
    return checkedDraft(result.data, scope);
  }
  const projectRef = resolved.projectRef;
  const result = await mutate(
    (headers) =>
      createRuntimeSecretDraft({
        path: { projectRef },
        body: input,
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    undefined,
    key,
  );
  owner.assert(result.data);
  return checkedDraft(result.data, projectRef);
}

export async function saveSecretDraft(
  scope: RuntimeResourceAddress,
  secret: { ref: string; version: number },
  input: RuntimeSecretRotateInput,
  key: string,
): Promise<RuntimeSecretDraft> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const result = await mutate(
    (headers) =>
      saveRuntimeSecretDraft({
        path: { secretRef: secret.ref },
        body: input,
        headers: versioned(headers),
        signal: requestSignal(),
      }),
    secret.version,
    key,
  );
  owner.assert(result.data);
  return checkedDraft(result.data, scope, { secretRef: secret.ref });
}

export async function readSecretDraft(
  scope: RuntimeResourceAddress,
  draftRef: string,
  signal: AbortSignal,
): Promise<RuntimeSecretDraft> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const result = await unwrap(
    getRuntimeSecretDraft({
      path: { draftRef },
      signal: requestSignal(signal),
      cache: "no-store",
    }),
  );
  owner.assert(result.data);
  return checkedDraft(result.data, scope, { ref: draftRef });
}

export async function changeSecretDraft(
  draft: RuntimeSecretDraft,
  action: "validate" | "discard",
  key: string,
): Promise<RuntimeSecretDraft> {
  const owner = runtimeResourceOwnerBoundary(
    runtimeResourceAddressFromIdentity(draft),
  );
  owner.assert(draft);
  const operation =
    action === "validate"
      ? validateRuntimeSecretDraft
      : discardRuntimeSecretDraft;
  const result = await mutate(
    (headers) =>
      operation({
        path: { draftRef: draft.ref },
        headers: versioned(headers),
        signal: requestSignal(),
      }),
    draft.version,
    key,
  );
  owner.assert(result.data);
  return checkedDraft(
    result.data,
    runtimeResourceAddressFromIdentity(draft),
    draft,
  );
}
