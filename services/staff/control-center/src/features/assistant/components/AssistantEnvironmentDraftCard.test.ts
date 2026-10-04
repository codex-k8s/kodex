import { readFileSync } from "node:fs";
import { nextTick, type ComputedRef, type Ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  AssistantPlan,
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentDraft,
} from "@/shared/api/generated/openapi/types.gen";
import { captureSetupState } from "@/test-utils/setup-harness";

const api = vi.hoisted(() => ({
  read: vi.fn(),
  helper: vi.fn(),
  runtime: vi.fn(),
}));
vi.mock("@/features/agents/detail/runtime-api", () => ({
  loadAgentRuntime: api.runtime,
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({
    bootstrap: {
      organizationRef: "org_synthetic",
      assistant: { ref: "agt_system" },
    },
  }),
}));
vi.mock("@/features/runtime/environment-drafts", () => ({
  readEnvironmentDraft: api.read,
}));
vi.mock("@/features/assistant/project-helper-readback", () => ({
  readAssistantProjectHelper: api.helper,
}));
vi.mock("./AssistantEnvironmentBindingDialog.vue", () => ({ default: {} }));
import { i18n } from "@/app/i18n";
import Card from "./AssistantEnvironmentDraftCard.vue";

function plan(system: boolean): AssistantPlan {
  return {
    ref: "pln_synthetic",
    version: 2,
    revision: 1,
    state: "APPLIED",
    conversationRef: "conv_synthetic",
    ...(system ? {} : { projectRef: "prj_synthetic" }),
    operations: [
      {
        ref: "op_synthetic",
        type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
        action: "UPDATE",
        title: "Окружение",
        summary: "Подготовить окружение",
        target: {
          kind: "ENVIRONMENT",
          ref: "env_synthetic",
          name: "Окружение",
        },
        parameters: system ? { systemAssistantRef: "agt_system" } : {},
        before: {},
        after: {},
        selected: true,
        permitted: true,
        validationProblems: [],
      },
    ],
    auditSummary: "Окружение",
    applied: true,
    contentDigest: "a".repeat(64),
    validationProblems: [],
    nextActions: [],
    receipt: {
      ref: "rct_synthetic",
      planRef: "pln_synthetic",
      planRevision: 1,
      outcome: "APPLIED",
      operationReceipts: [
        {
          operationRef: "op_synthetic",
          resourceRef: "draft_synthetic",
          outcome: "APPLIED",
          auditRef: "aud_synthetic",
        },
      ],
      conflicts: [],
      auditRefs: [],
      createdResourceRefs: [],
      createdAt: "2026-10-04T10:00:00Z",
    },
  };
}

function draft(system: boolean, name: string): RuntimeEnvironmentDraft {
  return {
    scopeKind: system ? "ORGANIZATION" : "PROJECT",
    organizationRef: "org_synthetic",
    ref: "draft_synthetic",
    version: 1,
    projectRef: system ? "" : "prj_synthetic",
    environmentRef: "env_synthetic",
    expectedEnvironmentVersion: 2,
    state: "PUBLISHED",
    publishedEnvironmentRef: "env_synthetic",
    specification: {
      name,
      description: "",
      imageArtifactRef: "imgart_synthetic",
      tools: [],
      values: [],
      secretBindings: [],
    },
    diagnostics: [],
  };
}

async function readCard(system: boolean, name: string) {
  api.read.mockResolvedValue(draft(system, name));
  const state = await captureSetupState(Card, (app) => app.use(i18n), {
    plan: plan(system),
    operationRef: "op_synthetic",
  });
  // SSR закрывает watcher lifetime; отдельно подаётся typed readback для
  // presentation, не моделируется разрешение server-side owner boundary.
  (state.draft as Ref<RuntimeEnvironmentDraft | undefined>).value = draft(
    system,
    name,
  );
  await nextTick();
  return state as {
    draftTitle: ComputedRef<string>;
    draftName: ComputedRef<string>;
    systemBindingMessage: ComputedRef<string | undefined>;
    systemBinding: Ref<AgentRuntimeConfigurationView | undefined>;
    systemBindingUnavailable: Ref<boolean>;
    readSystemBinding(
      assistantRef: string,
      scope: { kind: "ORGANIZATION"; organizationRef: string },
      signal: AbortSignal,
    ): Promise<void>;
  };
}

const scope = {
  kind: "ORGANIZATION",
  organizationRef: "org_synthetic",
} as const;
function runtime(
  environmentRef = "env_synthetic",
): AgentRuntimeConfigurationView {
  return {
    configuration: { agentRef: "agt_system" },
    environmentBinding: {
      agentRef: "agt_system",
      environmentRef,
      versionRef: "renvv_effective",
    },
    environment: {
      ref: environmentRef,
      scopeKind: "ORGANIZATION",
      organizationRef: scope.organizationRef,
      projectRef: "",
      currentVersion: { ref: "renvv_effective", revision: 7 },
    },
  } as AgentRuntimeConfigurationView;
}

describe("Карточка применённого окружения", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    i18n.global.locale.value = "ru";
  });

  it("template использует локализованную presentation, не сырой DTO", () => {
    const source = readFileSync(
      new URL("./AssistantEnvironmentDraftCard.vue", import.meta.url),
      "utf8",
    );
    expect(source).toContain("<strong>{{ draftTitle }}</strong>");
    expect(source).toContain("<p>{{ draftName }}</p>");
    expect(source).not.toContain("{{ draft.specification.name }}");
  });

  it.each(["ru", "en"] as const)(
    "переводит server name и показывает SYSTEM scope в %s",
    async (locale) => {
      i18n.global.locale.value = locale;
      const state = await readCard(true, "i18n:DEFAULT_RUNTIME_ENVIRONMENT");
      expect(state.draftTitle.value).toBe(
        locale === "ru" ? "Общесистемное окружение" : "System environment",
      );
      expect(state.draftName.value).toBe(
        locale === "ru" ? "Основное окружение" : "Default environment",
      );
    },
  );

  it("сохраняет PROJECT presentation и literal owner name", async () => {
    const state = await readCard(false, "SYSTEM project workspace");
    expect(state.draftTitle.value).toBe("Окружение сотрудника");
    expect(state.draftName.value).toBe("SYSTEM project workspace");
  });

  it("не показывает неизвестный server token и обновляет locale без нового запроса", async () => {
    const state = await readCard(true, "i18n:UNKNOWN_ENVIRONMENT");
    expect(state.draftName.value).toBe(
      i18n.global.t("serverMessages.unsupported"),
    );
    expect(state.draftName.value).not.toContain("i18n:");
    const requests = api.read.mock.calls.length;
    i18n.global.locale.value = "en";
    await nextTick();
    expect(state.draftTitle.value).toBe("System environment");
    expect(state.draftName.value).toBe(
      i18n.global.t("serverMessages.unsupported"),
    );
    expect(api.read).toHaveBeenCalledTimes(requests);
  });

  it.each(["ru", "en"] as const)(
    "SYSTEM published показывает фактическое назначение и effective version в %s, не выдумывает binding mode",
    async (locale) => {
      const state = await readCard(true, "Окружение");
      i18n.global.locale.value = locale;
      api.runtime.mockResolvedValue(runtime());
      state.systemBindingUnavailable.value = false;
      await state.readSystemBinding(
        "agt_system",
        scope,
        new AbortController().signal,
      );
      expect(state.systemBindingMessage.value).toBe(
        i18n.global.t("assistant.environmentDraft.systemBound", {
          revision: 7,
        }),
      );
      expect(i18n.global.t("assistant.environmentDraft.published")).not.toMatch(
        /Привяжите|Bind it|follow.current/i,
      );
      expect(state.systemBindingMessage.value).not.toContain("renvv_effective");
      expect(state.systemBindingMessage.value).not.toMatch(/follow.current/i);
      expect(api.runtime).toHaveBeenLastCalledWith(
        "agt_system",
        expect.any(AbortSignal),
      );
    },
  );

  it("другое фактически назначенное окружение не объявляется привязанным к опубликованному draft", async () => {
    const state = await readCard(true, "Окружение");
    api.runtime.mockResolvedValue(runtime("env_other"));
    state.systemBindingUnavailable.value = false;
    await state.readSystemBinding(
      "agt_system",
      scope,
      new AbortController().signal,
    );
    expect(state.systemBindingMessage.value).toBe(
      i18n.global.t("assistant.environmentDraft.systemBindingChanged"),
    );
  });

  it("ошибка protected read и чужой/mismatched tuple дают только нейтральное неподтверждённое состояние", async () => {
    const current = runtime();
    for (const invalid of [
      {
        ...current,
        configuration: { ...current.configuration, agentRef: "agt_foreign" },
      },
      {
        ...current,
        environmentBinding: {
          ...current.environmentBinding,
          agentRef: "agt_foreign",
        },
      },
      {
        ...current,
        environmentBinding: {
          ...current.environmentBinding,
          environmentRef: "env_foreign",
        },
      },
      {
        ...current,
        environmentBinding: {
          ...current.environmentBinding,
          versionRef: "renvv_other",
        },
      },
      {
        ...current,
        environment: { ...current.environment, organizationRef: "org_foreign" },
      },
      {
        ...current,
        environment: {
          ...current.environment,
          scopeKind: "PROJECT",
          projectRef: "prj_synthetic",
        },
      },
      {
        ...current,
        environment: {
          ...current.environment,
          currentVersion: {
            ...current.environment.currentVersion,
            revision: 0,
          },
        },
      },
      undefined,
    ]) {
      const state = await readCard(true, "Окружение");
      if (invalid) api.runtime.mockResolvedValue(invalid);
      else api.runtime.mockRejectedValue(new Error("PRIVATE_SENTINEL"));
      await state.readSystemBinding(
        "agt_system",
        scope,
        new AbortController().signal,
      );
      expect(state.systemBinding.value).toBeUndefined();
      expect(state.systemBindingMessage.value).toBe(
        i18n.global.t("assistant.environmentDraft.bindingUnavailable"),
      );
      expect(state.systemBindingMessage.value).not.toContain(
        "PRIVATE_SENTINEL",
      );
      expect(state.systemBindingMessage.value).not.toContain("Привяжите");
    }
  });

  it("поздний protected read после закрытия lifetime не обновляет presentation", async () => {
    const state = await readCard(true, "Окружение");
    state.systemBindingUnavailable.value = false;
    let resolve: ((value: AgentRuntimeConfigurationView) => void) | undefined;
    api.runtime.mockReturnValue(
      new Promise<AgentRuntimeConfigurationView>((done) => {
        resolve = done;
      }),
    );
    const controller = new AbortController();
    const read = state.readSystemBinding(
      "agt_system",
      scope,
      controller.signal,
    );
    controller.abort();
    resolve?.(runtime());
    await read;
    expect(state.systemBinding.value).toBeUndefined();
    expect(state.systemBindingUnavailable.value).toBe(false);
    const calls = api.runtime.mock.calls.length;
    await state.readSystemBinding("agt_system", scope, controller.signal);
    expect(api.runtime).toHaveBeenCalledTimes(calls);
  });

  it("PROJECT published не заявляет назначение SYSTEM и сохраняет отдельную форму назначения", async () => {
    const state = await readCard(false, "Окружение проекта");
    expect(state.systemBindingMessage.value).toBeUndefined();
    expect(api.runtime).not.toHaveBeenCalled();
  });
});
