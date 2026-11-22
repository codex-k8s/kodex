import { isSSHBrokerDefinition } from "./ssh-identity-setup-context";
import { kubernetesSetupRequirements } from "./kubernetes-identity-setup-api";
import { githubDiscoveryView } from "./github-repository-scope";
import {
  personalCredentialDelivery,
  personalCredentialMetadataRefreshRequired,
  readCredentialDelivery,
} from "./credential-delivery";
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
  const delivery = readCredentialDelivery(value.credentialDelivery);
  const identity = value.brokerIdentity;
  if (
    (value.credentialDelivery !== undefined && !delivery) ||
    (delivery &&
      (!Number.isSafeInteger(value.bindingGeneration) ||
        !value.bindingGeneration ||
        value.bindingGeneration < 1)) ||
    (!delivery && value.bindingGeneration !== undefined) ||
    (identity !== undefined &&
      (!isRecord(identity) ||
        Object.keys(identity).length !== 2 ||
        typeof identity.ref !== "string" ||
        !/^[A-Za-z0-9_-]{8,96}$/.test(identity.ref) ||
        !Number.isSafeInteger(identity.revision) ||
        identity.revision < 1 ||
        delivery?.kind !== "BROKER_IDENTITY")) ||
    (delivery?.kind === "BROKER_IDENTITY" &&
      value.credentialConfigured &&
      !identity)
  )
    throw new Error("Personal credential metadata is invalid");
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
    ...(delivery
      ? {
          credentialDelivery: delivery,
          bindingGeneration: value.bindingGeneration,
        }
      : {}),
    ...(identity
      ? { brokerIdentity: { ref: identity.ref, revision: identity.revision } }
      : {}),
    ...(value.expiresAt ? { expiresAt: value.expiresAt } : {}),
    ...(value.githubDiscovery
      ? { githubDiscovery: githubDiscoveryView(value.githubDiscovery) }
      : {}),
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
    (expectedState === "NOT_CONFIGURED" && result.credentialConfigured) ||
    (expectedState !== "NOT_CONFIGURED" &&
      expectedState !== "REVOKED" &&
      !result.credentialConfigured)
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
      const delivery = readCredentialDelivery(item.credentialDelivery);
      if (item.credentialDelivery !== undefined && !delivery)
        throw new Error("Personal connection credential metadata is invalid");
      const requirements = kubernetesSetupRequirements({
        requirements: item.kubernetesSetupRequirements,
        connectionVersion: item.connectionVersion,
        policy: item.policy,
      });
      if (
        item.kubernetesSetupRequirements !== undefined &&
        (!requirements ||
          delivery?.kind !== "BROKER_IDENTITY" ||
          delivery.brokerPurpose !== "KUBERNETES_TARGET")
      )
        throw new Error("Personal Kubernetes export requirements are invalid");
      return {
        ...(requirements
          ? { kubernetesSetupRequirements: { ...requirements } }
          : {}),
        connectionRef: item.connectionRef,
        connectionVersion: item.connectionVersion,
        name: item.name,
        definitionKey: item.definitionKey,
        policy: item.policy,
        enabled: item.enabled,
        available: item.available,
        ...(delivery ? { credentialDelivery: delivery } : {}),
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
  const delivery = readCredentialDelivery(connection.credentialDelivery);
  if (!delivery || delivery.kind === "NONE")
    throw new Error(personalCredentialMetadataRefreshRequired);
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
  if (
    isSSHBrokerDefinition(connection.definitionKey) ||
    personalCredentialDelivery({ ...connection, credential })?.kind !== "TOKEN"
  )
    throw new Error(personalCredentialMetadataRefreshRequired);
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
  const delivery = personalCredentialDelivery({ ...connection, credential });
  if (
    isSSHBrokerDefinition(connection.definitionKey) &&
    (delivery?.kind !== "BROKER_IDENTITY" ||
      delivery.brokerPurpose !== "SSH_TARGET")
  )
    throw new Error(personalCredentialMetadataRefreshRequired);
  if (!delivery || delivery.kind === "NONE")
    throw new Error(personalCredentialMetadataRefreshRequired);
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
