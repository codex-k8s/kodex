import { describe, expect, it } from "vitest";

import {
  agentInitials,
  agentStatusTone,
  availableAgentRoles,
  availableAgentStates,
  filterAgentCatalog,
  parseAgentCatalogView,
  sameOriginAvatarUrl,
  toAgentCatalogItem,
} from "@/features/agents/catalog/model";
import type { Agent } from "@/shared/api/generated/openapi/types.gen";

function agent(overrides: Partial<Agent> = {}): Agent {
  return {
    ref: "agent_sales",
    version: 1,
    projectRef: "project_sales",
    roleDefinitionName: "Аналитик",
    name: "Аналитик продаж",
    purpose: "Проверяет входящие обращения",
    roleDescription: "Сопоставляет факты и отмечает допущения",
    avatarUrl: "https://images.example/agent.png",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_standard",
    runtimeName: "Стандартный runtime",
    runtimeRevision: "rev-4",
    runtimeProvider: "openai",
    runtimeModel: "gpt-5",
    runtimeReady: true,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: "2026-08-28T10:00:00Z",
    nextActions: [],
    ...overrides,
  };
}

describe("agent catalog model", () => {
  it("строит устойчивый fallback и не использует внешний URL аватара", () => {
    const item = toAgentCatalogItem(agent());

    expect(item.initials).toBe("АП");
    expect(item.avatarUrl).toBeUndefined();
    expect(item.statusTone).toBe("success");
    expect(
      sameOriginAvatarUrl(
        "/api/v1/artifacts/art_avatar01/content?purpose=PREVIEW",
      ),
    ).toBe("/api/v1/artifacts/art_avatar01/content?purpose=PREVIEW");
    expect(
      sameOriginAvatarUrl("/api/v1/avatars/agent_sales?v=2"),
    ).toBeUndefined();
    expect(sameOriginAvatarUrl("//images.example/agent.png")).toBeUndefined();
    expect(sameOriginAvatarUrl("/\\images.example/agent.png")).toBeUndefined();
    expect(agentInitials("   ")).toBe("AI");
    expect(agentStatusTone("RUNNING")).toBe("accent");
    expect(agentStatusTone("DISABLED")).toBe("neutral");
  });

  it("фильтрует только уже загруженную модель и ищет по содержательным полям", () => {
    const source = [
      toAgentCatalogItem(agent({ ref: "agent_sales", name: "Яна" })),
      toAgentCatalogItem(
        agent({
          ref: "agent_docs",
          name: "Борис",
          purpose: "Готовит документы",
          roleDefinitionName: "Редактор",
          state: "RUNNING",
        }),
      ),
    ];

    expect(
      filterAgentCatalog(source, {
        query: "ДОКУМЕНТЫ",
        role: "Редактор",
        state: "RUNNING",
      }).map((item) => item.ref),
    ).toEqual(["agent_docs"]);
    expect(
      filterAgentCatalog(source, { query: "", role: "", state: "ALL" }).map(
        (item) => item.name,
      ),
    ).toEqual(["Борис", "Яна"]);
    expect(source.map((item) => item.name)).toEqual(["Яна", "Борис"]);
  });

  it("выводит только доступные состояния и роли в стабильном порядке", () => {
    const items = [
      toAgentCatalogItem(agent({ roleDefinitionName: "Редактор" })),
      toAgentCatalogItem(
        agent({ state: "RUNNING", roleDefinitionName: "Аналитик" }),
      ),
      toAgentCatalogItem(
        agent({ state: "READY", roleDefinitionName: undefined }),
      ),
    ];

    expect(availableAgentStates(items)).toEqual(["RUNNING", "READY"]);
    expect(availableAgentRoles(items)).toEqual(["Аналитик", "Редактор"]);
    expect(parseAgentCatalogView("list")).toBe("list");
    expect(parseAgentCatalogView("unknown")).toBe("grid");
    expect(parseAgentCatalogView(null)).toBe("grid");
  });
});
