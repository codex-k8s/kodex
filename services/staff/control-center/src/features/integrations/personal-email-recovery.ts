import type {
  MailboxSetupBody,
  MailboxSetupRequest,
} from "./personal-email-api";
import {
  readEmailRecoveryEntries,
  emailRecoveryEntryLimit,
} from "./email-recovery-storage";
import { hasExactKeys, isRecord } from "@/shared/lib/exact-object";
import { idempotencyKey } from "@/shared/api/mutation";
import { asProblem } from "@/shared/api/problem";

export const mailboxSetupRecoveryKey = "kodex.email.mailbox-setup-attempts";
const invalidMailboxAttempt = "Mailbox setup recovery metadata is invalid";
export interface MailboxSetupAttempt extends MailboxSetupRequest {
  digest: string;
}
type RecoveryStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

function validMailboxSetupAttempt(
  entry: unknown,
): entry is MailboxSetupAttempt {
  return (
    isRecord(entry) &&
    hasExactKeys(entry, [
      "operation",
      "key",
      "connectionRef",
      "connectionVersion",
      "digest",
    ]) &&
    ["CREATE", "CONFIGURE", "REVOKE"].includes(String(entry.operation)) &&
    typeof entry.key === "string" &&
    /^[0-9a-f-]{36}$/.test(entry.key) &&
    typeof entry.digest === "string" &&
    /^[a-f0-9]{64}$/.test(entry.digest) &&
    typeof entry.connectionRef === "string" &&
    Number.isSafeInteger(entry.connectionVersion) &&
    (entry.operation === "CREATE"
      ? entry.connectionRef === "" && entry.connectionVersion === 0
      : /^[A-Za-z][A-Za-z0-9_-]{7,95}$/.test(entry.connectionRef) &&
        Number(entry.connectionVersion) > 0)
  );
}

// Только первый запрос с новым ключом можно завершить по явному отказу owner.
// Отказ повтору старой неопределённой попытки не доказывает отсутствие её commit.
export function mailboxSetupRejected(error: unknown): boolean {
  return [400, 403, 404, 409, 413, 422].includes(asProblem(error).status);
}

// Только coordinates и digest предложения; адрес, имя, passwords и config body
// в storage не сохраняются. Неопределённая попытка не вытесняется новым ключом.
export function mailboxSetupAttempts(
  storage: RecoveryStorage,
): MailboxSetupAttempt[] {
  const entries =
    readEmailRecoveryEntries(
      storage,
      mailboxSetupRecoveryKey,
      invalidMailboxAttempt,
    ) ?? [];
  for (const entry of entries) {
    if (!validMailboxSetupAttempt(entry))
      throw new Error(invalidMailboxAttempt);
  }
  const values = entries as unknown as MailboxSetupAttempt[];
  if (
    new Set(values.map((value) => value.connectionRef)).size !== values.length
  )
    throw new Error(invalidMailboxAttempt);
  return values;
}

async function setupDigest(
  request: Omit<MailboxSetupRequest, "key">,
  body: MailboxSetupBody,
): Promise<string> {
  const normalized =
    body && "name" in body
      ? { name: body.name, sender: body.sender }
      : body
        ? {
            policy: body.policy,
            ...(body.serviceSubjectRef
              ? { serviceSubjectRef: body.serviceSubjectRef }
              : {}),
          }
        : null;
  const bytes = new TextEncoder().encode(
    JSON.stringify({
      operation: request.operation,
      connectionRef: request.connectionRef,
      connectionVersion: request.connectionVersion,
      body: normalized,
    }),
  );
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  bytes.fill(0);
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

export async function prepareMailboxSetup(
  request: Omit<MailboxSetupRequest, "key">,
  body: MailboxSetupBody,
  pending?: MailboxSetupAttempt,
): Promise<MailboxSetupAttempt> {
  const digest = await setupDigest(request, body);
  if (pending) {
    if (
      pending.digest !== digest ||
      pending.operation !== request.operation ||
      pending.connectionRef !== request.connectionRef ||
      pending.connectionVersion !== request.connectionVersion
    )
      throw new Error("Mailbox setup differs from the pending proposal");
    return pending;
  }
  const result = {
    operation: request.operation,
    connectionRef: request.connectionRef,
    connectionVersion: request.connectionVersion,
    key: idempotencyKey(),
    digest,
  };
  if (!validMailboxSetupAttempt(result)) throw new Error(invalidMailboxAttempt);
  return result;
}

export function rememberMailboxSetup(
  attempt: MailboxSetupAttempt,
  storage: RecoveryStorage,
): void {
  const packed = {
    operation: attempt.operation,
    connectionRef: attempt.connectionRef,
    connectionVersion: attempt.connectionVersion,
    key: attempt.key,
    digest: attempt.digest,
  };
  if (!validMailboxSetupAttempt(packed)) throw new Error(invalidMailboxAttempt);
  const entries = mailboxSetupAttempts(storage);
  const prior = entries.find(
    (value) => value.connectionRef === attempt.connectionRef,
  );
  if (
    prior &&
    (prior.key !== packed.key ||
      prior.operation !== packed.operation ||
      prior.digest !== packed.digest ||
      prior.connectionVersion !== packed.connectionVersion)
  )
    throw new Error("Pending mailbox setup cannot be replaced");
  if (prior) return;
  if (entries.length >= emailRecoveryEntryLimit)
    throw new Error("Mailbox setup recovery limit exceeded");
  entries.push(packed);
  storage.setItem(mailboxSetupRecoveryKey, JSON.stringify(entries));
}

export function forgetMailboxSetup(
  attempt: MailboxSetupAttempt,
  storage: RecoveryStorage,
): void {
  const entries = mailboxSetupAttempts(storage).filter(
    (value) =>
      value.key !== attempt.key ||
      value.connectionRef !== attempt.connectionRef,
  );
  if (entries.length)
    storage.setItem(mailboxSetupRecoveryKey, JSON.stringify(entries));
  else storage.removeItem(mailboxSetupRecoveryKey);
}
