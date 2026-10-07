import { describe, expect, it } from "vitest";

import {
  buildIntegrationPackages,
  connectionAllows,
  filterIntegrationPackages,
  flattenIntegrationGrants,
  integrationCategories,
  isUnboundOpenAPITemplate,
  publicIntegrationConfiguration,
  allowedIntegrationApprovalPolicies,
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
    version: 1,
    nextActions: [],
    connectionCount: 0,
    healthyConnectionCount: 0,
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
        allowedApprovalPolicies: ["NONE"],
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
    adapterOwner: "integration-gateway",
    executionRoute: "MANAGED_MCP",
    adapterReadiness: "READY",
    ...overrides,
  };
}

describe("allowedIntegrationApprovalPolicies", () => {
  it("возвращает только авторитетный allowed set, без добавления fallback", () => {
    const capability = definition("github").capabilities[0];
    if (!capability) throw new Error("Capability fixture is missing");
    expect(allowedIntegrationApprovalPolicies(capability)).toEqual(["NONE"]);
    expect(
      allowedIntegrationApprovalPolicies({
        ...capability,
        approvalPolicy: "HUMAN_EACH_EFFECT",
        allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
      }),
    ).toEqual(["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"]);
    expect(allowedIntegrationApprovalPolicies(undefined)).toEqual([]);
  });

  it("закрыто отклоняет пустой, дублированный, неизвестный набор или default вне него", () => {
    const capability = definition("github").capabilities[0];
    if (!capability) throw new Error("Capability fixture is missing");
    expect(
      allowedIntegrationApprovalPolicies({
        ...capability,
        allowedApprovalPolicies: [],
      }),
    ).toEqual([]);
    expect(
      allowedIntegrationApprovalPolicies({
        ...capability,
        allowedApprovalPolicies: ["NONE", "NONE"],
      }),
    ).toEqual([]);
    expect(
      allowedIntegrationApprovalPolicies({
        ...capability,
        allowedApprovalPolicies: ["HUMAN_EACH_EFFECT"],
      }),
    ).toEqual([]);
    const malformed = { ...capability };
    Reflect.set(malformed, "allowedApprovalPolicies", ["NONE", "UNKNOWN"]);
    expect(allowedIntegrationApprovalPolicies(malformed)).toEqual([]);
    Reflect.deleteProperty(malformed, "allowedApprovalPolicies");
    expect(allowedIntegrationApprovalPolicies(malformed)).toEqual([]);
  });
});

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
  it("отличает поставленный OpenAPI-шаблон от привязанной ревизии владельца", () => {
    const shipped = definition("openapi-mcp");
    expect(
      isUnboundOpenAPITemplate(connection("template", "openapi-mcp"), shipped),
    ).toBe(true);
    expect(
      isUnboundOpenAPITemplate(
        connection("bound", "openapi-mcp", {
          definitionDigest: "b".repeat(64),
        }),
        shipped,
      ),
    ).toBe(false);
    expect(
      isUnboundOpenAPITemplate(
        connection("other", "github"),
        definition("github"),
      ),
    ).toBe(false);
  });
  it("считает подключения и не открывает create без server action", () => {
    const packages = buildIntegrationPackages(
      [
        definition("github", {
          connectionCount: 2,
          healthyConnectionCount: 1,
        }),
        definition("custom", { builtIn: false, available: false }),
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
              allowedApprovalPolicies: ["NONE"],
              resourceKind: "GITHUB_REPOSITORY",
              inputFields: [],
            },
          ],
        }),
      ],
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
    expect(grants[0]?.capabilityAvailable).toBe(true);
    expect(grants[0]).toMatchObject({
      resourceKind: "GITHUB_REPOSITORY",
      resourceValues: [{ key: "repository", value: "codex-k8s/kodex" }],
    });
  });

  it("не показывает внутренний key удалённой возможности как пользовательское имя", () => {
    const [grant] = flattenIntegrationGrants([
      connection("legacy", "github", {
        capabilities: [],
        grants: [
          {
            ref: "grant-legacy",
            version: 1,
            capabilityKey: "op.technical-reference",
            agentRef: "agent-release",
            targetName: "Инженер релизов",
            enabled: false,
            risk: "READ",
            approvalPolicy: "NONE",
            resourceScope: {
              kind: "HTTPS_RESOURCE",
              values: { url: "https://example.test" },
              digest: "b".repeat(64),
            },
          },
        ],
      }),
    ]);

    expect(grant).toMatchObject({
      capabilityKey: "op.technical-reference",
      capabilityName: "",
      capabilityAvailable: false,
    });
  });

  it("разрешает команду только при exact nextAction", () => {
    const source = connection("github-main", "github", {
      nextActions: ["TEST"],
    });

    expect(connectionAllows(source, "TEST")).toBe(true);
    expect(connectionAllows(source, "DISABLE")).toBe(false);
  });

  it("никогда не визуализирует secret-like поля из publicConfiguration", () => {
    const sourceDefinition = definition("github", {
      credentialSecretKey: "token",
      configurationFields: [
        {
          key: "owner",
          label: "Владелец",
          help: "Организация или пользователь",
          valueType: "TEXT",
          required: true,
        },
      ],
    });
    const source = connection("github-main", "github", {
      publicConfiguration: {
        owner: "example-org",
        token: "must-not-render",
        api_key: "must-not-render-either",
        enabled: true,
      },
    });

    expect(publicIntegrationConfiguration(source, sourceDefinition)).toEqual([
      { key: "enabled", label: "enabled", value: "true" },
      { key: "owner", label: "Владелец", value: "example-org" },
    ]);
    expect(
      JSON.stringify(publicIntegrationConfiguration(source, sourceDefinition)),
    ).not.toContain("must-not-render");
  });
});
