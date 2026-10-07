import type { RuntimeEnvironmentDraft } from "@/shared/api/generated/openapi/types.gen";
export const environmentDraftReauthKey =
  "kodex.runtime-environment.server-draft-reauth";
export function rememberEnvironmentDraft(
  draft: RuntimeEnvironmentDraft,
  storage: Storage,
): void {
  storage.setItem(
    environmentDraftReauthKey,
    JSON.stringify({
      ref: draft.ref,
      version: draft.version,
      projectRef: draft.projectRef,
      environmentRef: draft.environmentRef ?? "",
      expiresAt: Date.now() + 5 * 60_000,
    }),
  );
}
export function consumeEnvironmentDraftReference(
  projectRef: string,
  environmentRef: string | undefined,
  storage: Storage,
): { ref: string; version: number } | undefined {
  const source = storage.getItem(environmentDraftReauthKey);
  storage.removeItem(environmentDraftReauthKey);
  if (!source) return;
  const value: unknown = JSON.parse(source);
  if (
    !value ||
    typeof value !== "object" ||
    !("ref" in value) ||
    typeof value.ref !== "string" ||
    !/^[A-Za-z0-9_-]{8,128}$/.test(value.ref) ||
    !("version" in value) ||
    typeof value.version !== "number" ||
    !Number.isSafeInteger(value.version) ||
    value.version <= 0 ||
    !("projectRef" in value) ||
    value.projectRef !== projectRef ||
    !("environmentRef" in value) ||
    value.environmentRef !== (environmentRef ?? "") ||
    !("expiresAt" in value) ||
    typeof value.expiresAt !== "number" ||
    !Number.isFinite(value.expiresAt) ||
    value.expiresAt <= Date.now() ||
    value.expiresAt > Date.now() + 5 * 60_000
  )
    throw new Error("Invalid environment draft reauthentication reference");
  return { ref: value.ref, version: value.version };
}
