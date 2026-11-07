import { isRecord } from "@/shared/lib/exact-object";
import {
  configurePersonalIntegrationCredential,
  createPersonalIntegrationCredential,
  listPersonalIntegrationConnections,
  revokePersonalIntegrationCredential,
  testPersonalIntegrationCredential,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  PersonalIntegrationConnection,
  PersonalIntegrationCredential,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

const states = new Set([
  "NOT_CONFIGURED",
  "CHECK_REQUIRED",
  "CHECKING",
  "READY",
  "INVALID",
  "REVOKED",
  "EXPIRED",
]);

// Повторное значение и owner/Secret metadata не входят в сохраняемую UI-проекцию.
export function personalCredentialView(
  value: PersonalIntegrationCredential,
  connection: Pick<
    PersonalIntegrationConnection,
    "connectionRef" | "connectionVersion" | "policy"
  >,
): PersonalIntegrationCredential {
  if (
    !isRecord(value) ||
    value.connectionRef !== connection.connectionRef ||
    value.connectionVersion !== connection.connectionVersion ||
    value.policy !== connection.policy ||
    !value.bindingRef ||
    !Number.isSafeInteger(value.version) ||
    value.version < 1 ||
    !states.has(value.state) ||
    typeof value.credentialConfigured !== "boolean" ||
    !Array.isArray(value.verifiedCapabilityKeys) ||
    !Array.isArray(value.deniedCapabilityKeys) ||
    [...value.verifiedCapabilityKeys, ...value.deniedCapabilityKeys].some(
      (key) => typeof key !== "string" || !key || key.length > 256,
    ) ||
    (value.state === "READY" &&
      (!value.credentialConfigured || !value.verifiedCapabilityKeys.length)) ||
    (value.expiresAt !== undefined &&
      !Number.isFinite(Date.parse(value.expiresAt)))
  )
    throw new Error("Personal credential response is invalid");
  return {
    connectionRef: value.connectionRef,
    connectionVersion: value.connectionVersion,
    bindingRef: value.bindingRef,
    version: value.version,
    policy: value.policy,
    state: value.state,
    credentialConfigured: value.credentialConfigured,
    verifiedCapabilityKeys: [...value.verifiedCapabilityKeys],
    deniedCapabilityKeys: [...value.deniedCapabilityKeys],
    ...(value.expiresAt ? { expiresAt: value.expiresAt } : {}),
  };
}

// Квитанция меняет ровно ожидаемую binding version и закрытое состояние действия.
function personalMutationView(
  value: PersonalIntegrationCredential,
  connection: PersonalIntegrationConnection,
  expectedVersion: number,
  expectedState: PersonalIntegrationCredential["state"],
): PersonalIntegrationCredential {
  const result = personalCredentialView(value, connection);
  if (
    !Number.isSafeInteger(expectedVersion) ||
    result.version !== expectedVersion ||
    result.state !== expectedState ||
    result.credentialConfigured !==
      (expectedState !== "NOT_CONFIGURED" && expectedState !== "REVOKED")
  )
    throw new Error("Personal credential mutation receipt is invalid");
  return result;
}

export async function loadPersonalConnections(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  const page = (
    await unwrap(
      listPersonalIntegrationConnections({
        query: { query, pageToken, pageSize: 50 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    !isRecord(page) ||
    !Array.isArray(page.items) ||
    page.items.length > 50 ||
    (page.nextPageToken !== undefined &&
      (typeof page.nextPageToken !== "string" ||
        page.nextPageToken.length > 512))
  )
    throw new Error("Personal connection page is invalid");
  return {
    // Контракт разрешает отсутствие курсора на последней странице; items остаётся обязательным.
    nextPageToken: page.nextPageToken ?? "",
    items: page.items.map((item): PersonalIntegrationConnection => {
      if (
        !isRecord(item) ||
        !item.connectionRef ||
        !Number.isSafeInteger(item.connectionVersion) ||
        item.connectionVersion < 1 ||
        !["PROJECT_SHARED", "INITIATOR_REQUIRED"].includes(item.policy) ||
        typeof item.available !== "boolean" ||
        typeof item.enabled !== "boolean" ||
        typeof item.name !== "string" ||
        typeof item.definitionKey !== "string"
      )
        throw new Error("Personal connection response is invalid");
      return {
        connectionRef: item.connectionRef,
        connectionVersion: item.connectionVersion,
        name: item.name,
        definitionKey: item.definitionKey,
        policy: item.policy,
        enabled: item.enabled,
        available: item.available,
        ...(item.credential
          ? { credential: personalCredentialView(item.credential, item) }
          : {}),
      };
    }),
  };
}

// Каждое явное действие выполняется один раз. Ошибка не запускает скрытый write retry.
export async function createPersonalConnection(
  connection: PersonalIntegrationConnection,
  signal: AbortSignal,
) {
  return personalMutationView(
    (
      await mutate((headers) =>
        createPersonalIntegrationCredential({
          path: { connectionRef: connection.connectionRef },
          body: { expectedConnectionVersion: connection.connectionVersion },
          headers: { ...headers },
          signal: requestSignal(signal),
        }),
      )
    ).data,
    connection,
    1,
    "NOT_CONFIGURED",
  );
}

export async function savePersonalConnection(
  connection: PersonalIntegrationConnection,
  credential: PersonalIntegrationCredential,
  value: string,
  signal: AbortSignal,
) {
  return personalMutationView(
    (
      await mutate(
        (headers) =>
          configurePersonalIntegrationCredential({
            path: { connectionRef: connection.connectionRef },
            body: {
              expectedConnectionVersion: connection.connectionVersion,
              value,
            },
            headers: { ...headers, "If-Match": etag(credential.version) },
            signal: requestSignal(signal),
          }),
        credential.version,
      )
    ).data,
    connection,
    credential.version + 1,
    "CHECK_REQUIRED",
  );
}

export async function testPersonalConnection(
  connection: PersonalIntegrationConnection,
  credential: PersonalIntegrationCredential,
  signal: AbortSignal,
) {
  return personalMutationView(
    (
      await mutate(
        (headers) =>
          testPersonalIntegrationCredential({
            path: { connectionRef: connection.connectionRef },
            body: { expectedConnectionVersion: connection.connectionVersion },
            headers: { ...headers, "If-Match": etag(credential.version) },
            signal: requestSignal(signal),
          }),
        credential.version,
      )
    ).data,
    connection,
    credential.version + 1,
    "CHECKING",
  );
}

export async function revokePersonalConnection(
  connection: PersonalIntegrationConnection,
  credential: PersonalIntegrationCredential,
  signal: AbortSignal,
) {
  return personalMutationView(
    (
      await mutate(
        (headers) =>
          revokePersonalIntegrationCredential({
            path: { connectionRef: connection.connectionRef },
            body: {},
            headers: { ...headers, "If-Match": etag(credential.version) },
            signal: requestSignal(signal),
          }),
        credential.version,
      )
    ).data,
    connection,
    credential.version + 1,
    "REVOKED",
  );
}
