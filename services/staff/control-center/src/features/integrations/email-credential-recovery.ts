import type { EmailMailboxCredentialKind } from "@/shared/api/generated/openapi/types.gen";

export const mailboxCredentialRecoveryKey = "kodex.email.credential-attempts";
export interface MailboxCredentialAttempt {
  connectionRef: string;
  connectionVersion: number;
  kind: EmailMailboxCredentialKind;
  key: string;
}
type RecoveryStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;
function read(storage: RecoveryStorage): MailboxCredentialAttempt[] {
  const raw = storage.getItem(mailboxCredentialRecoveryKey);
  if (!raw) return [];
  const entries: unknown = JSON.parse(raw);
  if (
    !Array.isArray(entries) ||
    entries.length > 20 ||
    entries.some((entry: unknown) => {
      if (!entry || typeof entry !== "object") return true;
      const value = entry as Partial<MailboxCredentialAttempt>;
      return (
        Object.keys(value).sort().join(",") !==
          "connectionRef,connectionVersion,key,kind" ||
        typeof value.connectionRef !== "string" ||
        !/^[A-Za-z0-9_-]{1,128}$/.test(value.connectionRef) ||
        !Number.isSafeInteger(value.connectionVersion) ||
        (value.connectionVersion ?? 0) < 1 ||
        !["CA_CERTIFICATE", "USERNAME", "AUTH_SECRET"].includes(
          value.kind ?? "",
        ) ||
        typeof value.key !== "string" ||
        !/^[0-9a-f-]{36}$/.test(value.key)
      );
    })
  )
    throw new Error("Invalid mailbox credential recovery metadata");
  const result = entries as MailboxCredentialAttempt[];
  if (
    new Set(result.map((entry) => entry.connectionRef)).size !== result.length
  )
    throw new Error("Duplicate mailbox credential recovery metadata");
  return result;
}
export function pendingMailboxCredential(
  connectionRef: string,
  storage: RecoveryStorage,
): MailboxCredentialAttempt | undefined {
  return read(storage).find((entry) => entry.connectionRef === connectionRef);
}
export function rememberMailboxCredential(
  attempt: MailboxCredentialAttempt,
  storage: RecoveryStorage,
): void {
  const entries = read(storage);
  const previous = entries.find(
    (entry) => entry.connectionRef === attempt.connectionRef,
  );
  if (
    previous &&
    (previous.key !== attempt.key ||
      previous.kind !== attempt.kind ||
      previous.connectionVersion !== attempt.connectionVersion)
  )
    throw new Error("Mailbox credential recovery attempt changed");
  if (!previous) {
    if (entries.length >= 20)
      throw new Error("Mailbox credential recovery limit reached");
    entries.push({
      connectionRef: attempt.connectionRef,
      connectionVersion: attempt.connectionVersion,
      kind: attempt.kind,
      key: attempt.key,
    });
    storage.setItem(mailboxCredentialRecoveryKey, JSON.stringify(entries));
  }
}
export function forgetMailboxCredential(
  attempt: MailboxCredentialAttempt,
  storage: RecoveryStorage,
): void {
  const entries = read(storage).filter(
    (entry) =>
      entry.connectionRef !== attempt.connectionRef ||
      entry.key !== attempt.key,
  );
  if (entries.length)
    storage.setItem(mailboxCredentialRecoveryKey, JSON.stringify(entries));
  else storage.removeItem(mailboxCredentialRecoveryKey);
}
