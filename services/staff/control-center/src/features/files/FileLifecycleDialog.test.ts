import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import FileLifecycleDialog from "@/features/files/FileLifecycleDialog.vue";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

const artifact = {
  ref: "artifact_contract",
  version: 3,
  projectRef: "project_sales",
  fileName: "contract.pdf",
  mediaType: "application/pdf",
  sizeBytes: 1024,
  digest: `sha256:${"a".repeat(64)}`,
  scanState: "CLEAN",
  source: "CONTROL_CENTER",
  revision: 3,
  lifecycleState: "ACTIVE",
  agentBindings: ["agent_sales"],
  previewAvailable: false,
  createdAt: "2026-08-30T06:00:00Z",
  nextActions: ["DELETE"],
} as Artifact;

describe("FileLifecycleDialog", () => {
  it("показывает затронутый запуск и блокирует удаление", async () => {
    const app = createSSRApp({
      render: () =>
        h(FileLifecycleDialog, {
          action: "DELETE",
          artifact,
          labels: {
            cancel: "Отмена",
            confirm: {
              DELETE: "В корзину",
              PURGE: "Удалить навсегда",
              RESTORE: "Восстановить",
            },
            description: {
              DELETE: "Файл перестанет выдаваться новым запускам.",
              PURGE: "Точная версия будет удалена.",
              RESTORE: "Файл будет восстановлен.",
            },
            impact: {
              activeRuns: "Активные запуски",
              activeRunsTruncated: "Показаны только первые запуски.",
              attachments: "Неизменяемые вложения",
              bindings: "Привязки к сотрудникам",
              openRun: "Открыть и отменить",
              summary: "Влияние удаления",
            },
            impactBlocked: "Сначала отмените затронутый запуск.",
            impactUnavailable: "Не удалось выполнить проверку.",
            reason: {
              ACTION_NOT_ALLOWED: "Действие недоступно.",
              IMPACT_BLOCKED: "Файл используется активным запуском.",
              IMPACT_UNAVAILABLE: "Проверка недоступна.",
            },
            title: {
              DELETE: "Переместить файл в корзину?",
              PURGE: "Удалить файл навсегда?",
              RESTORE: "Восстановить файл?",
            },
          },
          state: {
            action: "DELETE",
            available: true,
            impact: {
              action: "DELETE",
              activeRuns: [
                {
                  projectRef: "project_sales",
                  runRef: "run_contract_review",
                  state: "RUNNING",
                  title: "Проверка договора",
                },
              ],
              activeRunsTruncated: false,
              activeRuntimeCount: 1,
              artifactRef: artifact.ref,
              artifactVersion: artifact.version,
              attachmentCount: 1,
              bindingCount: 1,
              blockers: [],
              impactDigest: "b".repeat(64),
              permitted: true,
            },
          },
        }),
    });
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          component: { render: () => null },
          path: "/",
        },
        {
          component: { render: () => null },
          path: "/projects/:projectRef/runs/:runRef",
        },
      ],
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: { common: { close: "Закрыть" } } },
      }),
    );
    app.use(router);
    await router.push("/");
    await router.isReady();

    const html = await renderToString(app);

    expect(html).toContain("Проверка договора");
    expect(html).toContain("Открыть и отменить");
    expect(html).toContain("/projects/project_sales/runs/run_contract_review");
    expect(html).not.toContain("disabled");
    expect(html).not.toContain("ACTIVE_RUN_USES_ARTIFACT");
  });
});
