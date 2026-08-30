import type {
  IntegrationConnection,
  IntegrationDefinition,
  IntegrationGrant,
  NextAction,
} from "@/shared/api/generated/openapi/types.gen";

export type IntegrationsSection =
  | "CONNECTIONS"
  | "CATALOG"
  | "GRANTS"
  | "APPROVALS";

export interface IntegrationPackagePresentation {
  key: string;
  name: string;
  description: string;
  category: string;
  builtIn: boolean;
  available: boolean;
  capabilityCount: number;
  approvalCapabilityCount: number;
  connectionCount: number;
  healthyConnectionCount: number;
  canConnect: boolean;
  definition: IntegrationDefinition;
}

export interface IntegrationGrantPresentation {
  ref: string;
  connectionRef: string;
  connectionName: string;
  capabilityKey: string;
  capabilityName: string;
  targetName: string;
  targetKind: "AGENT" | "WORKFLOW" | "UNKNOWN";
  enabled: boolean;
  resourceKind: IntegrationGrant["resourceScope"]["kind"];
  resourceValues: Array<{ key: string; value: string }>;
  grant: IntegrationGrant;
  connection: IntegrationConnection;
}

export interface IntegrationPublicConfigurationEntry {
  key: string;
  label: string;
  value: string;
}

const sensitiveConfigurationKey =
  /(^|_)(secret|token|password|credential|api_key)(_|$)/i;

export function publicIntegrationConfiguration(
  connection: IntegrationConnection,
  definition?: IntegrationDefinition,
): IntegrationPublicConfigurationEntry[] {
  const credentialKey = definition?.credentialSecretKey?.trim().toLowerCase();
  return Object.entries(connection.publicConfiguration)
    .filter(([key]) => {
      const normalized = key.trim().toLowerCase();
      return (
        normalized !== credentialKey && !sensitiveConfigurationKey.test(key)
      );
    })
    .map(([key, value]) => {
      const field = definition?.configurationFields.find(
        (item) => item.key === key,
      );
      const rendered = Array.isArray(value)
        ? value.map(String).join(", ")
        : typeof value === "string" || typeof value === "number"
          ? String(value)
          : typeof value === "boolean"
            ? value
              ? "true"
              : "false"
            : "—";
      return {
        key,
        label: field?.label ?? key,
        value: rendered.length > 160 ? rendered.slice(0, 157) + "…" : rendered,
      };
    })
    .sort((left, right) => left.label.localeCompare(right.label));
}

export function buildIntegrationPackages(
  definitions: readonly IntegrationDefinition[],
  connections: readonly IntegrationConnection[],
  canCreateConnection: boolean,
): IntegrationPackagePresentation[] {
  return definitions
    .map((definition): IntegrationPackagePresentation => {
      const packageConnections = connections.filter(
        (connection) => connection.definitionKey === definition.key,
      );
      return {
        key: definition.key,
        name: definition.name,
        description: definition.description,
        category: definition.category,
        builtIn: definition.builtIn,
        available: definition.available,
        capabilityCount: definition.capabilities.length,
        approvalCapabilityCount: definition.capabilities.filter(
          (capability) => capability.approvalRequired,
        ).length,
        connectionCount: packageConnections.length,
        healthyConnectionCount: packageConnections.filter(
          (connection) => connection.state === "CONNECTED",
        ).length,
        canConnect: canCreateConnection && definition.available,
        definition,
      };
    })
    .sort((left, right) => {
      if (left.builtIn !== right.builtIn) return left.builtIn ? -1 : 1;
      return left.name.localeCompare(right.name);
    });
}

export function integrationCategories(
  packages: readonly IntegrationPackagePresentation[],
): string[] {
  return [
    ...new Set(packages.map((item) => item.category).filter(Boolean)),
  ].sort((left, right) => left.localeCompare(right));
}

export function filterIntegrationPackages(
  packages: readonly IntegrationPackagePresentation[],
  search: string,
  category?: string,
): IntegrationPackagePresentation[] {
  const normalized = search.trim().toLocaleLowerCase();
  return packages.filter((item) => {
    if (category && item.category !== category) return false;
    if (!normalized) return true;
    return [
      item.name,
      item.description,
      item.category,
      ...item.definition.capabilities.flatMap((capability) => [
        capability.name,
        capability.description,
        capability.key,
      ]),
    ].some((value) => value.toLocaleLowerCase().includes(normalized));
  });
}

export function flattenIntegrationGrants(
  connections: readonly IntegrationConnection[],
): IntegrationGrantPresentation[] {
  return connections
    .flatMap((connection) =>
      connection.grants.map((grant) => ({
        ref: grant.ref,
        connectionRef: connection.ref,
        connectionName: connection.name,
        capabilityKey: grant.capabilityKey,
        capabilityName:
          connection.capabilities.find(
            (capability) => capability.key === grant.capabilityKey,
          )?.name ?? grant.capabilityKey,
        targetName: grant.targetName,
        targetKind: grant.agentRef
          ? ("AGENT" as const)
          : grant.workflowRef
            ? ("WORKFLOW" as const)
            : ("UNKNOWN" as const),
        enabled: grant.enabled,
        resourceKind: grant.resourceScope.kind,
        resourceValues: Object.entries(grant.resourceScope.values)
          .map(([key, value]) => ({ key, value }))
          .sort((left, right) => left.key.localeCompare(right.key)),
        grant,
        connection,
      })),
    )
    .sort(
      (left, right) =>
        Number(right.enabled) - Number(left.enabled) ||
        left.targetName.localeCompare(right.targetName),
    );
}

export function connectionAllows(
  connection: IntegrationConnection,
  action: NextAction,
): boolean {
  return connection.nextActions.includes(action);
}
