const opaqueReferencePattern = /^[A-Za-z0-9_-]{8,128}$/;
const challengeReferencePattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const intentLifetimeMs = 5 * 60 * 1000;
const allowedFutureSkewMs = 30 * 1000;

export const runtimeSecretRevealIntentStorageKey =
  "kodex.oidc.runtime-secret-reveal";

export interface RuntimeSecretRevealIntent {
  readonly action: "reveal";
  readonly challengeRef: string;
  readonly issuedAt: number;
  readonly kind: "runtime-secret";
  readonly projectRef: string;
  readonly returnPath: string;
  readonly secretRef: string;
  readonly version: 1;
}

export type OidcIntent = { readonly kind: "login" } | RuntimeSecretRevealIntent;

function runtimeSecretsPath(projectRef: string): string {
  return `/projects/${encodeURIComponent(projectRef)}/secrets`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(
  value: Record<string, unknown>,
  expected: readonly string[],
): boolean {
  const actual = Object.keys(value).sort();
  return (
    actual.length === expected.length &&
    expected.every((key, index) => actual[index] === key)
  );
}

function sameIntent(
  left: RuntimeSecretRevealIntent,
  right: RuntimeSecretRevealIntent,
): boolean {
  return (
    left.challengeRef === right.challengeRef &&
    left.issuedAt === right.issuedAt &&
    left.projectRef === right.projectRef &&
    left.returnPath === right.returnPath &&
    left.secretRef === right.secretRef
  );
}

export function createRuntimeSecretRevealIntent(
  projectRef: string,
  secretRef: string,
  now = Date.now(),
): RuntimeSecretRevealIntent {
  if (!opaqueReferencePattern.test(projectRef))
    throw new Error("OIDC re-auth project reference is invalid");
  if (!opaqueReferencePattern.test(secretRef))
    throw new Error("OIDC re-auth secret reference is invalid");
  return {
    action: "reveal",
    challengeRef: globalThis.crypto.randomUUID(),
    issuedAt: now,
    kind: "runtime-secret",
    projectRef,
    returnPath: runtimeSecretsPath(projectRef),
    secretRef,
    version: 1,
  };
}

export function parseRuntimeSecretRevealIntent(
  value: unknown,
  now = Date.now(),
): RuntimeSecretRevealIntent {
  const expectedKeys = [
    "action",
    "challengeRef",
    "issuedAt",
    "kind",
    "projectRef",
    "returnPath",
    "secretRef",
    "version",
  ] as const;
  if (!isRecord(value) || !hasExactKeys(value, expectedKeys))
    throw new Error("OIDC re-auth state shape is invalid");
  if (
    value.action !== "reveal" ||
    value.kind !== "runtime-secret" ||
    value.version !== 1 ||
    typeof value.challengeRef !== "string" ||
    !challengeReferencePattern.test(value.challengeRef) ||
    typeof value.issuedAt !== "number" ||
    !Number.isSafeInteger(value.issuedAt) ||
    value.issuedAt > now + allowedFutureSkewMs ||
    now - value.issuedAt > intentLifetimeMs ||
    typeof value.projectRef !== "string" ||
    !opaqueReferencePattern.test(value.projectRef) ||
    typeof value.secretRef !== "string" ||
    !opaqueReferencePattern.test(value.secretRef) ||
    typeof value.returnPath !== "string" ||
    value.returnPath !== runtimeSecretsPath(value.projectRef)
  )
    throw new Error("OIDC re-auth state is invalid or expired");
  return value as unknown as RuntimeSecretRevealIntent;
}

export function consumeOidcIntent(
  value: unknown,
  storage: Pick<Storage, "getItem" | "removeItem">,
  now = Date.now(),
): OidcIntent {
  const pendingRaw = storage.getItem(runtimeSecretRevealIntentStorageKey);
  if (value === undefined || value === null) {
    if (pendingRaw !== null) {
      storage.removeItem(runtimeSecretRevealIntentStorageKey);
      throw new Error("OIDC callback intent does not match pending re-auth");
    }
    return { kind: "login" };
  }

  storage.removeItem(runtimeSecretRevealIntentStorageKey);
  if (pendingRaw === null)
    throw new Error("OIDC re-auth state is missing or already consumed");

  let pending: unknown;
  try {
    pending = JSON.parse(pendingRaw);
  } catch {
    throw new Error("Pending OIDC re-auth state is invalid");
  }
  const returnedIntent = parseRuntimeSecretRevealIntent(value, now);
  const pendingIntent = parseRuntimeSecretRevealIntent(pending, now);
  if (!sameIntent(returnedIntent, pendingIntent))
    throw new Error("OIDC re-auth state does not match pending operation");
  return returnedIntent;
}
