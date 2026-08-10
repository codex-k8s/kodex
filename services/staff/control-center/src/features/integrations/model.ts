import type {
  IntegrationApproval,
  IntegrationConfiguration,
  IntegrationDefinition,
  IntegrationTestReceipt,
  ProviderConnection,
} from "@/shared/api/generated/openapi/types.gen";

export interface IntegrationDefinitionModel {
  definitionRef: string;
  version: number;
  digestSha256: string;
  displayName: string;
  state: string;
  capabilities: Array<{
    name: string;
    risk: string;
    requiresApproval: boolean;
  }>;
}

export interface IntegrationView {
  configurationRef: string;
  stableKey: string;
  version: number;
  digestSha256: string;
  definitionRef: string;
  definitionVersion: number;
  definitionDigestSha256: string;
  connectionRef: string;
  connectionVersion: number;
  connectionGeneration: number;
  capabilities: string[];
  effectKind: "MCP_TOOL" | "CLI" | "ENVIRONMENT";
  state: string;
}

export interface IntegrationApprovalModel {
  approvalRef: string;
  version: number;
  status: string;
  requestHash: string;
  redactedPreview: { summary: string; fields: string[] };
}

export interface IntegrationConnectionModel {
  connectionRef: string;
  displayName: string;
  version: number;
  generation: number;
  state: string;
  maskedAccount: string;
  capabilities: string[];
}

export interface IntegrationTestModel {
  testRef: string;
  category: string;
  testedAt: string;
}

export const toIntegrationDefinitionModel = (
  value: IntegrationDefinition,
): IntegrationDefinitionModel => ({
  definitionRef: value.definitionRef,
  version: value.version,
  digestSha256: value.digestSha256,
  displayName: value.displayName,
  state: value.state,
  capabilities: value.capabilities.map((item) => ({
    name: item.name,
    risk: item.risk,
    requiresApproval: item.requiresApproval,
  })),
});

export const toIntegrationView = (
  value: IntegrationConfiguration,
): IntegrationView => ({
  configurationRef: value.configurationRef,
  stableKey: value.stableKey,
  version: value.version,
  digestSha256: value.digestSha256,
  definitionRef: value.definitionRef,
  definitionVersion: value.definitionVersion,
  definitionDigestSha256: value.definitionDigestSha256,
  connectionRef: value.connectionRef,
  connectionVersion: value.connectionVersion,
  connectionGeneration: value.connectionGeneration,
  capabilities: [...value.capabilities],
  effectKind: value.effectKind,
  state: value.state,
});

export const toIntegrationApprovalModel = (
  value: IntegrationApproval,
): IntegrationApprovalModel => ({
  approvalRef: value.approvalRef,
  version: value.version,
  status: value.status,
  requestHash: value.requestHash,
  redactedPreview: {
    summary: value.redactedPreview.summary,
    fields: [...value.redactedPreview.fields],
  },
});

export const toIntegrationConnectionModel = (
  value: ProviderConnection,
): IntegrationConnectionModel => ({
  connectionRef: value.connectionRef,
  displayName: value.displayName,
  version: value.version,
  generation: value.generation,
  state: value.state,
  maskedAccount: value.maskedAccount,
  capabilities: [...value.capabilities],
});

export const toIntegrationTestModel = (
  value: IntegrationTestReceipt,
): IntegrationTestModel => ({
  testRef: value.testRef,
  category: value.category,
  testedAt: value.testedAt,
});
