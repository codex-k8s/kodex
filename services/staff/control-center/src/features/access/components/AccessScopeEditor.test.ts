import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import AccessScopeEditor from "@/features/access/components/AccessScopeEditor.vue";

const messages = {
  ru: {
    access: {
      scope: {
        kind: "Тип области",
        project: "Проект",
        chooseProject: "Выберите Проект",
        resourceKind: "Тип ресурса",
        chooseResource: "Выберите конкретный ресурс",
        exactResourceHint: "Backend повторно разрешает ресурс.",
        resourceRef: "Экземпляр",
        resourceRefPlaceholder: "ref",
        resourceRefHint: "Непрозрачная ссылка",
        contractBoundary: "Граница контракта",
        operationCondition: "Конкретная операция",
        operationConditionHint: "Операция задаётся полномочием.",
        values: {
          ORGANIZATION: "Организация",
          PROJECT: "Проект",
          RESOURCE_KIND: "Тип ресурсов",
          RESOURCE_INSTANCE: "Конкретный ресурс",
        },
      },
      resourceKinds: {
        ORGANIZATION: "Организация",
        PROJECT: "Проект",
        AGENT: "ИИ-сотрудник",
        WORKFLOW: "Процесс",
        RUN: "Запуск",
        OWNER_GATE: "Human Gate",
        ARTIFACT: "Файл",
        SCHEDULE: "Автоматизация",
        INTEGRATION: "Интеграция",
        RUNTIME_ENVIRONMENT: "Рабочее окружение",
        ROLE_IMAGE: "Образ роли",
        SESSION: "Сессия",
        SECRET: "Секрет",
      },
    },
  },
};

describe("AccessScopeEditor", () => {
  it("использует реальные workflow options без устаревшего environment blocker", async () => {
    const app = createSSRApp({
      render: () =>
        h(AccessScopeEditor, {
          modelValue: {
            kind: "RESOURCE_INSTANCE",
            projectRef: "project_sales",
            resourceKind: "WORKFLOW",
            resourceRef: "",
          },
          projects: [
            {
              ref: "project_sales",
              version: 1,
              name: "Продажи",
              purpose: "",
              language: "ru",
              lifecycle: "ACTIVE",
              agentCount: 0,
              integrationState: "NONE",
              workflowCount: 1,
              activeRunCount: 0,
              pendingGateCount: 0,
              createdAt: "2026-08-29T00:00:00Z",
              updatedAt: "2026-08-29T00:00:00Z",
              nextActions: [],
            },
          ],
          agents: [],
          workflows: [
            {
              ref: "workflow_leads",
              version: 1,
              projectRef: "project_sales",
              name: "Квалификация лида",
              purpose: "Проверить входящий лид",
              state: "PUBLISHED",
              cardSummary: {
                stageCount: 0,
                uniqueAgentCount: 0,
                parallelGroupCount: 0,
                hasHumanGate: false,
                activeRunCount: 0,
                pendingGateCount: 0,
              },
              launchReadiness: {
                allowedToSubmit: false,
                reason: "PERMISSION_REQUIRED",
                workflowVersion: 1,
                operationalState: "UNKNOWN",
                contextDigest: "a".repeat(64),
              },
              inputFields: [],
              steps: [],
              validationMessages: [],
              updatedAt: "2026-08-29T00:00:00Z",
              nextActions: [],
            },
          ],
          integrations: [],
        }),
    });
    app.use(
      createI18n({ legacy: false, locale: "ru", messages, missingWarn: false }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Квалификация лида");
    expect(html).not.toContain("Недоступно текущим API");
  });
});
