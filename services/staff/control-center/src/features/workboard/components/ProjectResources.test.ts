import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import ProjectResources from "@/features/workboard/components/ProjectResources.vue";
import type {
  Project,
  RuntimeEnvironmentSet,
  Schedule,
} from "@/shared/api/generated/openapi/types.gen";

const project: Project = {
  ref: "project_sales",
  version: 1,
  name: "Продажи",
  purpose: "Работа с клиентами",
  language: "ru",
  lifecycle: "ACTIVE",
  agentCount: 3,
  workflowCount: 2,
  activeRunCount: 7,
  pendingGateCount: 4,
  updatedAt: "2026-08-29T10:00:00Z",
  nextActions: [],
};

const schedule: Schedule = {
  ref: "schedule_daily",
  version: 1,
  projectRef: project.ref,
  name: "Ежедневная квалификация",
  target: {
    type: "AGENT",
    ref: "agent_sales",
    displayName: "Аналитик",
    version: 1,
  },
  state: "ACTIVE",
  preset: "DAILY",
  timezone: "Europe/Saratov",
  sessionPolicy: "NEW_EACH_RUN",
  notificationPolicy: "CONTROL_CENTER_ONLY",
  nextActions: [],
  currentRevision: {
    ref: "schedule_revision_daily",
    revision: 1,
    digest: "schedule-digest",
    name: "Ежедневная квалификация",
    target: {
      type: "AGENT",
      ref: "agent_sales",
      displayName: "Аналитик",
      version: 1,
    },
    preset: "DAILY",
    cronExpression: "0 9 * * *",
    timezone: "Europe/Saratov",
    input: {},
    sessionPolicy: "NEW_EACH_RUN",
    notificationPolicy: "CONTROL_CENTER_ONLY",
    createdAt: "2026-08-29T09:00:00Z",
  },
};

const environment: RuntimeEnvironmentSet = {
  ref: "environment_sales",
  version: 1,
  projectRef: project.ref,
  name: "Инструменты продаж",
  description: "gh, psql и redis-cli",
  state: "ACTIVE",
  currentVersion: {} as RuntimeEnvironmentSet["currentVersion"],
  updatedAt: "2026-08-29T10:00:00Z",
  ready: true,
  readinessBlockers: [],
  nextActions: [],
};

async function render(): Promise<string> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { render: () => null } }],
  });
  await router.push("/");
  await router.isReady();
  const app = createSSRApp({
    render: () =>
      h(ProjectResources, {
        project,
        schedules: [schedule],
        environments: [environment],
        schedulesReady: true,
        environmentsReady: true,
      }),
  });
  app.use(router);
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          common: { all: "Все", loading: "Загрузка", retry: "Повторить" },
          project: {
            agents: "Сотрудники",
            workflows: "Процессы",
            decisions: "Решения",
          },
          runs: { title: "Запуски" },
          files: { title: "Файлы" },
          automations: { title: "Автоматизации" },
          runtime: { environmentsTitle: "Окружения" },
          workboard: {
            projectCollections: "Ресурсы Проекта",
            resourceCount: "Всего: {count}",
            openCurrentWork: "Открыть текущую работу",
            openDecisions: "Открыть ожидающие решения",
            openCollection: "Открыть список",
            resourceUnavailable: "Недоступно",
            noAutomations: "Нет автоматизаций",
            noEnvironments: "Нет окружений",
            nextRun: "Следующий запуск: {date}",
            moreEnvironments: "Есть ещё окружения",
          },
          states: { ACTIVE: "Активна", READY: "Готово" },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("ProjectResources", () => {
  it("показывает реальные списки автоматизаций и окружений", async () => {
    const html = await render();

    expect(html).toContain("Ежедневная квалификация");
    expect(html).toContain("Инструменты продаж");
    expect(html).toContain("/projects/project_sales/automations");
    expect(html).toContain(
      "/projects/project_sales/environments/environment_sales",
    );
  });

  it("не превращает запуски, решения и файлы в KPI", async () => {
    const html = await render();

    expect(html).toContain("Открыть текущую работу");
    expect(html).toContain("Открыть ожидающие решения");
    expect(html).toContain("Открыть список");
    expect(html).not.toContain("Активных: 7");
    expect(html).not.toContain("Ожидают решения: 4");
  });
});
