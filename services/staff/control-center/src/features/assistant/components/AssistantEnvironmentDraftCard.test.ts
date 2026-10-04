import { readFileSync } from "node:fs";
import { nextTick, type ComputedRef, type Ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  AssistantPlan,
  RuntimeEnvironmentDraft,
} from "@/shared/api/generated/openapi/types.gen";
import { captureSetupState } from "@/test-utils/setup-harness";

const api = vi.hoisted(() => ({ read: vi.fn(), helper: vi.fn() }));
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
  };
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
});
