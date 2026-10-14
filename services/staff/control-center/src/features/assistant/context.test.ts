import { describe, expect, it, vi } from "vitest";
import type { RouteLocationNormalizedLoaded } from "vue-router";

import { i18n } from "@/app/i18n";
import {
  assistantContextIdentity,
  assistantContextOperations,
  assistantContextRouteLabelKey,
  assistantContextTitle,
  conversationMatchesContext,
  resolveAssistantContext,
  readableContextOperations,
} from "@/features/assistant/context";
import type {
  Agent,
  Project,
  RoleImageRecipe,
  Run,
  RuntimeEnvironmentSet,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

it.each(["ru", "en"] as const)(
  "явно переводит весь текущий реестр операций контекста в %s без fallback",
  (locale) => {
    for (const operation of assistantContextOperations) {
      const key = `assistant.contextOperation.${operation}`;
      expect(i18n.global.te(key, locale), key).toBe(true);
      const text = i18n.global.t(key, {}, { locale });
      expect(text.trim(), key).not.toBe("");
      expect(text, key).not.toBe(key);
      expect(text, key).not.toBe(operation);
    }
  },
);

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
  roleImages: {
    imgrec_1: {
      ref: "imgrec_1",
      projectRef: "prj_sales",
      name: "Проверка образа",
      version: 5,
    } as RoleImageRecipe,
  },
};

it("показывает поздно загруженное имя текущей сущности", () => {
  const current = {
    route: "/projects/prj-1/agents/agt-1",
    entityKind: "AGENT",
    entityRef: "agt-1",
    entityName: "Координатор продаж",
    allowedOperations: [],
  };

  expect(assistantContextTitle(current, { ...current, entityName: "" })).toBe(
    "Координатор продаж",
  );
});

describe("подписи экранов системного помощника", () => {
  it.each([
    [
      "system-assistant-environment",
      "/organization/assistant/environment?draftRef=draft_synthetic#tools",
      "Окружение помощника",
      "Assistant environment",
    ],
    [
      "system-role-images",
      "/organization/role-images?view=published",
      "Образы помощника",
      "Assistant images",
    ],
    [
      "system-role-image-new",
      "/organization/role-images/new",
      "Новый образ помощника",
      "New assistant image",
    ],
    [
      "system-role-image",
      "/organization/role-images/imgrec_synthetic?revisionRef=rev_synthetic",
      "Образ помощника",
      "Assistant image",
    ],
    [
      "system-runtime-secrets",
      "/organization/secrets?draftRef=draft_synthetic&planRef=plan_synthetic",
      "Секреты помощника",
      "Assistant secrets",
    ],
  ])(
    "%s показывает название без технических ссылок и сохраняет контекст",
    (name, fullPath, russianTitle, englishTitle) => {
      const current = route(fullPath, {});
      current.name = name;
      const descriptor = resolveAssistantContext(current, sources).descriptor;
      const before = structuredClone(descriptor);
      const identity = assistantContextIdentity(descriptor);
      const key = assistantContextRouteLabelKey(current.name);
      expect(key).toBeDefined();
      const previousLocale = i18n.global.locale.value;
      try {
        for (const locale of ["ru", "en"] as const) {
          i18n.global.locale.value = locale;
          expect(
            assistantContextTitle(
              descriptor,
              undefined,
              key ? i18n.global.t(key) : undefined,
            ),
          ).toBe(locale === "ru" ? russianTitle : englishTitle);
        }
      } finally {
        i18n.global.locale.value = previousLocale;
      }
      expect(descriptor).toEqual(before);
      expect(descriptor.route).toBe(fullPath);
      expect(assistantContextIdentity(descriptor)).toBe(identity);
      expect(descriptor.allowedOperations).toEqual([]);
    },
  );

  it("сохраняет приоритет имени ресурса над названием экрана", () => {
    const descriptor = resolveAssistantContext(
      route("/organization/role-images/imgrec_synthetic", {}),
      sources,
    ).descriptor;
    expect(
      assistantContextTitle(
        { ...descriptor, entityName: "Инструменты разработки" },
        { ...descriptor, entityName: "Сохранённое имя" },
        "Образ помощника",
      ),
    ).toBe("Инструменты разработки");
    expect(
      assistantContextTitle(
        descriptor,
        { ...descriptor, entityName: "Сохранённое имя" },
        "Образ помощника",
      ),
    ).toBe("Сохранённое имя");
  });

  it.each([undefined, "unknown-route", Symbol("route")])(
    "не назначает системную подпись неизвестному или проектному маршруту %s",
    (name) => {
      expect(assistantContextRouteLabelKey(name)).toBeUndefined();
      const descriptor = resolveAssistantContext(
        route("/projects/prj_sales/environments/env_synthetic", {}),
        sources,
      ).descriptor;
      expect(assistantContextTitle(descriptor)).toBe(descriptor.route);
    },
  );
});

describe("assistant route context", () => {
  const environmentRoute = () => {
    const current = route(
      "/projects/prj_sales/environments/env_1?draftRef=draft_1#image",
      {
        projectRef: "prj_sales",
        environmentRef: "env_1",
      },
    );
    current.name = "runtime-environment";
    return current;
  };
  const environment = {
    ref: "env_1",
    projectRef: "prj_sales",
    organizationRef: "org_sales",
    scopeKind: "PROJECT",
    name: "Окружение проверки",
    state: "ACTIVE",
    version: 12,
  } as RuntimeEnvironmentSet;

  it("показывает поздно загруженное exact имя окружения без изменения pins/identity/operations", () => {
    const current = environmentRoute();
    const missing = resolveAssistantContext(current, sources);
    const loaded = resolveAssistantContext(current, {
      ...sources,
      environments: { env_1: environment },
      organizationRef: "org_sales",
    });
    expect(loaded.descriptor.entityName).toBe("Окружение проверки");
    expect(assistantContextTitle(loaded.descriptor)).toBe("Окружение проверки");
    expect(assistantContextIdentity(loaded.descriptor, loaded.projectRef)).toBe(
      assistantContextIdentity(missing.descriptor, missing.projectRef),
    );
    expect({ ...loaded.descriptor, entityName: "" }).toEqual(
      missing.descriptor,
    );
    expect(loaded.descriptor.allowedOperations).toEqual([]);
    expect(loaded.descriptor.entityVersion).toBeUndefined();
    expect(loaded.descriptor.route).toBe(current.fullPath);
  });

  it.each([
    { ref: "env_other" },
    { projectRef: "prj_other" },
    { organizationRef: "org_other" },
    { scopeKind: "ORGANIZATION" as const },
    { state: "DELETED" as const },
  ])("не переносит имя чужого/удалённого/позднего окружения %j", (change) => {
    const value = resolveAssistantContext(environmentRoute(), {
      ...sources,
      environments: { env_1: { ...environment, ...change } },
      organizationRef: "org_sales",
    });
    expect(value.descriptor.entityName).toBe("");
    expect(value.descriptor.entityKind).toBe("ENVIRONMENT");
    expect(value.descriptor.entityRef).toBe("env_1");
    expect(value.descriptor.allowedOperations).toEqual([]);
  });

  it.each([undefined, "org_other"])(
    "без exact owner organization %s имя не показывается",
    (organizationRef) => {
      const value = resolveAssistantContext(environmentRoute(), {
        ...sources,
        environments: { env_1: environment },
        organizationRef,
      });
      expect(value.descriptor.entityName).toBe("");
    },
  );

  it("не показывает сохранённое имя при незавершённом или отказавшем current read", () => {
    const value = resolveAssistantContext(environmentRoute(), {
      ...sources,
      environments: { env_1: environment },
      organizationRef: "org_sales",
      environmentReadBlocked: true,
    });
    expect(value.descriptor.entityName).toBe("");
  });

  it("смена route не принимает позднее имя предыдущего окружения даже под новым cache key", () => {
    const current = environmentRoute();
    current.params.environmentRef = "env_next";
    current.fullPath = "/projects/prj_sales/environments/env_next";
    const value = resolveAssistantContext(current, {
      ...sources,
      environments: { env_1: environment, env_next: environment },
      organizationRef: "org_sales",
    });
    expect(value.descriptor.entityRef).toBe("env_next");
    expect(value.descriptor.entityName).toBe("");
  });

  it.each(["ru", "en"] as const)(
    "до загрузки имени окружения использует локализованную подпись в %s без URL",
    (locale) => {
      const current = environmentRoute();
      const value = resolveAssistantContext(current, sources);
      const key = assistantContextRouteLabelKey(current.name);
      expect(key).toBe("nav.environment");
      const label = i18n.global.t(key ?? "", {}, { locale });
      expect(assistantContextTitle(value.descriptor, undefined, label)).toBe(
        locale === "ru" ? "Окружение" : "Environment",
      );
      expect(value.descriptor.entityName).toBe("");
    },
  );

  it.each([
    "CREATE_PROJECT_ASSISTANT",
    "CREATE_INSTRUCTION_DRAFT",
    "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS",
  ])(
    "распознаёт серверную %s, не выдавая дополнительных операций",
    (operation) => {
      expect(readableContextOperations([operation])).toEqual([operation]);
      expect(
        readableContextOperations([operation, "UNKNOWN_COMMAND"]),
      ).toBeUndefined();
      expect(
        resolveAssistantContext(
          route("/projects/prj_sales/agents/agt_sales", {
            projectRef: "prj_sales",
            agentRef: "agt_sales",
          }),
          sources,
        ).descriptor.allowedOperations,
      ).toEqual([]);
    },
  );
  it("показывает только объявленные владельцем операции и не придумывает unknown", () => {
    expect(readableContextOperations(["LAUNCH_RUN"])).toEqual(["LAUNCH_RUN"]);
    expect(
      readableContextOperations(["CREATE_RUNTIME_ENVIRONMENT_DRAFT"]),
    ).toEqual(["CREATE_RUNTIME_ENVIRONMENT_DRAFT"]);
    expect(readableContextOperations(["CREATE_ROLE_IMAGE_RECIPE"])).toEqual([
      "CREATE_ROLE_IMAGE_RECIPE",
    ]);
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

  it("связывает рецепт образа с точным resource context и принимает авторитетное имя", () => {
    const current = route("/projects/prj_sales/role-images/imgrec_1", {
      projectRef: "prj_sales",
      recipeRef: "imgrec_1",
    });
    current.name = "role-image";
    const value = resolveAssistantContext(current, sources);
    expect(value.projectRef).toBe("prj_sales");
    expect(value.descriptor.entityKind).toBe("ROLE_IMAGE_RECIPE");
    expect(value.descriptor.entityRef).toBe("imgrec_1");
    expect(value.descriptor.entityName).toBe("Проверка образа");
    expect(value.descriptor.entityVersion).toBe(5);
    expect(assistantContextTitle(value.descriptor)).toBe("Проверка образа");
  });

  it("связывает выбранную автоматизацию с точным project context", () => {
    const current = route(
      "/projects/prj_sales/automations?scheduleRef=sch_daily",
      {
        projectRef: "prj_sales",
      },
    );
    current.name = "automations";
    current.query = { scheduleRef: "sch_daily" };
    const value = resolveAssistantContext(current, sources);
    expect(value.projectRef).toBe("prj_sales");
    expect(value.descriptor.entityKind).toBe("SCHEDULE");
    expect(value.descriptor.entityRef).toBe("sch_daily");
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
