import type { ProviderAccountQueuedWorkCancellationInput } from "@/shared/api/generated/openapi/types.gen";

const prefix = "kodex.provider-lifecycle:";
type Base = { accountRef: string; version: number; key: string };
export type ProviderLifecycleAttempt = Base &
  (
    | { action: "DELETE" | "VERIFY" | "REAUTHORIZE" }
    | {
        action: "CANCEL_QUEUED";
        body: ProviderAccountQueuedWorkCancellationInput;
      }
  );
function validRef(value: unknown): value is string {
  return typeof value === "string" && /^[A-Za-z0-9_-]{8,128}$/.test(value);
}
function checked(value: unknown, accountRef: string): ProviderLifecycleAttempt {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Invalid provider lifecycle recovery intent");
  const record = value as Record<string, unknown>;
  if (
    record.accountRef !== accountRef ||
    !validRef(accountRef) ||
    typeof record.version !== "number" ||
    !Number.isSafeInteger(record.version) ||
    record.version < 1 ||
    typeof record.key !== "string" ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
      record.key,
    )
  )
    throw new Error("Invalid provider lifecycle recovery intent");
  const base = { accountRef, version: record.version, key: record.key };
  if (
    record.action === "DELETE" ||
    record.action === "VERIFY" ||
    record.action === "REAUTHORIZE"
  )
    return { ...base, action: record.action };
  if (
    record.action !== "CANCEL_QUEUED" ||
    !record.body ||
    typeof record.body !== "object" ||
    Array.isArray(record.body)
  )
    throw new Error("Invalid provider lifecycle recovery intent");
  const body = record.body as Record<string, unknown>;
  if (
    typeof body.blockersDigest !== "string" ||
    !/^[a-f0-9]{64}$/.test(body.blockersDigest) ||
    !Array.isArray(body.selectedRunRefs) ||
    body.selectedRunRefs.length < 1 ||
    body.selectedRunRefs.length > 64 ||
    !body.selectedRunRefs.every(validRef) ||
    new Set(body.selectedRunRefs).size !== body.selectedRunRefs.length
  )
    throw new Error("Invalid provider lifecycle recovery intent");
  return {
    ...base,
    action: "CANCEL_QUEUED",
    body: {
      blockersDigest: body.blockersDigest,
      selectedRunRefs: [...body.selectedRunRefs],
    },
  };
}
export function readProviderLifecycleAttempt(
  accountRef: string,
  storage: Storage,
): ProviderLifecycleAttempt | undefined {
  const raw = storage.getItem(prefix + accountRef);
  if (!raw) return undefined;
  if (raw.length > 12_000)
    throw new Error("Provider lifecycle recovery intent exceeds limit");
  return checked(JSON.parse(raw), accountRef);
}
export function rememberProviderLifecycleAttempt(
  attempt: ProviderLifecycleAttempt,
  storage: Storage,
): void {
  const current = checked(attempt, attempt.accountRef);
  const previous = readProviderLifecycleAttempt(attempt.accountRef, storage);
  if (previous && JSON.stringify(previous) !== JSON.stringify(current))
    throw new Error("Unresolved provider lifecycle intent cannot be replaced");
  storage.setItem(prefix + attempt.accountRef, JSON.stringify(current));
}
export function forgetProviderLifecycleAttempt(
  accountRef: string,
  storage: Storage,
): void {
  storage.removeItem(prefix + accountRef);
}
export function clearProviderLifecycleAttempts(storage: Storage): void {
  const keys = Array.from({ length: storage.length }, (_, index) =>
    storage.key(index),
  );
  for (const key of keys) if (key?.startsWith(prefix)) storage.removeItem(key);
}
