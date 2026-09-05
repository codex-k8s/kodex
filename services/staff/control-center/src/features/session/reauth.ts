const opaqueReferencePattern = /^[A-Za-z0-9_-]{8,128}$/;
const challengeReferencePattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const intentLifetimeMs = 5 * 60 * 1000;
const allowedFutureSkewMs = 30 * 1000;

export const oidcReauthIntentStorageKey = "kodex.oidc.reauth-intent";
export const runtimeEnvironmentPolicyReauthCompletionStorageKey =
  "kodex.oidc.runtime-environment-policy-completion";

interface ReauthIntentBase {
  readonly challengeRef: string;
  readonly issuedAt: number;
  readonly projectRef: string;
  readonly returnPath: string;
  readonly version: 1;
}

export interface RuntimeSecretRevealIntent extends ReauthIntentBase {
  readonly action: "reveal";
  readonly kind: "runtime-secret";
  readonly secretRef: string;
}

export type RuntimeEnvironmentPolicyOperation = "CREATE" | "PUBLISH";

export interface RuntimeEnvironmentPolicyIntent extends ReauthIntentBase {
  readonly environmentRef?: string;
  readonly kind: "runtime-environment-policy";
  readonly operation: RuntimeEnvironmentPolicyOperation;
}
export interface EmailReconciliationIntent extends Omit<
  ReauthIntentBase,
  "projectRef"
> {
  readonly kind: "email-reconciliation";
  readonly receiptRef: string;
  readonly receiptVersion: number;
  readonly receiptDigest: string;
  readonly connectionRef: string;
  readonly invocationRef: string;
}
export function emailReconciliationPath(
  connectionRef: string,
  invocationRef: string,
): string {
  return `/integrations?${new URLSearchParams({ connectionRef, invocationRef }).toString()}`;
}
export function createEmailReconciliationIntent(
  input: Pick<
    EmailReconciliationIntent,
    | "receiptRef"
    | "receiptVersion"
    | "receiptDigest"
    | "connectionRef"
    | "invocationRef"
  >,
  now = Date.now(),
): EmailReconciliationIntent {
  return parseEmailReconciliationIntent(
    {
      ...input,
      kind: "email-reconciliation",
      version: 1,
      challengeRef: crypto.randomUUID(),
      issuedAt: now,
      returnPath: emailReconciliationPath(
        input.connectionRef,
        input.invocationRef,
      ),
    },
    now,
  );
}
export function parseEmailReconciliationIntent(
  value: unknown,
  now = Date.now(),
): EmailReconciliationIntent {
  const keys = [
    "kind",
    "version",
    "challengeRef",
    "issuedAt",
    "returnPath",
    "receiptRef",
    "receiptVersion",
    "receiptDigest",
    "connectionRef",
    "invocationRef",
  ];
  if (
    !isRecord(value) ||
    !hasExactKeys(value, keys) ||
    value.kind !== "email-reconciliation" ||
    value.version !== 1 ||
    typeof value.challengeRef !== "string" ||
    !challengeReferencePattern.test(value.challengeRef) ||
    typeof value.issuedAt !== "number" ||
    !Number.isSafeInteger(value.issuedAt) ||
    value.issuedAt > now + allowedFutureSkewMs ||
    now - value.issuedAt > intentLifetimeMs ||
    typeof value.receiptRef !== "string" ||
    !opaqueReferencePattern.test(value.receiptRef) ||
    typeof value.receiptVersion !== "number" ||
    !Number.isSafeInteger(value.receiptVersion) ||
    value.receiptVersion < 1 ||
    typeof value.receiptDigest !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.receiptDigest) ||
    typeof value.connectionRef !== "string" ||
    !opaqueReferencePattern.test(value.connectionRef) ||
    typeof value.invocationRef !== "string" ||
    !opaqueReferencePattern.test(value.invocationRef) ||
    value.returnPath !==
      emailReconciliationPath(value.connectionRef, value.invocationRef)
  )
    throw new Error("Invalid email reconciliation OIDC intent");
  return value as unknown as EmailReconciliationIntent;
}

export type ReauthIntent =
  | RuntimeSecretRevealIntent
  | RuntimeEnvironmentPolicyIntent
  | EmailReconciliationIntent;
export type OidcIntent = { readonly kind: "login" } | ReauthIntent;

interface RuntimeEnvironmentPolicyReauthCompletion {
  readonly challengeRef: string;
  readonly environmentRef?: string;
  readonly expiresAt: number;
  readonly kind: "runtime-environment-policy";
  readonly operation: RuntimeEnvironmentPolicyOperation;
  readonly projectRef: string;
  readonly returnPath: string;
  readonly version: 1;
}

function runtimeSecretsPath(projectRef: string): string {
  return `/projects/${encodeURIComponent(projectRef)}/secrets`;
}

export function runtimeEnvironmentPolicyPath(
  projectRef: string,
  environmentRef?: string,
): string {
  const projectPath = `/projects/${encodeURIComponent(projectRef)}/environments`;
  return environmentRef
    ? `${projectPath}/${encodeURIComponent(environmentRef)}`
    : `${projectPath}/new`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(
  value: Record<string, unknown>,
  expected: readonly string[],
): boolean {
  const actual = Object.keys(value).sort();
  const sortedExpected = [...expected].sort();
  return (
    actual.length === sortedExpected.length &&
    sortedExpected.every((key, index) => actual[index] === key)
  );
}

function validBase(
  value: Record<string, unknown>,
  now: number,
): value is Record<string, unknown> & ReauthIntentBase {
  return (
    value.version === 1 &&
    typeof value.challengeRef === "string" &&
    challengeReferencePattern.test(value.challengeRef) &&
    typeof value.issuedAt === "number" &&
    Number.isSafeInteger(value.issuedAt) &&
    value.issuedAt <= now + allowedFutureSkewMs &&
    now - value.issuedAt <= intentLifetimeMs &&
    typeof value.projectRef === "string" &&
    opaqueReferencePattern.test(value.projectRef) &&
    typeof value.returnPath === "string"
  );
}

function sameIntent(left: ReauthIntent, right: ReauthIntent): boolean {
  if (left.kind !== right.kind) return false;
  if (
    left.challengeRef !== right.challengeRef ||
    left.issuedAt !== right.issuedAt ||
    left.returnPath !== right.returnPath
  )
    return false;
  if (left.kind === "runtime-secret" && right.kind === "runtime-secret")
    return (
      left.secretRef === right.secretRef && left.projectRef === right.projectRef
    );
  if (
    left.kind === "runtime-environment-policy" &&
    right.kind === "runtime-environment-policy"
  )
    return (
      left.environmentRef === right.environmentRef &&
      left.projectRef === right.projectRef &&
      left.operation === right.operation
    );
  if (
    left.kind === "email-reconciliation" &&
    right.kind === "email-reconciliation"
  )
    return (
      left.receiptRef === right.receiptRef &&
      left.receiptVersion === right.receiptVersion &&
      left.receiptDigest === right.receiptDigest &&
      left.connectionRef === right.connectionRef &&
      left.invocationRef === right.invocationRef
    );
  return false;
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

export function createRuntimeEnvironmentPolicyIntent(
  projectRef: string,
  operation: RuntimeEnvironmentPolicyOperation,
  environmentRef?: string,
  now = Date.now(),
): RuntimeEnvironmentPolicyIntent {
  if (!opaqueReferencePattern.test(projectRef))
    throw new Error("OIDC re-auth project reference is invalid");
  if (
    (operation === "CREATE" && environmentRef !== undefined) ||
    (operation === "PUBLISH" &&
      (environmentRef === undefined ||
        !opaqueReferencePattern.test(environmentRef)))
  )
    throw new Error("OIDC re-auth environment operation is invalid");
  return {
    challengeRef: globalThis.crypto.randomUUID(),
    ...(environmentRef ? { environmentRef } : {}),
    issuedAt: now,
    kind: "runtime-environment-policy",
    operation,
    projectRef,
    returnPath: runtimeEnvironmentPolicyPath(projectRef, environmentRef),
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
    !validBase(value, now) ||
    value.action !== "reveal" ||
    value.kind !== "runtime-secret" ||
    typeof value.secretRef !== "string" ||
    !opaqueReferencePattern.test(value.secretRef) ||
    value.returnPath !== runtimeSecretsPath(value.projectRef)
  )
    throw new Error("OIDC re-auth state is invalid or expired");
  return value as unknown as RuntimeSecretRevealIntent;
}

export function parseRuntimeEnvironmentPolicyIntent(
  value: unknown,
  now = Date.now(),
): RuntimeEnvironmentPolicyIntent {
  if (!isRecord(value)) throw new Error("OIDC re-auth state shape is invalid");
  const hasEnvironment = Object.hasOwn(value, "environmentRef");
  const expectedKeys = [
    "challengeRef",
    ...(hasEnvironment ? ["environmentRef"] : []),
    "issuedAt",
    "kind",
    "operation",
    "projectRef",
    "returnPath",
    "version",
  ] as const;
  if (!hasExactKeys(value, expectedKeys))
    throw new Error("OIDC re-auth state shape is invalid");
  const operation = value.operation;
  const environmentRef = value.environmentRef;
  if (
    !validBase(value, now) ||
    value.kind !== "runtime-environment-policy" ||
    (operation !== "CREATE" && operation !== "PUBLISH") ||
    (operation === "CREATE" && hasEnvironment) ||
    (operation === "PUBLISH" &&
      (typeof environmentRef !== "string" ||
        !opaqueReferencePattern.test(environmentRef))) ||
    value.returnPath !==
      runtimeEnvironmentPolicyPath(
        value.projectRef,
        operation === "PUBLISH" ? String(environmentRef) : undefined,
      )
  )
    throw new Error("OIDC re-auth state is invalid or expired");
  return value as unknown as RuntimeEnvironmentPolicyIntent;
}

function parseReauthIntent(value: unknown, now: number): ReauthIntent {
  if (!isRecord(value)) throw new Error("OIDC re-auth state shape is invalid");
  if (value.kind === "runtime-secret")
    return parseRuntimeSecretRevealIntent(value, now);
  if (value.kind === "runtime-environment-policy")
    return parseRuntimeEnvironmentPolicyIntent(value, now);
  if (value.kind === "email-reconciliation")
    return parseEmailReconciliationIntent(value, now);
  throw new Error("OIDC re-auth state kind is invalid");
}

export function consumeOidcIntent(
  value: unknown,
  storage: Pick<Storage, "getItem" | "removeItem">,
  now = Date.now(),
): OidcIntent {
  const pendingRaw = storage.getItem(oidcReauthIntentStorageKey);
  if (value === undefined || value === null) {
    if (pendingRaw !== null) {
      storage.removeItem(oidcReauthIntentStorageKey);
      throw new Error("OIDC callback intent does not match pending re-auth");
    }
    return { kind: "login" };
  }

  storage.removeItem(oidcReauthIntentStorageKey);
  if (pendingRaw === null)
    throw new Error("OIDC re-auth state is missing or already consumed");

  let pending: unknown;
  try {
    pending = JSON.parse(pendingRaw);
  } catch {
    throw new Error("Pending OIDC re-auth state is invalid");
  }
  const returnedIntent = parseReauthIntent(value, now);
  const pendingIntent = parseReauthIntent(pending, now);
  if (!sameIntent(returnedIntent, pendingIntent))
    throw new Error("OIDC re-auth state does not match pending operation");
  return returnedIntent;
}

export function recordRuntimeEnvironmentPolicyReauthCompletion(
  intent: RuntimeEnvironmentPolicyIntent,
  storage: Pick<Storage, "setItem">,
  now = Date.now(),
): void {
  const completion: RuntimeEnvironmentPolicyReauthCompletion = {
    challengeRef: intent.challengeRef,
    ...(intent.environmentRef ? { environmentRef: intent.environmentRef } : {}),
    expiresAt: now + intentLifetimeMs,
    kind: intent.kind,
    operation: intent.operation,
    projectRef: intent.projectRef,
    returnPath: intent.returnPath,
    version: 1,
  };
  storage.setItem(
    runtimeEnvironmentPolicyReauthCompletionStorageKey,
    JSON.stringify(completion),
  );
}

export function consumeRuntimeEnvironmentPolicyReauthCompletion(
  storage: Pick<Storage, "getItem" | "removeItem">,
  expected: {
    readonly environmentRef?: string;
    readonly operation: RuntimeEnvironmentPolicyOperation;
    readonly projectRef: string;
  },
  now = Date.now(),
): boolean {
  const raw = storage.getItem(
    runtimeEnvironmentPolicyReauthCompletionStorageKey,
  );
  if (raw === null) return false;
  storage.removeItem(runtimeEnvironmentPolicyReauthCompletionStorageKey);

  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    throw new Error("OIDC re-auth completion is invalid");
  }
  if (!isRecord(value)) throw new Error("OIDC re-auth completion is invalid");
  const hasEnvironment = Object.hasOwn(value, "environmentRef");
  const expectedKeys = [
    "challengeRef",
    ...(hasEnvironment ? ["environmentRef"] : []),
    "expiresAt",
    "kind",
    "operation",
    "projectRef",
    "returnPath",
    "version",
  ] as const;
  if (
    !hasExactKeys(value, expectedKeys) ||
    value.version !== 1 ||
    value.kind !== "runtime-environment-policy" ||
    typeof value.challengeRef !== "string" ||
    !challengeReferencePattern.test(value.challengeRef) ||
    typeof value.expiresAt !== "number" ||
    !Number.isSafeInteger(value.expiresAt) ||
    value.expiresAt <= now ||
    value.expiresAt > now + intentLifetimeMs + allowedFutureSkewMs ||
    value.projectRef !== expected.projectRef ||
    value.operation !== expected.operation ||
    value.environmentRef !== expected.environmentRef ||
    value.returnPath !==
      runtimeEnvironmentPolicyPath(expected.projectRef, expected.environmentRef)
  )
    throw new Error("OIDC re-auth completion does not match current editor");
  return true;
}
