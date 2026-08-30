import { describe, expect, it } from "vitest";

import {
  buildIntegrationPackages,
  connectionAllows,
  filterIntegrationPackages,
  flattenIntegrationGrants,
  integrationCategories,
} from "@/features/integrations/ui/model";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";

function definition(
  key: string,
  overrides: Partial<IntegrationDefinition> = {},
): IntegrationDefinition {
  return {
    key,
    name: key,
    description: `Описание ${key}`,
    category: "development",
    builtIn: true,
    available: true,
    capabilities: [
      {
        key: `${key}.read`,
        name: "Чтение",
        description: "Чтение данных",
        risk: "READ",
        approvalRequired: false,
        operation: `${key}.read`,
        approvalPolicy: "NONE",
        resourceKind: "GITHUB_REPOSITORY",
        inputFields: [],
      },
    ],
    configurationFields: [],
    schemaVersion: "integrations.kodex.io/v1",
    definitionVersion: "1.0.0",
    origin: "SHIPPED",
    digest: "a".repeat(64),
    adapter: "GITHUB",
    ...overrides,
  };
}

function connection(
  ref: string,
  definitionKey: string,
  overrides: Partial<IntegrationConnection> = {},
): IntegrationConnection {
  return {
    ref,
    version: 1,
    definitionKey,
    name: ref,
    state: "CONNECTED",
    credentialsConfigured: true,
    credentialsHint: "configured",
    capabilities: definition(definitionKey).capabilities,
    grants: [],
    nextActions: [],
    definitionVersion: "1.0.0",
    definitionDigest: "a".repeat(64),
    publicConfiguration: {},
    ...overrides,
  };
}

describe("integrations presentation model", () => {
  it("считает подключения и не открывает create без server action", () => {
    const packages = buildIntegrationPackages(
      [
        definition("github"),
        definition("custom", { builtIn: false, available: false }),
      ],
      [
        connection("github-main", "github"),
        connection("github-off", "github", { state: "DISABLED" }),
      ],
      false,
    );

    expect(packages.map((item) => item.key)).toEqual(["github", "custom"]);
    expect(packages[0]).toMatchObject({
      connectionCount: 2,
      healthyConnectionCount: 1,
      canConnect: false,
    });
    expect(packages[1]?.canConnect).toBe(false);
  });

  it("фильтрует каталог по категории и capability", () => {
    const packages = buildIntegrationPackages(
      [
        definition("github"),
        definition("jira", {
          category: "tasks",
          capabilities: [
            {
              key: "jira.issue.read",
              name: "Задачи",
              description: "Поиск задач",
              risk: "READ",
              approvalRequired: false,
              operation: "jira.issue.read",
              approvalPolicy: "NONE",
              resourceKind: "GITHUB_REPOSITORY",
              inputFields: [],
            },
          ],
        }),
      ],
      [],
      true,
    );

    expect(integrationCategories(packages)).toEqual(["development", "tasks"]);
    expect(filterIntegrationPackages(packages, "поиск", "tasks")).toHaveLength(
      1,
    );
    expect(filterIntegrationPackages(packages, "поиск", "development")).toEqual(
      [],
    );
  });

  it("выводит grants из авторитетных connection readbacks", () => {
    const source = connection("github-main", "github", {
      grants: [
        {
          ref: "grant-workflow",
          version: 1,
          capabilityKey: "github.read",
          workflowRef: "workflow-release",
          targetName: "Проверка релиза",
          enabled: false,
          risk: "READ",
          approvalPolicy: "NONE",
          resourceScope: {
            kind: "GITHUB_REPOSITORY",
            values: { repository: "codex-k8s/kodex" },
            digest: "b".repeat(64),
          },
        },
        {
          ref: "grant-agent",
          version: 1,
          capabilityKey: "github.read",
          agentRef: "agent-release",
          targetName: "Инженер релизов",
          enabled: true,
          risk: "READ",
          approvalPolicy: "NONE",
          resourceScope: {
            kind: "GITHUB_REPOSITORY",
            values: { repository: "codex-k8s/kodex" },
            digest: "b".repeat(64),
          },
        },
      ],
    });

    const grants = flattenIntegrationGrants([source]);

    expect(grants.map((item) => item.targetKind)).toEqual([
      "AGENT",
      "WORKFLOW",
    ]);
    expect(grants[0]?.capabilityName).toBe("Чтение");
    expect(grants[0]).toMatchObject({
      resourceKind: "GITHUB_REPOSITORY",
      resourceValues: [{ key: "repository", value: "codex-k8s/kodex" }],
    });
  });

  it("разрешает команду только при exact nextAction", () => {
    const source = connection("github-main", "github", {
      nextActions: ["TEST"],
    });

    expect(connectionAllows(source, "TEST")).toBe(true);
    expect(connectionAllows(source, "DISABLE")).toBe(false);
  });
});
