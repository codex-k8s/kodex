import { createPinia } from "pinia";
import { createSSRApp } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { describe, expect, it } from "vitest";

import { usePlatformStore } from "@/features/platform/store";
import DecisionsPage from "@/pages/DecisionsPage.vue";
import type {
  AuditEvent,
  OwnerGate,
  Project,
  Run,
} from "@/shared/api/generated/openapi/types.gen";

const project: Project = {
  ref: "prj_sales",
  version: 1,
  name: "Продажи",
  purpose: "Работа с клиентами",
  language: "ru",
  lifecycle: "ACTIVE",
  agentCount: 1,
  workflowCount: 1,
  activeRunCount: 1,
  pendingGateCount: 1,
  updatedAt: "2026-08-29T10:00:00Z",
  nextActions: [],
};

const run: Run = {
  ref: "run_offer",
  version: 1,
  projectRef: project.ref,
  sessionRef: "ses_offer",
  rootRunRef: "run_offer",
  target: {
    type: "AGENT",
    ref: "agt_sales",
    displayName: "Менеджер продаж",
    version: 1,
  },
  title: "Согласование коммерческого предложения",
  titleSource: "USER_EDITED",
  activitySummary: "Ожидает решения владельца",
  state: "WAITING_HUMAN",
  source: "CONTROL_CENTER",
  initiator: { ref: "usr_owner", displayName: "Владелец" },
  attempt: 1,
  graphRevision: 2,
  lastEventSequence: 3,
  usage: {
    totalTokens: 0,
    inputTokens: 0,
    cachedInputTokens: 0,
    cacheWriteInputTokens: 0,
    outputTokens: 0,
    reasoningOutputTokens: 0,
    modelContextWindow: 0,
  },
  artifactRefs: [],
  gateRefs: ["gat_offer"],
  createdAt: "2026-08-29T10:00:00Z",
  nextActions: [],
};

const gate: OwnerGate = {
  ref: "gat_offer",
  version: 1,
  projectRef: project.ref,
  runRef: run.ref,
  nodeRef: "nod_offer_gate",
  title: "Утвердить отправку предложения",
  contextSummary: "Проверены цена, срок и состав работ.",
  consequencesSummary: "После одобрения агент отправит предложение клиенту.",
  requestedBy: { ref: "agt_sales", displayName: "Менеджер продаж" },
  state: "OPEN",
  allowedDecisions: ["APPROVE", "REQUEST_CHANGES", "REJECT"],
  openedAt: "2026-08-29T10:05:00Z",
  nextActions: ["RESOLVE_GATE"],
};

const auditEvent: AuditEvent = {
  ref: "aud_gate_opened",
  projectRef: project.ref,
  initiator: { ref: "usr_owner", displayName: "Владелец" },
  executor: "control-plane",
  source: "CONTROL_CENTER",
  action: "OWNER_GATE_OPENED",
  resourceType: "OWNER_GATE",
  resourceRef: gate.ref,
  resourceName: gate.title,
  outcome: "SUCCEEDED",
  safeSummary: "Запрос решения зарегистрирован",
  occurredAt: "2026-08-29T10:05:00Z",
};

describe("DecisionsPage", () => {
  it("показывает сгруппированный контекст и ведёт на точный узел запуска", async () => {
    const pinia = createPinia();
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/decisions", component: DecisionsPage },
        { path: "/:pathMatch(.*)*", component: { template: "<div />" } },
      ],
    });
    await router.push("/decisions");
    await router.isReady();

    const platform = usePlatformStore(pinia);
    platform.projects[project.ref] = project;
    platform.runs[run.ref] = run;
    platform.gates[gate.ref] = gate;
    platform.auditEvents = [auditEvent];
    const i18n = createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          app: { project: "Проект" },
          common: {
            approve: "Одобрить",
            reject: "Отклонить",
            requestChanges: "Запросить изменения",
            cancel: "Отменить",
            unknownStatus: "Неизвестно",
          },
          decisions: {
            title: "Решения",
            subtitle: "Вопросы, ожидающие ответа",
            pending: "Ожидают ответа",
            history: "История",
            projectFilter: "Проект",
            allProjects: "Все Проекты",
            pendingCount: "Ожидают ответа: {count}",
            historyCount: "В истории: {count}",
            emptyTitle: "Нет ожидающих решений",
            emptyText: "Вопросов нет",
            historyEmpty: "История решений пуста",
            historyEmptyText: "Завершённых решений нет",
            urgency: {
              OVERDUE: "Срок истёк",
              SOON: "Срочно",
              NORMAL: "Обычный приоритет",
            },
            projectUnavailable: "Название Проекта недоступно",
            runUnavailable: "Название запуска недоступно",
            question: "Решение человека",
            fullQuestion: "Что нужно решить",
            questionUnavailable: "Текст вопроса недоступен",
            consequences: "Что произойдёт",
            consequencesUnavailable: "Последствия недоступны",
            process: "Запуск и точный узел",
            openNode: "Открыть точный узел",
            requestedBy: "Запросил",
            openedAt: "Запрошено",
            deadline: "Срок ответа",
            noDeadline: "Без срока",
            evidence: "Материалы",
            evidenceCount: "Открыть материалы: {count}",
            noEvidence: "Материалы не приложены",
            comment: "Комментарий",
            commentPlaceholder: "Добавьте комментарий",
            outcome: "Принятое решение",
            actionsUnavailable: "Ответ недоступен",
            actionsUnavailableText: "Нет разрешённого действия",
          },
          states: {
            OPEN: "Открыто",
            CLEAN: "Проверен",
            APPROVED: "Одобрено",
            CHANGES_REQUESTED: "Нужны изменения",
            REJECTED: "Отклонено",
          },
        },
      },
    });

    const app = createSSRApp(DecisionsPage);
    app.use(pinia);
    app.use(router);
    app.use(i18n);
    const html = await renderToString(app);

    expect(html).toContain("Продажи");
    expect(html).toContain("Согласование коммерческого предложения");
    expect(html).toContain("Проверены цена, срок и состав работ.");
    expect(html).toContain(
      "После одобрения агент отправит предложение клиенту.",
    );
    expect(html).toContain("Менеджер продаж");
    expect(html).toContain("Согласование коммерческого предложения");
    expect(html).toContain("nod_offer_gate");
    expect(html).toContain("nodeRef=nod_offer_gate");
    expect(html).toContain("ses_offer");
    expect(html).toContain("Инициатор Run");
    expect(html).toContain("Запрос решения зарегистрирован");
    expect(html).toContain("OWNER_GATE_OPENED · SUCCEEDED");
    expect(html.match(/type="radio"/g)).toHaveLength(3);
    expect(html).toContain('data-state="APPROVED"');
    expect(html).toContain('data-state="CHANGES_REQUESTED"');
    expect(html).toContain('data-state="REJECTED"');
    expect(html.match(/Комментарий обязателен/g)).toHaveLength(2);
    expect(html.match(/button--primary/g)).toHaveLength(1);
    expect(html).toContain("Запросить изменения");
    expect(html).toContain("Отклонить");
  });
});
