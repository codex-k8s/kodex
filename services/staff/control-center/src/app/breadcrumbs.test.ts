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
  filesTrash: "Корзина",
  skills: "Навыки",
  skill: "Навык",
  newSkill: "Новый навык",
  memory: "Память Kodex",
  memoryEntry: "Запись памяти",
  newMemory: "Новая запись памяти",
  automations: "Автоматизации",
  environments: "Окружения",
  environment: "Окружение",
  newEnvironment: "Новое окружение",
  secrets: "Секреты",
  members: "Участники",
  roleImages: "Образы ИИ-сотрудников",
  roleImage: "Образ ИИ-сотрудника",
  newRoleImage: "Новый образ",
  integrations: "Интеграции",
  decisions: "Решения",
  administration: "Администрирование",
  access: "Участники и доступ",
  audit: "Аудит и диагностика",
  providers: "Учётные записи моделей",
};

describe("breadcrumbs", () => {
  it.each([
    ["organization-agents", "ИИ-сотрудники"],
    ["organization-workflows", "Процессы"],
    ["organization-files", "Файлы и знания"],
    ["organization-automations", "Автоматизации"],
    ["organization-environments", "Окружения"],
    ["organization-secrets", "Секреты"],
    ["organization-members", "Участники"],
  ])("показывает глобальный каталог %s", (routeName, label) => {
    expect(buildBreadcrumbs({ routeName }, labels)).toEqual([{ label }]);
  });

  it.each(["configuration-catalog", "configuration"])(
    "связывает %s с администрированием без технического kind",
    (routeName) => {
      expect(
        buildBreadcrumbs(
          { routeName, configurationKindName: "Определение интеграции" },
          labels,
        ),
      ).toEqual([
        { label: "Администрирование", path: "/administration" },
        { label: "Определение интеграции" },
      ]);
    },
  );

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

  it("связывает учётные записи моделей с разделом администрирования", () => {
    expect(
      buildBreadcrumbs({ routeName: "provider-accounts" }, labels),
    ).toEqual([
      { label: "Администрирование", path: "/administration" },
      { label: "Учётные записи моделей" },
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

  it("сохраняет контекст проекта в каталоге runtime-секретов", () => {
    expect(
      buildBreadcrumbs(
        {
          routeName: "runtime-secrets",
          project: { ref: "project_sales", name: "Продажи" },
        },
        labels,
      ),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      { label: "Продажи", path: "/projects/project_sales" },
      { label: "Секреты" },
    ]);
  });

  it("показывает канонический deep link корзины внутри файлов Проекта", () => {
    expect(
      buildBreadcrumbs(
        {
          routeName: "files-trash",
          project: { ref: "project_sales", name: "Продажи" },
        },
        labels,
      ),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      { label: "Продажи", path: "/projects/project_sales" },
      { label: "Файлы и знания", path: "/projects/project_sales/files" },
      { label: "Корзина" },
    ]);
  });

  it("ведёт из нового навыка к каталогу текущего Проекта", () => {
    expect(
      buildBreadcrumbs(
        {
          routeName: "project-context-resource",
          project: { ref: "project_sales", name: "Продажи" },
          contextResourceKind: "skills",
          contextResourceNew: true,
        },
        labels,
      ),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      { label: "Продажи", path: "/projects/project_sales" },
      {
        label: "Файлы и знания",
        path: "/projects/project_sales/files",
      },
      {
        label: "Навыки",
        path: "/projects/project_sales/files?view=skills",
      },
      { label: "Новый навык" },
    ]);
  });

  it("ведёт из глобальной записи памяти к её каталогу", () => {
    expect(
      buildBreadcrumbs(
        {
          routeName: "context-resource",
          contextResourceKind: "memory",
        },
        labels,
      ),
    ).toEqual([
      { label: "Файлы и знания", path: "/files" },
      { label: "Память Kodex", path: "/files?view=memory" },
      { label: "Запись памяти" },
    ]);
  });

  it("сохраняет контекст проекта в каталоге и редакторе образов", () => {
    const context = {
      project: { ref: "project_sales", name: "Продажи" },
    };
    expect(
      buildBreadcrumbs({ ...context, routeName: "role-images" }, labels),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      { label: "Продажи", path: "/projects/project_sales" },
      { label: "Образы ИИ-сотрудников" },
    ]);
    expect(
      buildBreadcrumbs({ ...context, routeName: "role-image-new" }, labels),
    ).toEqual([
      { label: "Проекты", path: "/projects" },
      { label: "Продажи", path: "/projects/project_sales" },
      {
        label: "Образы ИИ-сотрудников",
        path: "/projects/project_sales/role-images",
      },
      { label: "Новый образ" },
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
