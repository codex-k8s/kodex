import { configureEmailMailboxCredential } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  EmailMailboxCredential,
  EmailMailboxCredentialKind,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { etag, idempotencyKey, mutate } from "@/shared/api/mutation";
import { requestSignal } from "@/shared/api/client";

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

export interface MailboxCredentialAttempt {
  connectionRef: string;
  connectionVersion: number;
  kind: EmailMailboxCredentialKind;
  key: string;
  inputDigest: string;
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
  const inputDigest = Array.from(
    new Uint8Array(
      await crypto.subtle.digest(
        "SHA-256",
        new TextEncoder().encode(JSON.stringify({ kind, value })),
      ),
    ),
    (byte) => byte.toString(16).padStart(2, "0"),
  ).join("");
  if (pending) {
    if (
      pending.connectionRef !== connection.ref ||
      pending.kind !== kind ||
      pending.inputDigest !== inputDigest
    )
      throw new MailboxCredentialMismatch();
    return pending;
  }
  return {
    connectionRef: connection.ref,
    connectionVersion: connection.version,
    kind,
    inputDigest,
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
  if (
    item.connectionRef !== attempt.connectionRef ||
    item.kind !== attempt.kind ||
    !Number.isSafeInteger(item.connectionVersion) ||
    item.connectionVersion <= attempt.connectionVersion ||
    !Number.isSafeInteger(item.generation) ||
    item.generation < 1 ||
    typeof item.name !== "string" ||
    !/^[A-Za-z0-9_-]{1,128}$/.test(item.name) ||
    result.etag !== etag(item.connectionVersion)
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
