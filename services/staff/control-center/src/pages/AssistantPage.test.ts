import { createPinia } from "pinia";
import { createSSRApp } from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "@vue/server-renderer";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import type { AssistantConversation } from "@/shared/api/generated/openapi";

import AssistantPage from "./AssistantPage.vue";

beforeAll(() => {
  vi.stubGlobal("window", {
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  });
});

afterAll(() => vi.unstubAllGlobals());

const messages = {
  common: {
    input: "Вы",
    edit: "Изменить",
    reject: "Отклонить",
    requestChanges: "Запросить изменения",
    retry: "Повторить",
    loading: "Загрузка",
    empty: "Нет данных",
    error: "Ошибка",
    unknownStatus: "Статус недоступен",
    status: "Состояние",
    result: "Результат",
    yes: "Да",
    no: "Нет",
  },
  app: { assistantShort: "Помощник" },
  assistant: {
    title: "Помощник Kodex",
    subtitle: "Настройка и выполнение работы",
    newConversation: "Новый диалог",
    ready: "Помощник готов",
    system: "Системный и неудаляемый",
    audit: "Все изменения записываются в аудит",
    empty: "Выберите пример или опишите задачу своими словами.",
    message: "Опишите, что нужно сделать",
    send: "Отправить",
    plan: "План изменений",
    applyPlan: "Применить изменения",
  },
  projects: {
    new: "Новый Проект",
    emptyTitle: "Создайте первый Проект",
  },
  agents: {
    new: "Новый сотрудник",
    emptyTitle: "Создайте первого сотрудника",
  },
  workflows: {
    new: "Новый Процесс",
    emptyTitle: "Создайте первый Процесс",
  },
  states: {
    READY: "Готов",
    COMPLETED: "Готово",
    WAITING_HUMAN: "Ждёт решения",
    APPLIED: "Применён",
    REJECTED: "Отклонён",
    UNAVAILABLE: "Недоступно",
    NEEDS_ATTENTION: "Требует внимания",
  },
};

function conversation(applied: boolean): AssistantConversation {
  return {
    ref: "cnv_assistant_test",
    version: 1,
    title: "Настройка отдела продаж",
    turns: [
      {
        ref: "trn_assistant_test",
        sequence: 1,
        role: "ASSISTANT",
        content:
          'Создать Проект. План: `pln_secret_ref`, версия 1. {"type":"CREATE_PROJECT"}',
        state: "COMPLETED",
        plan: {
          ref: "pln_secret_ref",
          version: 1,
          conversationRef: "cnv_assistant_test",
          auditSummary: "Будет создана рабочая область отдела продаж.",
          applied,
          nextActions: applied ? [] : ["APPLY_PLAN"],
          operations: [
            {
              ref: "op_secret_ref",
              type: "CREATE_PROJECT",
              title: "Создать Проект",
              summary: "Рабочая область для команды продаж.",
              permitted: true,
            },
            {
              ref: "op_unavailable_ref",
              type: "CREATE_INTEGRATION_CONNECTION",
              title: "Подключить CRM",
              summary: '{"status":"blocked","connection_ref":"con_secret_ref"}',
              permitted: false,
              unavailableReason: "Сначала настройте учётные данные.",
            },
          ],
        },
        createdAt: "2026-08-28T09:00:00Z",
      },
    ],
    updatedAt: "2026-08-28T09:00:00Z",
  };
}

async function renderAssistant(value?: AssistantConversation): Promise<string> {
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
  await router.push("/assistant");
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
    nextActions: ["OPEN", "CREATE_CONVERSATION", "ADD_TURN"],
  };
  if (value) platform.conversations[value.ref] = value;
  useRealtimeStore(pinia).platformState.state = "live";

  const app = createSSRApp(AssistantPage);
  app.use(pinia);
  app.use(router);
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: { ru: messages },
    }),
  );
  return renderToString(app);
}

describe("AssistantPage", () => {
  it("показывает ожидающий решения план одной типизированной карточкой", async () => {
    const html = await renderAssistant(conversation(false));

    expect(html).toContain("План изменений");
    expect(html).toContain("Будет создана рабочая область отдела продаж.");
    expect(html).toContain("Создать Проект");
    expect(html.match(/Создать Проект/g)).toHaveLength(1);
    expect(html).toContain("Подключить CRM");
    expect(html).toContain("Сначала настройте учётные данные.");
    expect(html).toContain("Ждёт решения");
    expect(html).toContain("Применить изменения");
    expect(html).toContain("Изменить");
    expect(html).toContain("Отклонить");
    expect(html).not.toContain("pln_secret_ref");
    expect(html).not.toContain("op_secret_ref");
    expect(html).not.toContain("con_secret_ref");
    expect(html).not.toContain("CREATE_PROJECT");
    expect(html).not.toContain("&quot;type&quot;");
    expect(html).not.toContain("&quot;status&quot;");
    expect(html).not.toContain(">WAITING_HUMAN<");
  });

  it("показывает применённый план без повторных действий", async () => {
    const html = await renderAssistant(conversation(true));

    expect(html).toContain("Применён");
    expect(html).not.toContain("Применить изменения");
    expect(html).not.toContain(">Изменить<");
    expect(html).not.toContain(">Отклонить<");
    expect(html).not.toContain(">APPLIED<");
  });

  it("предлагает типовые запросы в пустом диалоге", async () => {
    const html = await renderAssistant();

    expect(html).toContain(
      "Выберите пример или опишите задачу своими словами.",
    );
    expect(html).toContain("Новый Проект");
    expect(html).toContain("Новый сотрудник");
    expect(html).toContain("Новый Процесс");
  });
});
