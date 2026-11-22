import { describe, expect, it } from "vitest";
import type {
  IntegrationCapability,
  IntegrationConnection,
  IntegrationGrant,
} from "@/shared/api/generated/openapi/types.gen";
import {
  kubernetesGrantScopeFields,
  validKubernetesGrantNamespace,
} from "./kubernetes-grant-scope";

const capability: IntegrationCapability = {
  key: "kubernetes.get",
  name: "Прочитать объект",
  description: "Точное чтение",
  operation: "kubernetes.get",
  risk: "READ",
  approvalRequired: false,
  approvalPolicy: "NONE",
  allowedApprovalPolicies: ["NONE"],
  resourceKind: "KUBERNETES_TARGET",
  inputFields: [],
};
function grant(
  namespace: string,
  ref: string,
  agentRef = "agent_example",
): IntegrationGrant {
  return {
    ref,
    version: 1,
    capabilityKey: capability.key,
    agentRef,
    targetName: "Сотрудник",
    enabled: true,
    risk: "READ",
    approvalPolicy: "NONE",
    resourceScope: {
      kind: "KUBERNETES_TARGET",
      values: { target_ref: "target_example", namespace },
      digest: "a".repeat(64),
    },
  };
}
const connection: IntegrationConnection = {
  ref: "connection_example",
  version: 9,
  name: "Кластер",
  definitionKey: "kubernetes-managed",
  definitionVersion: "1.0.0",
  definitionDigest: "a".repeat(64),
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  credentialPolicy: "INITIATOR_REQUIRED",
  credentialDelivery: {
    kind: "BROKER_IDENTITY",
    brokerPurpose: "KUBERNETES_TARGET",
  },
  capabilities: [capability],
  grants: [
    grant("first", "grant_first"),
    grant("second", "grant_second"),
    grant("first", "grant_foreign", "other_agent"),
  ],
  nextActions: ["MANAGE_GRANTS"],
  publicConfiguration: { target_ref: "target_example" },
};

describe("Kubernetes grant scope", () => {
  it("выбирает ref только той namespace и получателя из owner snapshot", () => {
    for (const [namespace, expected] of [
      ["first", "grant_first"],
      ["second", "grant_second"],
    ] as const) {
      expect(
        kubernetesGrantScopeFields(
          connection,
          capability,
          namespace,
          "AGENT",
          "agent_example",
        ),
      ).toEqual({
        kubernetesNamespace: namespace,
        kubernetesGrantRef: expected,
      });
    }
    expect(
      kubernetesGrantScopeFields(
        connection,
        capability,
        "third",
        "AGENT",
        "agent_example",
      ),
    ).toEqual({ kubernetesNamespace: "third" });
    expect(
      kubernetesGrantScopeFields(
        connection,
        capability,
        "first",
        "WORKFLOW",
        "agent_example",
      ),
    ).toEqual({ kubernetesNamespace: "first" });
  });
  it("не усыновляет неоднозначный ref и не нормализует чужую область", () => {
    const duplicate = {
      ...connection,
      grants: [...connection.grants, grant("first", "grant_duplicate")],
    };
    expect(
      kubernetesGrantScopeFields(
        duplicate,
        capability,
        "first",
        "AGENT",
        "agent_example",
      ),
    ).toBeUndefined();
    for (const namespace of [
      "",
      "*",
      "First",
      "first ",
      " first",
      "first/second",
      "@all-namespaces",
      "a".repeat(64),
    ])
      expect(
        kubernetesGrantScopeFields(
          connection,
          capability,
          namespace,
          "AGENT",
          "agent_example",
        ),
      ).toBeUndefined();
    expect(
      kubernetesGrantScopeFields(
        { ...connection, credentialDelivery: undefined },
        capability,
        "first",
        "AGENT",
        "agent_example",
      ),
    ).toBeUndefined();
  });
  it("all-namespaces доступен лишь двум отдельным READ operations", () => {
    for (const operation of [
      "kubernetes.list.all-namespaces",
      "kubernetes.watch.all-namespaces",
    ]) {
      expect(validKubernetesGrantNamespace(operation, "@all-namespaces")).toBe(
        true,
      );
      expect(validKubernetesGrantNamespace(operation, "@cluster")).toBe(false);
      expect(validKubernetesGrantNamespace(operation, "first")).toBe(false);
    }
    expect(
      validKubernetesGrantNamespace("kubernetes.list", "@all-namespaces"),
    ).toBe(false);
    expect(validKubernetesGrantNamespace("kubernetes.patch", "@cluster")).toBe(
      true,
    );
  });
  it("non-Kubernetes payload остаётся без новых fields", () => {
    const other = { ...capability, resourceKind: "GITHUB_SCOPE" as const };
    expect(
      kubernetesGrantScopeFields(
        connection,
        other,
        "",
        "AGENT",
        "agent_example",
      ),
    ).toEqual({});
    expect(
      kubernetesGrantScopeFields(
        connection,
        other,
        "first",
        "AGENT",
        "agent_example",
      ),
    ).toBeUndefined();
    expect(
      kubernetesGrantScopeFields(
        undefined,
        capability,
        "first",
        "AGENT",
        "agent_example",
      ),
    ).toBeUndefined();
  });
});
