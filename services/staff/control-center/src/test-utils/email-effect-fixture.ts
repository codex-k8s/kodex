import type {
  IntegrationConnection,
  EmailEffectReceiptView,
} from "../shared/api/generated/openapi/types.gen";

// Создаёт прежние независимые connection и UNKNOWN_OUTCOME receipt для синтетических email
// сценариев. Сначала собирает connection с переданными ref/version/name, затем связывает receipt
// с его ref; все массивы и объекты создаются заново, без OAuth, HTTP или реальных credentials.
export function syntheticEmailEffectFixture(
  connectionRef: string,
  connectionVersion: number,
  connectionName: string,
): { connection: IntegrationConnection; view: EmailEffectReceiptView } {
  const connection: IntegrationConnection = {
    ref: connectionRef,
    version: connectionVersion,
    definitionKey: "email",
    name: connectionName,
    state: "CONNECTED",
    credentialsConfigured: true,
    credentialsHint: "",
    capabilities: [],
    grants: [],
    nextActions: [],
    definitionVersion: "1",
    definitionDigest: "a".repeat(64),
    publicConfiguration: {},
  };
  const view: EmailEffectReceiptView = {
    receipt: {
      ref: "receipt_synthetic",
      version: 7,
      invocationRef: "invocation_synthetic",
      externalReceiptDigest: "b".repeat(64),
      semanticInputDigest: "c".repeat(64),
      outcome: "UNKNOWN_OUTCOME",
      mailboxRef: "mailbox_synthetic",
      configurationRevision: 4,
      connectionRef: connection.ref,
      projectRef: "project_synthetic",
      createdAt: "2026-09-05T00:00:00Z",
      updatedAt: "2026-09-05T00:00:00Z",
    },
  };
  return { connection, view };
}
