import { createPinia } from "pinia";
import { createSSRApp, h, type Component } from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "@vue/server-renderer";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import AssistantPage from "@/pages/AssistantPage.vue";
import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

beforeAll(() => {
  vi.stubGlobal("window", {
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  });
});

afterAll(() => vi.unstubAllGlobals());

function messages() {
  return {
    common: {
      loading: "Загрузка",
      empty: "Нет данных",
      error: "Ошибка",
      retry: "Повторить",
      close: "Закрыть",
      input: "Вы",
      edit: "Изменить",
      reject: "Отклонить",
      requestChanges: "Запросить изменения",
      unknownStatus: "Статус недоступен",
    },
    app: { assistantShort: "Помощник" },
    assistant: {
      title: "Помощник Kodex",
      subtitle: "Настройка и выполнение работы",
      newConversation: "Новый диалог",
      ready: "Помощник готов",
      system: "Системный и неудаляемый",
      audit: "Все изменения записываются в аудит",
      empty: "Опишите задачу",
      message: "Опишите, что нужно сделать",
      send: "Отправить помощнику",
      plan: "План изменений",
      applyPlan: "Применить разрешённые изменения",
    },
    errors: {
      STALE_VERSION: "Данные изменились. Показано актуальное состояние.",
      ACCESS_DENIED: "Недостаточно прав для просмотра.",
      default: "Не удалось выполнить действие.",
    },
    states: {
      READY: "Готов",
      COMPLETED: "Готово",
      WAITING_HUMAN: "Ждёт решения",
    },
  };
}

async function render(
  component: Component,
  props: Record<string, unknown> = {},
): Promise<string> {
  const app = createSSRApp({
    render: () => h(component, props),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: { ru: messages() },
    }),
  );
  return renderToString(app);
}

describe("authoritative UI states and accessibility", () => {
  it("объявляет loading и empty состояния assistive technology", async () => {
    const loading = await render(AsyncState, { loading: true });
    expect(loading).toContain('role="status"');
    expect(loading).toContain('aria-label="Загрузка"');

    const empty = await render(AsyncState, {
      empty: true,
      emptyTitle: "Сотрудников пока нет",
      emptyText: "Создайте первого сотрудника",
    });
    expect(empty).toContain("<h2>Сотрудников пока нет</h2>");
    expect(empty).toContain("Создайте первого сотрудника");
  });

  it.each([
    [409, "STALE_VERSION", "Данные изменились"],
    [403, "ACCESS_DENIED", "Недостаточно прав"],
  ])(
    "показывает локализованную безопасную ошибку %s",
    async (status, code, expected) => {
      const problem = new AppProblem({
        status,
        code,
        retryable: true,
        kind: status === 403 ? "forbidden" : "conflict",
        detail: "raw-provider-secret-must-not-render",
      });
      const html = await render(AsyncState, { problem });

      expect(html).toContain('role="alert"');
      expect(html).toContain(expected);
      expect(html).toContain("Повторить");
      expect(html).not.toContain("raw-provider-secret-must-not-render");
    },
  );

  it("использует безопасный локализованный title из API", async () => {
    const problem = new AppProblem({
      status: 503,
      code: "UNAVAILABLE",
      retryable: true,
      kind: "unavailable",
      title: "Сервис временно недоступен",
      detail: "raw-provider-secret-must-not-render",
    });
    const html = await render(AsyncState, { problem });

    expect(html).toContain("Сервис временно недоступен");
    expect(html).not.toContain("raw-provider-secret-must-not-render");
  });

  it("задаёт dialog semantics и блокирует закрытие busy-формы", async () => {
    const html = await render(ModalDialog, {
      title: "Новый сотрудник",
      busy: true,
    });

    expect(html).toContain('role="dialog"');
    expect(html).toContain('aria-modal="true"');
    expect(html).toContain('aria-label="Новый сотрудник"');
    expect(html).toContain('tabindex="-1"');
    expect(html).toMatch(/<button[^>]*aria-label="Закрыть"[^>]*disabled/);
  });

  it("показывает системного помощника полноценным live region без delete CTA", async () => {
    const pinia = createPinia();
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: "/assistant",
          component: AssistantPage,
          meta: { public: true },
        },
      ],
    });
    await router.push("/assistant?projectRef=project_sales01");
    await router.isReady();
    const platform = usePlatformStore(pinia);
    platform.assistant = {
      ref: "agent_system_assistant",
      version: 1,
      name: "Помощник Kodex",
      system: true,
      removable: false,
      corePromptRevision: "core-v1",
      ownerInstructions: "",
      runtimeState: "READY",
      readinessSummary: "Готов принимать задания",
      nextActions: ["OPEN", "CREATE_CONVERSATION", "ADD_TURN", "EDIT"],
    };
    platform.conversations.cnv_test12345678 = {
      ref: "cnv_test12345678",
      version: 1,
      title: "Настройка проекта",
      turns: [
        {
          ref: "trn_test12345678",
          sequence: 1,
          role: "ASSISTANT",
          content: "План: `pln_test12345678`, версия 1",
          state: "COMPLETED",
          plan: {
            ref: "pln_test12345678",
            version: 1,
            conversationRef: "cnv_test12345678",
            auditSummary: "Будет создан один проект",
            applied: false,
            nextActions: ["APPLY_PLAN"],
            operations: [
              {
                ref: "op_test12345678",
                type: "CREATE_PROJECT",
                title: "Создать проект",
                summary: "Новая рабочая область",
                permitted: true,
              },
            ],
          },
          createdAt: "2026-08-27T17:00:00Z",
        },
      ],
      updatedAt: "2026-08-27T17:00:00Z",
    };
    useRealtimeStore(pinia).platformState.state = "live";
    const app = createSSRApp(AssistantPage);
    app.use(pinia);
    app.use(router);
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: messages() },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Помощник готов");
    expect(html).toContain("Системный и неудаляемый");
    expect(html).toContain('role="log"');
    expect(html).toContain('aria-live="polite"');
    expect(html).toContain("Новый диалог");
    expect(html).toContain("Отправить помощнику");
    expect(html).toContain("Будет создан один проект");
    expect(html).toContain("Создать проект");
    expect(html).toContain("Применить разрешённые изменения");
    expect(html).not.toContain("pln_test12345678");
    expect(html).not.toMatch(/Удалить|Архивировать|Отключить/);
  });
});
