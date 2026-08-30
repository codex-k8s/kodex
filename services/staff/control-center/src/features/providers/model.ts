import type {
  NextAction,
  ProviderAccount as ApiProviderAccount,
  ProviderAccountCandidate as ApiProviderAccountCandidate,
  ProviderAccountCreateInput as ApiProviderAccountCreateInput,
  ProviderAccountPage as ApiProviderAccountPage,
  ProviderAuthorization as ApiProviderAuthorization,
  ProviderDefinition as ApiProviderDefinition,
  ProviderDefinitionPage as ApiProviderDefinitionPage,
} from "@/shared/api/generated/openapi/types.gen";

export type ProviderDefinition = ApiProviderDefinition;
export type ProviderDefinitionPage = ApiProviderDefinitionPage;
export type ProviderDefinitionKey = ProviderDefinition["key"];
export type ProviderAuthorization = ApiProviderAuthorization;
export type ProviderAuthorizationMethod = ProviderAuthorization["method"];
export type ProviderAuthorizationState = ProviderAuthorization["state"];
export type ProviderAccount = ApiProviderAccount;
export type ProviderAccountState = ProviderAccount["state"];
export type ProviderAccountPage = ApiProviderAccountPage;
export type ProviderAccountCreateInput = ApiProviderAccountCreateInput;
export type ProviderAccountCandidate = ApiProviderAccountCandidate;
export type ProviderAccountAction = NextAction;
export type ProviderPolicyMode = "FIXED" | "LEAST_USED" | "WEIGHTED";

export function accountAllows(
  account: Pick<ProviderAccount, "nextActions">,
  action: ProviderAccountAction,
): boolean {
  return account.nextActions.includes(action);
}

export function pageAllowsAccountCreation(
  actions: readonly ProviderAccountAction[],
): boolean {
  return actions.includes("CREATE_CONNECTION");
}

export function isRuntimeEligible(
  account: Pick<ProviderAccount, "enabled" | "ready" | "state">,
): boolean {
  return account.enabled && account.ready && account.state === "AUTHORIZED";
}

export function isPendingDeviceAuthorization(
  account: Pick<ProviderAccount, "authorization">,
  now = Date.now(),
): boolean {
  const authorization = account.authorization;
  if (
    authorization?.method !== "DEVICE_CODE" ||
    authorization.state !== "PENDING"
  )
    return false;
  const expiresAt = authorization.expiresAt
    ? Date.parse(authorization.expiresAt)
    : Number.NaN;
  return !Number.isFinite(expiresAt) || expiresAt > now;
}

export function safeVerificationUri(value: string | undefined): string | null {
  if (!value) return null;
  try {
    const url = new URL(value);
    return url.protocol === "https:" ? url.toString() : null;
  } catch {
    return null;
  }
}

export function readableProviderBlocker(
  code: string,
):
  | "PROVIDER_UNAVAILABLE"
  | "AUTHORIZATION_UNAVAILABLE"
  | "RUNTIME_UNAVAILABLE"
  | "UNKNOWN" {
  if (code.includes("AUTH")) return "AUTHORIZATION_UNAVAILABLE";
  if (code.includes("RUNTIME") || code.includes("MATERIAL"))
    return "RUNTIME_UNAVAILABLE";
  if (code.includes("PROVIDER") || code.includes("DEFINITION"))
    return "PROVIDER_UNAVAILABLE";
  return "UNKNOWN";
}

export function upsertProviderAccount(
  accounts: readonly ProviderAccount[],
  account: ProviderAccount,
): ProviderAccount[] {
  const next = accounts.filter((item) => item.ref !== account.ref);
  next.push(account);
  return next.sort((left, right) => left.name.localeCompare(right.name, "ru"));
}

export function toggleProviderAccountCandidate(
  candidates: readonly ProviderAccountCandidate[],
  accountRef: string,
  mode: ProviderPolicyMode,
): ProviderAccountCandidate[] {
  if (candidates.some((item) => item.accountRef === accountRef))
    return candidates.filter((item) => item.accountRef !== accountRef);
  const candidate = { accountRef, weight: 1 };
  return mode === "FIXED" ? [candidate] : [...candidates, candidate];
}

export function normalizeProviderAccountCandidates(
  candidates: readonly ProviderAccountCandidate[],
  mode: ProviderPolicyMode,
): ProviderAccountCandidate[] {
  if (mode === "FIXED") {
    const first = candidates[0];
    return first ? [{ ...first, weight: 1 }] : [];
  }
  if (mode === "LEAST_USED")
    return candidates.map((item) => ({ ...item, weight: 1 }));
  return candidates.map((item) => ({ ...item }));
}
