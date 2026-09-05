import {
  configureEmailMailboxCredential,
  getEmailMailboxCredentialReceipt,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  EmailMailboxCredential,
  EmailMailboxCredentialKind,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { etag, idempotencyKey, mutate } from "@/shared/api/mutation";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import type { MailboxCredentialAttempt } from "./email-credential-recovery";
export type { MailboxCredentialAttempt } from "./email-credential-recovery";

export const mailboxCredentialLimits: Record<
  EmailMailboxCredentialKind,
  number
> = {
  CA_CERTIFICATE: 65536,
  USERNAME: 320,
  AUTH_SECRET: 16384,
};

export function validMailboxCredential(
  kind: EmailMailboxCredentialKind,
  value: string,
): boolean {
  const bytes = new TextEncoder().encode(value);
  const size = bytes.byteLength;
  return (
    Object.hasOwn(mailboxCredentialLimits, kind) &&
    new TextDecoder().decode(bytes) === value &&
    size > 0 &&
    size <= mailboxCredentialLimits[kind] &&
    (kind === "CA_CERTIFICATE" ||
      !["\0", "\r", "\n"].some((character) => value.includes(character)))
  );
}

export class MailboxCredentialMismatch extends Error {
  constructor() {
    super("Mailbox credential differs from the pending attempt");
  }
}

export async function prepareMailboxCredential(
  connection: IntegrationConnection,
  kind: EmailMailboxCredentialKind,
  value: string,
  pending?: MailboxCredentialAttempt,
): Promise<MailboxCredentialAttempt> {
  if (
    connection.definitionKey !== "email" ||
    !connection.nextActions.includes("CONFIGURE_CREDENTIAL") ||
    !validMailboxCredential(kind, value)
  )
    throw new Error("Mailbox credential input is invalid");
  etag(connection.version);
  // Сохраняем асинхронную границу для очистки ввода до отправки команды.
  await Promise.resolve();
  if (pending) {
    if (pending.connectionRef !== connection.ref || pending.kind !== kind)
      throw new MailboxCredentialMismatch();
    return pending;
  }
  return {
    connectionRef: connection.ref,
    connectionVersion: connection.version,
    kind,
    key: idempotencyKey(),
  };
}

export async function saveMailboxCredential(
  attempt: MailboxCredentialAttempt,
  value: string,
  signal: AbortSignal,
): Promise<EmailMailboxCredential> {
  if (!validMailboxCredential(attempt.kind, value))
    throw new Error("Mailbox credential input is invalid");
  const result = await mutate(
    (headers) =>
      configureEmailMailboxCredential({
        path: { connectionRef: attempt.connectionRef },
        body: { kind: attempt.kind, value },
        headers: { ...headers, "If-Match": etag(attempt.connectionVersion) },
        signal: requestSignal(signal),
      }),
    attempt.connectionVersion,
    attempt.key,
  );
  const item = result.data;
  if (result.etag !== etag(item.connectionVersion))
    throw new Error("Invalid mailbox credential receipt ETag");
  return checkedMailboxCredential(item, attempt);
}

export async function recoverMailboxCredential(
  attempt: MailboxCredentialAttempt,
  signal: AbortSignal,
): Promise<EmailMailboxCredential> {
  const result = await unwrap(
    getEmailMailboxCredentialReceipt({
      path: { connectionRef: attempt.connectionRef },
      query: { idempotencyKey: attempt.key },
      signal: requestSignal(signal),
      cache: "no-store",
    }),
  );
  return checkedMailboxCredential(result.data, attempt);
}

function checkedMailboxCredential(
  item: EmailMailboxCredential,
  attempt: MailboxCredentialAttempt,
): EmailMailboxCredential {
  if (
    item.connectionRef !== attempt.connectionRef ||
    item.kind !== attempt.kind ||
    !Number.isSafeInteger(item.connectionVersion) ||
    item.connectionVersion <= attempt.connectionVersion ||
    !Number.isSafeInteger(item.generation) ||
    item.generation < 1 ||
    typeof item.name !== "string" ||
    !/^[A-Za-z0-9_-]{1,128}$/.test(item.name)
  )
    throw new Error("Invalid mailbox credential receipt");
  return {
    name: item.name,
    generation: item.generation,
    kind: item.kind,
    connectionRef: item.connectionRef,
    connectionVersion: item.connectionVersion,
  };
}
