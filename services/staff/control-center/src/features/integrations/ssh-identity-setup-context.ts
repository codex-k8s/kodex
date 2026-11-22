import type {
  IntegrationConnection,
  PersonalIntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import type { SSHSetupContext } from "./ssh-identity-setup-api";
import {
  personalCredentialDelivery,
  readCredentialDelivery,
} from "./credential-delivery";

// Metadata выбирают только отдельную форму. Actor, target и permission остаются
// owner-derived; известный purpose не подтверждает доступ либо readiness SSH.
// Имя definition ограничивает противоречивый metadata route; положительный
// setup дополнительно требует exact BROKER_IDENTITY/SSH_TARGET.
export function isSSHBrokerDefinition(key: string): boolean {
  return key === "ssh-managed" || key === "ssh-execute";
}

export function personalSSHSetupContext(
  connection: PersonalIntegrationConnection,
): SSHSetupContext | undefined {
  const delivery = personalCredentialDelivery(connection);
  const binding = connection.credential;
  if (
    !isSSHBrokerDefinition(connection.definitionKey) ||
    delivery?.kind !== "BROKER_IDENTITY" ||
    delivery.brokerPurpose !== "SSH_TARGET" ||
    connection.policy !== "INITIATOR_REQUIRED" ||
    !connection.available ||
    typeof connection.connectionRef !== "string" ||
    !/^[A-Za-z0-9_-]{8,96}$/.test(connection.connectionRef) ||
    !Number.isSafeInteger(connection.connectionVersion) ||
    connection.connectionVersion < 1 ||
    (binding &&
      (binding.policy !== "INITIATOR_REQUIRED" ||
        binding.connectionRef !== connection.connectionRef ||
        binding.connectionVersion !== connection.connectionVersion ||
        ![
          "NOT_CONFIGURED",
          "CHECK_REQUIRED",
          "READY",
          "INVALID",
          "REVOKED",
          "EXPIRED",
        ].includes(binding.state) ||
        !Number.isSafeInteger(binding.version) ||
        binding.version < 1 ||
        !Number.isSafeInteger(binding.bindingGeneration) ||
        !binding.bindingGeneration ||
        binding.bindingGeneration < 1))
  )
    return;
  return {
    connectionRef: connection.connectionRef,
    connectionVersion: connection.connectionVersion,
    bindingVersion: binding?.version ?? 0,
    policy: "INITIATOR_REQUIRED",
  };
}

// Общая форма получает только свежий shared Connection snapshot. SERVICE,
// target и право manager выбираются повторно владельцем, не этим action hint.
export function sharedSSHSetupContext(
  connection: IntegrationConnection,
): SSHSetupContext | undefined {
  const delivery = readCredentialDelivery(connection.credentialDelivery);
  if (
    !isSSHBrokerDefinition(connection.definitionKey) ||
    delivery?.kind !== "BROKER_IDENTITY" ||
    delivery.brokerPurpose !== "SSH_TARGET" ||
    connection.credentialPolicy !== "PROJECT_SHARED" ||
    typeof connection.ref !== "string" ||
    !/^[A-Za-z0-9_-]{8,96}$/.test(connection.ref) ||
    !Number.isSafeInteger(connection.version) ||
    connection.version < 1 ||
    !Array.isArray(connection.nextActions) ||
    !connection.nextActions.includes("CONFIGURE_CREDENTIAL") ||
    !["CONNECTED", "DISABLED", "DEGRADED", "NOT_CONNECTED"].includes(
      connection.state,
    )
  )
    return;
  return {
    connectionRef: connection.ref,
    connectionVersion: connection.version,
    bindingVersion: 0,
    policy: "PROJECT_SHARED",
  };
}
