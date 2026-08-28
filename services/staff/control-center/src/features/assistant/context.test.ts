import { describe, expect, it } from "vitest";
import type { RouteLocationNormalizedLoaded } from "vue-router";

import {
  conversationMatchesContext,
  resolveAssistantContext,
} from "@/features/assistant/context";
import type {
  Agent,
  Project,
  Run,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";

function route(
  fullPath: string,
  params: Record<string, string>,
): RouteLocationNormalizedLoaded {
  return { fullPath, params } as unknown as RouteLocationNormalizedLoaded;
}

const sources = {
  projects: {
    prj_sales: { ref: "prj_sales", name: "Продажи", version: 4 } as Project,
  },
  agents: {
    agt_sales: {
      ref: "agt_sales",
      name: "Координатор продаж",
      version: 7,
    } as Agent,
  },
  workflows: {
    wfl_sales: {
      ref: "wfl_sales",
      name: "Квалификация лида",
      version: 3,
    } as Workflow,
  },
  runs: {
    run_sales: {
      ref: "run_sales",
      projectRef: "prj_sales",
      title: "Квалификация заявки",
      version: 9,
    } as Run,
  },
};

describe("assistant route context", () => {
  it("связывает страницу сотрудника с точным agent и project", () => {
    const value = resolveAssistantContext(
      route("/projects/prj_sales/agents/agt_sales", {
        projectRef: "prj_sales",
        agentRef: "agt_sales",
      }),
      sources,
    );

    expect(value).toEqual({
      projectRef: "prj_sales",
      descriptor: {
        route: "/projects/prj_sales/agents/agt_sales",
        entityKind: "AGENT",
        entityRef: "agt_sales",
        entityName: "Координатор продаж",
        entityVersion: 7,
        allowedOperations: [],
      },
    });
  });

  it("получает project запуска из авторитетной frontend-проекции", () => {
    const value = resolveAssistantContext(
      route("/runs/run_sales", { runRef: "run_sales" }),
      sources,
    );

    expect(value.projectRef).toBe("prj_sales");
    expect(value.descriptor.entityKind).toBe("RUN");
    expect(value.descriptor.entityName).toBe("Квалификация заявки");
    expect(value.descriptor.entityVersion).toBe(9);
  });

  it("не продолжает диалог из другого resource context", () => {
    const current = resolveAssistantContext(
      route("/projects/prj_sales/workflows/wfl_sales", {
        projectRef: "prj_sales",
        workflowRef: "wfl_sales",
      }),
      sources,
    ).descriptor;

    expect(
      conversationMatchesContext(
        { context: { entityKind: "AGENT", entityRef: "agt_sales" } },
        current,
      ),
    ).toBe(false);
    expect(
      conversationMatchesContext(
        { context: { entityKind: "WORKFLOW", entityRef: "wfl_sales" } },
        current,
      ),
    ).toBe(true);
  });
});
