import sentry from "../../../../../contracts/integrations/v1/definitions/sentry.yaml?raw";
import sentrySelfHosted from "../../../../../contracts/integrations/v1/definitions/sentry-self-hosted.yaml?raw";
import sentryOrganization from "../../../../../contracts/integrations/v1/definitions/sentry-organization.yaml?raw";
import sentrySelfHostedOrganization from "../../../../../contracts/integrations/v1/definitions/sentry-self-hosted-organization.yaml?raw";
import prometheus from "../../../../../contracts/integrations/v1/definitions/prometheus.yaml?raw";
import thanos from "../../../../../contracts/integrations/v1/definitions/thanos.yaml?raw";
import grafanaDashboard from "../../../../../contracts/integrations/v1/definitions/grafana-dashboard.yaml?raw";
import grafanaOrganization from "../../../../../contracts/integrations/v1/definitions/grafana-organization.yaml?raw";
import grafanaAnnotations from "../../../../../contracts/integrations/v1/definitions/grafana-dashboard-annotations.yaml?raw";
import grafanaDelete from "../../../../../contracts/integrations/v1/definitions/grafana-organization-rule-delete.yaml?raw";
import loki from "../../../../../contracts/integrations/v1/definitions/loki.yaml?raw";
import lokiRulerRead from "../../../../../contracts/integrations/v1/definitions/loki-ruler-namespace-read.yaml?raw";
import lokiRuler from "../../../../../contracts/integrations/v1/definitions/loki-ruler-namespace.yaml?raw";
import { parseConfigurationDocument } from "../../src/features/managed-configurations/document";
import { packageDiagnostics } from "../../src/features/managed-configurations/integration-package";
import type {
  IntegrationCapability,
  IntegrationConfigurationField,
  IntegrationDefinition,
} from "../../src/shared/api/generated/openapi/types.gen";

interface ShippedField {
  key: string;
  label: string;
  help: string;
  type: "STRING" | "INTEGER" | "BOOLEAN";
  format?: string;
  required: boolean;
  minimum?: number;
  maximum?: number;
  maximumLength?: number;
  allowedValues?: string[];
}
interface ShippedPackage {
  apiVersion: string;
  metadata: { key: string; version: string };
  spec: {
    name: string;
    description: string;
    category: string;
    adapter: IntegrationDefinition["adapter"];
    credential: { secretKey: string };
    configurationFields: ShippedField[];
    capabilities: Array<{
      key: string;
      name: string;
      description: string;
      operation: string;
      risk: IntegrationCapability["risk"];
      approvalPolicy: IntegrationCapability["approvalPolicy"];
      allowedApprovalPolicies: IntegrationCapability["allowedApprovalPolicies"];
      inputFields: ShippedField[];
    }>;
  };
}

// Проекция synthetic HTTP metadata использует реальные shipped documents;
// authority/pins доказывает отдельная native owner→Gate→Begin component matrix.
function field(value: ShippedField): IntegrationConfigurationField {
  return {
    key: value.key,
    label: value.label,
    help: value.help,
    valueType:
      value.type === "BOOLEAN"
        ? "BOOLEAN"
        : value.type === "INTEGER"
          ? "INTEGER"
          : value.format === "HTTPS_ORIGIN"
            ? "URL"
            : "TEXT",
    required: value.required,
    allowedValues: value.allowedValues,
    minimum: value.minimum,
    maximum: value.maximum,
    maximumLength: value.maximumLength,
  };
}

export const observabilityDefinitions: IntegrationDefinition[] = [
  sentry,
  sentrySelfHosted,
  sentryOrganization,
  sentrySelfHostedOrganization,
  prometheus,
  thanos,
  grafanaDashboard,
  grafanaOrganization,
  grafanaAnnotations,
  grafanaDelete,
  loki,
  lokiRulerRead,
  lokiRuler,
].map((raw) => {
  const parsed = parseConfigurationDocument(raw, "YAML");
  if (packageDiagnostics(parsed).length)
    throw new Error("Synthetic shipped integration package is invalid");
  const source = parsed as unknown as ShippedPackage;
  return {
    key: source.metadata.key,
    name: source.spec.name,
    description: source.spec.description,
    category: source.spec.category,
    version: 1,
    nextActions: [],
    builtIn: true,
    available: true,
    schemaVersion: source.apiVersion,
    definitionVersion: source.metadata.version,
    digest: "a".repeat(64),
    origin: "SHIPPED",
    adapter: source.spec.adapter,
    adapterOwner: "integration-gateway",
    executionRoute: "MANAGED_MCP",
    adapterReadiness: "READY",
    credentialSecretKey: source.spec.credential.secretKey,
    connectionCount: 0,
    healthyConnectionCount: 0,
    configurationFields: source.spec.configurationFields.map(field),
    capabilities: source.spec.capabilities.map((capability) => ({
      key: capability.key,
      name: capability.name,
      description: capability.description,
      operation: capability.operation,
      risk: capability.risk,
      approvalPolicy: capability.approvalPolicy,
      allowedApprovalPolicies: capability.allowedApprovalPolicies,
      approvalRequired: capability.approvalPolicy !== "NONE",
      resourceKind: "HTTPS_RESOURCE",
      inputFields: capability.inputFields.map(field),
    })),
  };
});
