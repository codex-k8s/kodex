import { describe, expect, it } from "vitest";

import { buildBreadcrumbs, type BreadcrumbLabels } from "@/app/breadcrumbs";

const labels: BreadcrumbLabels = {
  home: "Главная",
  onboarding: "Первичная настройка",
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
  environments: "Окружения",
  environment: "Окружение",
  newEnvironment: "Новое окружение",
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

  it("сохраняет контекст проекта в ссылке на запуск", () => {
    expect(
      buildBreadcrumbs(
        {
          routeName: "project-run",
          project: { ref: "project_sales", name: "Корпоративные продажи" },
          runName: "Квалификация лида",
        },
        labels,
      ),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      {
        label: "Корпоративные продажи",
        path: "/projects/project_sales",
      },
      { label: "Запуски", path: "/projects/project_sales/runs" },
      { label: "Квалификация лида" },
    ]);
  });

  it("сохраняет контекст проекта в маршруте редактора окружения", () => {
    expect(
      buildBreadcrumbs(
        {
          routeName: "runtime-environment-new",
          project: { ref: "project_sales", name: "Продажи" },
        },
        labels,
      ),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      { label: "Продажи", path: "/projects/project_sales" },
      {
        label: "Окружения",
        path: "/projects/project_sales/environments",
      },
      { label: "Новое окружение" },
    ]);
  });

  it("связывает новый запуск со списком запусков текущего Проекта", () => {
    expect(
      buildBreadcrumbs(
        {
          routeName: "new-run",
          project: { ref: "project_sales", name: "Продажи" },
        },
        labels,
      ),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      { label: "Продажи", path: "/projects/project_sales" },
      { label: "Запуски", path: "/projects/project_sales/runs" },
      { label: "Новый запуск" },
    ]);
  });
});
