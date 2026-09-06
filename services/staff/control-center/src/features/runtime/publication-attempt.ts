export type PublicationAttemptKind =
  | "SYSTEM_STT"
  | "RUNTIME_ENVIRONMENT"
  | "PROMPT_TEMPLATE"
  | "AGENT_INSTRUCTIONS"
  | "ROLE_IMAGE";
export interface PublicationAttempt {
  kind: PublicationAttemptKind;
  ownerRef: string;
  planRef: string;
  version: number;
  selectedItemRefs: string[];
  key: string;
}
export function publicationRefusalClearsIntent(
  hadUnknownAttempt: boolean,
  status: number,
): boolean {
  // Ответ на повтор не доказывает исход предшествующей попытки.
  return !hadUnknownAttempt && (status === 400 || status === 422);
}
function storageKey(kind: PublicationAttemptKind, ownerRef: string): string {
  return `kodex.publication-attempt:${kind}:${ownerRef}`;
}
export function clearPublicationAttempts(storage: Storage): void {
  const keys = Array.from({ length: storage.length }, (_, index) =>
    storage.key(index),
  );
  for (const key of keys) {
    if (key?.startsWith("kodex.publication-attempt:")) storage.removeItem(key);
  }
}
function validRef(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 256;
}
export function readPublicationAttempt(
  kind: PublicationAttemptKind,
  ownerRef: string,
  storage: Storage,
): PublicationAttempt | undefined {
  const raw = storage.getItem(storageKey(kind, ownerRef));
  if (!raw) return undefined;
  if (raw.length > 300_000)
    throw new Error("Invalid publication recovery intent");
  return checkedAttempt(JSON.parse(raw), kind, ownerRef);
}
function checkedAttempt(
  value: unknown,
  kind: PublicationAttemptKind,
  ownerRef: string,
): PublicationAttempt {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Invalid publication recovery intent");
  const record = value as Record<string, unknown>;
  if (
    record.kind !== kind ||
    record.ownerRef !== ownerRef ||
    !validRef(ownerRef) ||
    !validRef(record.planRef) ||
    typeof record.version !== "number" ||
    !Number.isSafeInteger(record.version) ||
    record.version <= 0 ||
    typeof record.key !== "string" ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
      record.key,
    ) ||
    !Array.isArray(record.selectedItemRefs) ||
    record.selectedItemRefs.length > 1000 ||
    !record.selectedItemRefs.every(validRef) ||
    new Set(record.selectedItemRefs).size !== record.selectedItemRefs.length
  )
    throw new Error("Invalid publication recovery intent");
  return {
    kind,
    ownerRef,
    planRef: record.planRef,
    version: record.version,
    selectedItemRefs: record.selectedItemRefs,
    key: record.key,
  };
}
export function rememberPublicationAttempt(
  attempt: PublicationAttempt,
  storage: Storage,
): void {
  const checked = checkedAttempt(attempt, attempt.kind, attempt.ownerRef);
  const previous = readPublicationAttempt(
    attempt.kind,
    attempt.ownerRef,
    storage,
  );
  if (
    previous &&
    (previous.planRef !== attempt.planRef ||
      previous.version !== attempt.version ||
      previous.key !== attempt.key ||
      JSON.stringify(previous.selectedItemRefs) !==
        JSON.stringify(attempt.selectedItemRefs))
  )
    throw new Error("Unresolved publication intent cannot be replaced");
  storage.setItem(
    storageKey(attempt.kind, attempt.ownerRef),
    JSON.stringify(checked),
  );
}
export function forgetPublicationAttempt(
  kind: PublicationAttemptKind,
  ownerRef: string,
  storage: Storage,
): void {
  storage.removeItem(storageKey(kind, ownerRef));
}
