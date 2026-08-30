import { describe, expect, it } from "vitest";

import {
  agentInitials,
  agentStatusTone,
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

  it("строго разбирает сохранённый режим каталога", () => {
    expect(parseAgentCatalogView("list")).toBe("list");
    expect(parseAgentCatalogView("unknown")).toBe("grid");
    expect(parseAgentCatalogView(null)).toBe("grid");
  });
});
