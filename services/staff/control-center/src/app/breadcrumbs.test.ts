import { describe, expect, it } from "vitest";

import { buildBreadcrumbs, type BreadcrumbLabels } from "@/app/breadcrumbs";

const labels: BreadcrumbLabels = {
  home: "Главная",
  onboarding: "Первичная настройка",
  assistant: "Помощник Kodex",
  projects: "Проекты",
  project: "Проект",
  agents: "ИИ-сотрудники",
  agent: "ИИ-сотрудник",
  workflows: "Процессы",
  workflow: "Процесс",
  newRun: "Новый запуск",
  runs: "Запуски",
  run: "Запуск",
  files: "Файлы и знания",
  automations: "Автоматизации",
  integrations: "Интеграции",
  decisions: "Решения",
  administration: "Администрирование",
  access: "Участники и доступ",
  audit: "Аудит и диагностика",
};

describe("breadcrumbs", () => {
  it("показывает полный путь к сотруднику без технического locator", () => {
    expect(
      buildBreadcrumbs(
        {
          routeName: "agent",
          project: { ref: "project_sales", name: "Корпоративные продажи" },
          agentName: "Аналитик лидов",
        },
        labels,
      ),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      {
        label: "Корпоративные продажи",
        path: "/projects/project_sales",
      },
      {
        label: "ИИ-сотрудники",
        path: "/projects/project_sales/agents",
      },
      { label: "Аналитик лидов" },
    ]);
  });

  it("использует понятное имя сущности до загрузки readback", () => {
    const result = buildBreadcrumbs({ routeName: "run" }, labels);
    expect(result.at(-1)).toEqual({ label: "Запуск" });
    expect(JSON.stringify(result)).not.toContain("run_");
  });

  it("связывает аудит с разделом администрирования", () => {
    expect(buildBreadcrumbs({ routeName: "audit" }, labels)).toEqual([
      { label: "Администрирование", path: "/administration" },
      { label: "Аудит и диагностика" },
    ]);
  });
});
