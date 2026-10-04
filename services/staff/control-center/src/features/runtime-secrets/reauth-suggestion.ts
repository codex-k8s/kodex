import type { RuntimeSecretDraftSuggestion } from "./model";
import {
  parseRuntimeResourceScope,
  runtimeResourceAddressScope,
  runtimeResourceScopeKey,
  type RuntimeResourceAddress,
  type RuntimeResourceScope,
} from "@/features/runtime/resource-scope";

const lifetimeMs = 5 * 60 * 1000;

export const runtimeSecretReauthSuggestionStorageKey =
  "kodex.oidc.runtime-secret-draft-suggestion";

interface StoredRuntimeSecretSuggestion {
  readonly expiresAt: number;
  readonly resourceScope: RuntimeResourceScope;
  readonly suggestion: RuntimeSecretDraftSuggestion;
  readonly surface?: "assistant";
  readonly version: 2;
}

function validSuggestion(
  value: unknown,
): value is RuntimeSecretDraftSuggestion {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    return false;
  const candidate = value as Record<string, unknown>;
  return (
    Object.keys(candidate).sort().join(",") ===
      "description,name,sourceHelp,valueType" &&
    typeof candidate.name === "string" &&
    candidate.name.length <= 120 &&
    typeof candidate.description === "string" &&
    candidate.description.length <= 1000 &&
    typeof candidate.sourceHelp === "string" &&
    candidate.sourceHelp.length <= 1000 &&
    (candidate.valueType === "STRING" ||
      candidate.valueType === "JSON" ||
      candidate.valueType === "BINARY")
  );
}

export function rememberRuntimeSecretReauthSuggestion(
  storage: Pick<Storage, "setItem">,
  projectRef: RuntimeResourceAddress,
  suggestion: RuntimeSecretDraftSuggestion,
  surface?: "assistant",
  now = Date.now(),
): void {
  const resourceScope = runtimeResourceAddressScope(projectRef);
  if (!validSuggestion(suggestion))
    throw new Error("Runtime secret re-auth suggestion is invalid");
  const stored: StoredRuntimeSecretSuggestion = {
    expiresAt: now + lifetimeMs,
    resourceScope,
    suggestion,
    ...(surface ? { surface } : {}),
    version: 2,
  };
  storage.setItem(
    runtimeSecretReauthSuggestionStorageKey,
    JSON.stringify(stored),
  );
}

export function consumeRuntimeSecretReauthSuggestion(
  storage: Pick<Storage, "getItem" | "removeItem">,
  expected: {
    readonly projectRef: RuntimeResourceAddress;
    readonly surface?: "assistant";
  },
  now = Date.now(),
): RuntimeSecretDraftSuggestion | undefined {
  const raw = storage.getItem(runtimeSecretReauthSuggestionStorageKey);
  storage.removeItem(runtimeSecretReauthSuggestionStorageKey);
  if (raw === null) return undefined;
  try {
    const value = JSON.parse(raw) as Record<string, unknown>;
    const keys = Object.keys(value).sort().join(",");
    const expectedKeys = value.surface
      ? "expiresAt,resourceScope,suggestion,surface,version"
      : "expiresAt,resourceScope,suggestion,version";
    if (
      keys !== expectedKeys ||
      value.version !== 2 ||
      typeof value.expiresAt !== "number" ||
      !Number.isSafeInteger(value.expiresAt) ||
      value.expiresAt <= now ||
      value.expiresAt > now + lifetimeMs ||
      runtimeResourceScopeKey(
        parseRuntimeResourceScope(value.resourceScope),
      ) !==
        runtimeResourceScopeKey(
          runtimeResourceAddressScope(expected.projectRef),
        ) ||
      value.surface !== expected.surface ||
      !validSuggestion(value.suggestion)
    )
      return undefined;
    return value.suggestion;
  } catch {
    return undefined;
  }
}
