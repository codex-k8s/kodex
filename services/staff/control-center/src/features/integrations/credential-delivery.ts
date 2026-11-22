import type {
  IntegrationCredentialDelivery,
  PersonalIntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";

// Диагностика не содержит identity, provider text либо credential values.
export const personalCredentialMetadataRefreshRequired =
  "Personal integration credential metadata requires refresh";

// Отсутствие либо неизвестный вариант остаётся UNKNOWN. Эта проверка metadata
// не выбирает identity и не является допуском к setup или внешнему вызову.
export function readCredentialDelivery(
  value: unknown,
): IntegrationCredentialDelivery | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return;
  const record = value as Record<string, unknown>;
  if (
    (record.kind === "NONE" || record.kind === "TOKEN") &&
    Object.keys(record).length === 1
  )
    return { kind: record.kind };
  if (
    record.kind === "BROKER_IDENTITY" &&
    Object.keys(record).length === 2 &&
    (record.brokerPurpose === "KUBERNETES_TARGET" ||
      record.brokerPurpose === "SSH_TARGET")
  )
    return { kind: "BROKER_IDENTITY", brokerPurpose: record.brokerPurpose };
}

// Нужен свежий известный вид Connection и, при наличии binding, тот же вид
// binding. Metadata старого receipt без kind не превращаются в TOKEN/NONE;
// пользователь обновляет авторитетное представление до следующего setup/test.
export function personalCredentialDelivery(
  connection: PersonalIntegrationConnection,
): IntegrationCredentialDelivery | undefined {
  const current = readCredentialDelivery(connection.credentialDelivery);
  if (!current) return;
  if (connection.credential) {
    const binding = readCredentialDelivery(
      connection.credential.credentialDelivery,
    );
    if (
      !binding ||
      binding.kind !== current.kind ||
      (current.kind === "BROKER_IDENTITY" &&
        (binding.kind !== "BROKER_IDENTITY" ||
          binding.brokerPurpose !== current.brokerPurpose))
    )
      return;
  }
  return current;
}
