import { describe, expect, it } from "vitest";
import type { RouteLocationNormalizedLoaded } from "vue-router";

import {
  assistantContextIdentity,
  conversationMatchesContext,
  resolveAssistantContext,
  readableContextOperations,
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
  it("показывает только объявленные владельцем операции и не придумывает unknown", () => {
    expect(readableContextOperations(["LAUNCH_RUN"])).toEqual(["LAUNCH_RUN"]);
    expect(readableContextOperations([])).toEqual([]);
    expect(
      readableContextOperations(["LAUNCH_RUN", "UNKNOWN_COMMAND"]),
    ).toBeUndefined();
  });
  it("не меняет identity при version bump той же сущности", () => {
    const descriptor = {
      route: "/projects/prj_sales",
      entityKind: "PROJECT",
      entityRef: "prj_sales",
      entityName: "Продажи",
      allowedOperations: [],
    };

    expect(
      assistantContextIdentity(
        { ...descriptor, entityVersion: 1 },
        "prj_sales",
      ),
    ).toBe(
      assistantContextIdentity(
        { ...descriptor, entityVersion: 2 },
        "prj_sales",
      ),
    );
    expect(
      assistantContextIdentity(
        { ...descriptor, entityRef: "prj_other" },
        "prj_other",
      ),
    ).not.toBe(assistantContextIdentity(descriptor, "prj_sales"));
  });

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

  it.each([
    ["files", "FILE", "artifactRef", "file_1"],
    ["files-trash", "FILE", "artifactRef", "file_2"],
    ["organization-files", "FILE", "artifactRef", "file_3"],
    ["integrations", "INTEGRATION_CONNECTION", "connectionRef", "connection_1"],
  ])(
    "передаёт точный выбранный ресурс %s без выдуманных полномочий",
    (name, kind, key, ref) => {
      const current = route("/files", {});
      current.name = name;
      current.query = { [key]: ref };
      const value = resolveAssistantContext(current, sources);
      expect(value.projectRef).toBeUndefined();
      expect(value.descriptor).toEqual({
        route: "/files",
        entityKind: kind,
        entityRef: ref,
        entityName: "",
        allowedOperations: [],
      });
    },
  );

  it("связывает окружение с реальным route project, не подменяя его Project context", () => {
    const current = route("/projects/prj_sales/environments/env_1", {
      projectRef: "prj_sales",
      environmentRef: "env_1",
    });
    current.name = "runtime-environment";
    const value = resolveAssistantContext(current, sources);
    expect(value.projectRef).toBe("prj_sales");
    expect(value.descriptor.entityKind).toBe("ENVIRONMENT");
    expect(value.descriptor.entityRef).toBe("env_1");
    expect(value.descriptor.entityVersion).toBeUndefined();
  });

  it("не принимает неоднозначный query и не переносит выбор на другую страницу", () => {
    const current = route("/files", {});
    current.name = "organization-files";
    current.query = { artifactRef: ["file_1", "file_2"] };
    expect(
      resolveAssistantContext(current, sources).descriptor.entityKind,
    ).toBe("");
    current.name = "onboarding";
    current.query = { artifactRef: "file_1", connectionRef: "connection_1" };
    expect(
      resolveAssistantContext(current, sources).descriptor.entityKind,
    ).toBe("");
  });

  it("сопоставляет глобальный контекст со старым ответом без пустых scalar", () => {
    const current = resolveAssistantContext(
      route("/onboarding", {}),
      sources,
    ).descriptor;

    expect(conversationMatchesContext({ context: {} }, current)).toBe(true);
  });
});
