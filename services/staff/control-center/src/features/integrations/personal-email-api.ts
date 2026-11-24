import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  EmailMailboxOwnership,
  EmailMailboxOwnershipInput,
  IntegrationConnection,
  MyEmailMailbox,
  PersonalEmailMailboxConnectionInput,
  PersonalEmailMailboxMutationOperation,
  PersonalEmailMailboxMutationReceipt,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { isRecord, hasExactKeys } from "@/shared/lib/exact-object";

const isRevokeOperation = (operation: unknown): operation is "REVOKE" =>
  operation === "REVOKE";
const invalidMailboxResponse = "Personal mailbox response is invalid";
const reference = (value: unknown): value is string =>
  typeof value === "string" && /^[A-Za-z][A-Za-z0-9_-]{7,95}$/.test(value);
const positive = (value: unknown): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value > 0;

export function validPersonalEmailName(value: unknown): value is string {
  return (
    typeof value === "string" &&
    !!value.trim() &&
    Array.from(value).length <= 160 &&
    !/[\0\r\n]/.test(value) &&
    new TextDecoder().decode(new TextEncoder().encode(value)) === value
  );
}

// Состояние ACTIVE не заменяет текущую eligibility subject/configurer. Это
// безопасная проекция настройки, не selector credentials или разрешение вызова.
export function checkedMailboxOwnership(
  value: unknown,
  connectionRef?: string,
): EmailMailboxOwnership {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "connectionRef",
      "connectionVersion",
      "deliveryKind",
      "policy",
      "state",
      "generation",
      "ownerEligible",
      ...(Object.hasOwn(value, "serviceSubjectRef")
        ? ["serviceSubjectRef"]
        : []),
    ]) ||
    !reference(value.connectionRef) ||
    (connectionRef !== undefined && value.connectionRef !== connectionRef) ||
    !positive(value.connectionVersion) ||
    value.deliveryKind !== "MAILBOX_CONFIGURATION" ||
    !["INITIATOR_REQUIRED", "PROJECT_SHARED"].includes(String(value.policy)) ||
    !["UNCONFIGURED", "ACTIVE", "REVOKED"].includes(String(value.state)) ||
    typeof value.ownerEligible !== "boolean" ||
    (value.ownerEligible && value.state !== "ACTIVE") ||
    typeof value.generation !== "number" ||
    !Number.isSafeInteger(value.generation) ||
    (value.state === "UNCONFIGURED"
      ? value.generation !== 0
      : value.generation < 1) ||
    (value.policy === "PROJECT_SHARED" && value.state !== "UNCONFIGURED"
      ? !reference(value.serviceSubjectRef)
      : Object.hasOwn(value, "serviceSubjectRef"))
  )
    throw new Error(invalidMailboxResponse);
  return {
    connectionRef: value.connectionRef,
    connectionVersion: value.connectionVersion,
    deliveryKind: "MAILBOX_CONFIGURATION",
    policy: value.policy as EmailMailboxOwnership["policy"],
    state: value.state as EmailMailboxOwnership["state"],
    generation: value.generation,
    ownerEligible: value.ownerEligible,
    ...(typeof value.serviceSubjectRef === "string"
      ? { serviceSubjectRef: value.serviceSubjectRef }
      : {}),
  };
}

export function checkedMyMailbox(value: unknown): MyEmailMailbox {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ["ownership", "name", "enabled", "available"]) ||
    !validPersonalEmailName(value.name) ||
    typeof value.enabled !== "boolean" ||
    typeof value.available !== "boolean"
  )
    throw new Error(invalidMailboxResponse);
  const ownership = checkedMailboxOwnership(value.ownership);
  if (value.available && (!value.enabled || !ownership.ownerEligible))
    throw new Error(invalidMailboxResponse);
  return {
    ownership,
    name: value.name,
    enabled: value.enabled,
    available: value.available,
  };
}

export async function loadMyMailboxes(
  search: string,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  const page = (
    await unwrap(
      sdk.listMyEmailMailboxes({
        query: { query: search, pageToken, pageSize: 50 },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (
    !isRecord(page) ||
    !hasExactKeys(page, [
      "items",
      ...(Object.hasOwn(page, "nextPageToken") ? ["nextPageToken"] : []),
    ]) ||
    !Array.isArray(page.items) ||
    page.items.length > 50 ||
    (page.nextPageToken !== undefined &&
      (typeof page.nextPageToken !== "string" ||
        page.nextPageToken.length > 4096 ||
        !page.nextPageToken))
  )
    throw new Error(invalidMailboxResponse);
  const items = page.items.map(checkedMyMailbox);
  if (
    new Set(items.map((item) => item.ownership.connectionRef)).size !==
      items.length ||
    (page.nextPageToken && !items.length)
  )
    throw new Error(invalidMailboxResponse);
  return { items, nextPageToken: page.nextPageToken ?? "" };
}

export async function readMailboxOwnership(
  connectionRef: string,
  signal: AbortSignal,
) {
  return checkedMailboxOwnership(
    (
      await unwrap(
        sdk.getEmailMailboxOwnership({
          path: { connectionRef },
          signal: requestSignal(signal),
          cache: "no-store",
        }),
      )
    ).data,
    connectionRef,
  );
}

export interface MailboxSetupRequest {
  operation: PersonalEmailMailboxMutationOperation;
  key: string;
  connectionRef: string;
  connectionVersion: number;
}
export type MailboxSetupBody =
  | PersonalEmailMailboxConnectionInput
  | EmailMailboxOwnershipInput
  | undefined;

// Один явный вызов с прежним ключом; автоматического retry либо нового ключа
// после потерянного ACK нет. Возвращённая квитанция требует свежего owner read.
export async function writeMailboxSetup(
  attempt: MailboxSetupRequest,
  body: MailboxSetupBody,
  signal: AbortSignal,
): Promise<PersonalEmailMailboxMutationReceipt> {
  const transport = requestSignal(signal);
  if (attempt.operation === "CREATE") {
    if (
      !body ||
      !("name" in body) ||
      attempt.connectionRef ||
      attempt.connectionVersion !== 0
    )
      throw new Error("Mailbox setup request is invalid");
    const mailbox = checkedMyMailbox(
      (
        await mutate(
          (headers) =>
            sdk.createPersonalEmailMailboxConnection({
              body,
              headers: { ...headers },
              signal: transport,
            }),
          undefined,
          attempt.key,
        )
      ).data,
    );
    if (
      mailbox.name !== body.name.trim() ||
      !mailbox.enabled ||
      mailbox.available ||
      mailbox.ownership.connectionVersion !== 1 ||
      mailbox.ownership.generation !== 1 ||
      mailbox.ownership.policy !== "INITIATOR_REQUIRED" ||
      !mailbox.ownership.ownerEligible
    )
      throw new Error(invalidMailboxResponse);
    return { operation: "CREATE", mailbox };
  }
  const path = { connectionRef: attempt.connectionRef };
  let result: EmailMailboxOwnership;
  if (attempt.operation === "CONFIGURE") {
    if (!body || !("policy" in body))
      throw new Error("Mailbox setup request is invalid");
    result = checkedMailboxOwnership(
      (
        await mutate(
          (headers) =>
            sdk.configureEmailMailboxOwnership({
              path,
              body,
              headers: {
                ...headers,
                "If-Match": etag(attempt.connectionVersion),
              },
              signal: transport,
            }),
          attempt.connectionVersion,
          attempt.key,
        )
      ).data,
      attempt.connectionRef,
    );
    if (
      result.state !== "ACTIVE" ||
      !result.ownerEligible ||
      result.policy !== body.policy ||
      result.serviceSubjectRef !== body.serviceSubjectRef
    )
      throw new Error(invalidMailboxResponse);
  } else {
    if (body !== undefined || !isRevokeOperation(attempt.operation))
      throw new Error("Mailbox setup request is invalid");
    result = checkedMailboxOwnership(
      (
        await mutate(
          (headers) =>
            sdk.revokeEmailMailboxOwnership({
              path,
              headers: {
                ...headers,
                "If-Match": etag(attempt.connectionVersion),
              },
              signal: transport,
            }),
          attempt.connectionVersion,
          attempt.key,
        )
      ).data,
      attempt.connectionRef,
    );
    if (result.state !== "REVOKED" || result.ownerEligible)
      throw new Error(invalidMailboxResponse);
  }
  if (result.connectionVersion !== attempt.connectionVersion + 1)
    throw new Error(invalidMailboxResponse);
  return { operation: attempt.operation, ownership: result };
}

export async function recoverMailboxSetup(
  attempt: MailboxSetupRequest,
  signal: AbortSignal,
): Promise<PersonalEmailMailboxMutationReceipt> {
  const value = (
    await unwrap(
      sdk.getPersonalEmailMailboxMutationReceipt({
        query: { operation: attempt.operation, idempotencyKey: attempt.key },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (
    !isRecord(value) ||
    value.operation !== attempt.operation ||
    !hasExactKeys(value, [
      "operation",
      attempt.operation === "CREATE" ? "mailbox" : "ownership",
    ])
  )
    throw new Error(invalidMailboxResponse);
  if (attempt.operation === "CREATE")
    return { operation: "CREATE", mailbox: checkedMyMailbox(value.mailbox) };
  return {
    operation: attempt.operation,
    ownership: checkedMailboxOwnership(value.ownership, attempt.connectionRef),
  };
}

// Проверка READ использует прежнюю owner-команду и серверный snapshot. Ошибка
// не означает отмену проверки; перед новым явным действием нужен fresh read.
export async function checkMailboxHealth(
  connection: IntegrationConnection,
  ownership: EmailMailboxOwnership,
  signal: AbortSignal,
): Promise<void> {
  if (
    connection.definitionKey !== "email" ||
    !["NOT_CONNECTED", "CONNECTED", "DEGRADED"].includes(connection.state) ||
    ownership.connectionRef !== connection.ref ||
    ownership.connectionVersion !== connection.version ||
    !ownership.ownerEligible ||
    ownership.state !== "ACTIVE"
  )
    throw new Error("Mailbox health setup is unavailable");
  const result = (
    await mutate(
      (headers) =>
        sdk.commandIntegrationConnection({
          path: { connectionRef: connection.ref },
          body: { action: "TEST" },
          headers: { ...headers, "If-Match": etag(connection.version) },
          signal: requestSignal(signal),
        }),
      connection.version,
    )
  ).data;
  if (
    result.ref !== connection.ref ||
    result.definitionKey !== "email" ||
    result.version !== connection.version + 1 ||
    result.state !== "TESTING"
  )
    throw new Error(invalidMailboxResponse);
}
