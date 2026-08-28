import { createPinia } from "pinia";
import { createSSRApp, h } from "vue";
import { renderToString, type SSRContext } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";

import ContextualAssistant from "./ContextualAssistant.vue";

beforeAll(() => {
  vi.stubGlobal("window", {
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  });
});

afterAll(() => vi.unstubAllGlobals());

describe("ContextualAssistant", () => {
  it("показывает контекст экрана и доступный live region в едином drawer", async () => {
    const pinia = createPinia();
    const platform = usePlatformStore(pinia);
    platform.assistant = {
      ref: "agent_system_assistant",
      version: 1,
      name: "Kodex",
      system: true,
      removable: false,
      corePromptRevision: "core-v1",
      ownerInstructions: "",
      runtimeState: "READY",
      readinessSummary: "Ready",
      nextActions: ["OPEN", "CREATE_CONVERSATION", "ADD_TURN"],
    };
    platform.loadAssistant = vi.fn().mockResolvedValue(undefined);
    useRealtimeStore(pinia).platformState.state = "live";

    const app = createSSRApp({
      render: () =>
        h(ContextualAssistant, {
          open: true,
          screenTitle: "ИИ-сотрудник",
          contextSummary: "Проект: Продажи · ИИ-сотрудник: Аналитик лидов",
          projectRef: "project_sales",
        }),
    });
    app.use(pinia);
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            common: {
              close: "Закрыть",
              input: "Вы",
              requestChanges: "Запросить изменения",
              retry: "Повторить",
              unknownStatus: "Статус недоступен",
            },
            assistant: {
              title: "Kodex",
              context: "Контекст",
              contextReady: "Готов работать с текущим экраном",
              contextHelp: "Опишите изменение",
              newConversation: "Новый диалог",
              plan: "План изменений",
              applyPlan: "Применить план",
              message: "Опишите задачу",
              voiceUnavailable: "Голосовой ввод пока недоступен",
              send: "Отправить",
            },
            states: { READY: "Готов" },
          },
        },
      }),
    );
    const context: SSRContext = {};

    await renderToString(app, context);
    const html = context.teleports?.body ?? "";

    expect(html).toContain('role="dialog"');
    expect(html).toContain('role="log"');
    expect(html).toContain('aria-live="polite"');
    expect(html).toContain("Проект: Продажи");
    expect(html).toContain("Новый диалог");
    expect(html).toContain("Kodex");
    expect(html).not.toMatch(/Удалить|Архивировать/);
  });
});
