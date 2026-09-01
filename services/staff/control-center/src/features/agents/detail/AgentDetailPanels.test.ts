import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it, vi } from "vitest";

import AgentApplyState from "@/features/agents/detail/AgentApplyState.vue";
import AgentInstructionsPanel from "@/features/agents/detail/AgentInstructionsPanel.vue";
import AgentProfilePanel from "@/features/agents/detail/AgentProfilePanel.vue";
import CodeEditorSurface from "@/features/agents/detail/CodeEditorSurface.vue";
import InstructionHistory from "@/features/agents/components/InstructionHistory.vue";

vi.mock("@/features/agents/detail/api", () => ({
  createTemplateVariableLoader: () => () =>
    Promise.resolve({
      items: [],
      nextCursor: null,
    }),
}));

const messages = {
  ru: {
    common: {
      details: "Подробнее",
      cancel: "Отмена",
      open: "Открыть",
      save: "Сохранить",
      loading: "Загрузка",
      empty: "Пусто",
      error: "Ошибка",
      retry: "Повторить",
      unavailable: "Недоступно",
      unknownStatus: "Неизвестно",
    },
    agents: {
      avatar: "Аватар",
      instructions: "Инструкции",
      validate: "Проверить инструкции",
      publish: "Опубликовать инструкции",
      history: "История публикаций",
      historyHelp: "Опубликованные версии неизменяемы",
      historyEmpty: "Опубликованных версий пока нет",
      revision: "Ревизия {revision}",
      currentRevision: "Текущая",
      rollback: "Вернуть опубликованную версию",
      rollbackConfirm: "Вернуть инструкции из ревизии {revision}?",
      provider: "Провайдер",
      model: "Модель",
      runtimeRevision: "Ревизия runtime",
    },
    states: {
      APPLIED: "Применён",
      AVAILABLE: "Доступно",
      DRAFT: "Черновик",
      FAILED: "Ошибка",
      INVALID: "Есть ошибки",
      PUBLISHED: "Опубликован",
      READY: "Готов",
      RUNNING: "Выполняется",
      UNAVAILABLE: "Недоступно",
      VALID: "Проверен",
    },
  },
};

async function render(component: Parameters<typeof h>[0], props: object) {
  const app = createSSRApp({ render: () => h(component, props) });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages,
    }),
  );
  return renderToString(app);
}

describe("agent detail panels", () => {
  it("явно показывает API readback и границу применения", async () => {
    const html = await render(AgentApplyState, {
      state: "APPLIED",
      scope: "Runtime",
      boundary: "next-turn",
    });

    expect(html).toContain('aria-live="polite"');
    expect(html).toContain("Подтверждено ответом API");
    expect(html).toContain("следующем ходе через RuntimeRevision");
    expect(html).toContain("Применён");
  });

  it("рендерит CodeMirror boundary без textarea-overlay", async () => {
    const html = await render(CodeEditorSurface, {
      modelValue: '# Роль\nmodel = "gpt-5.1"\n{{run.ref}}',
      language: "markdown",
      label: "Инструкции",
      readonly: true,
    });

    expect(html).toContain("code-editor__viewport");
    expect(html).toContain("code-editor--readonly");
    expect(html).toContain("Markdown");
    expect(html).not.toContain("textarea");
    expect(html).not.toContain("v-html");
  });

  it("не предлагает URL-поле и fail-closed блокирует отсутствующий avatar API", async () => {
    const html = await render(AgentProfilePanel, {
      modelValue: {
        name: "Аналитик",
        purpose: "Проверять данные",
        roleDescription: "Работает с фактами",
      },
      roleName: "Аналитик",
      avatarUrl: "/api/v1/artifacts/avatar/content",
      avatarAsset: {
        state: "UNAVAILABLE",
        code: "avatar_asset",
        reason: "Операция не представлена API",
      },
      canEdit: true,
      busy: false,
      dirty: false,
    });

    expect(html).not.toContain('type="url"');
    expect(html).toContain('type="file"');
    expect(html).toContain('aria-label="Загрузить изображение"');
    expect(html).toContain("avatar_asset");
    expect(html).toContain("Операция не представлена API");
    expect(html).toMatch(/<button[^>]*disabled[^>]*>[\s\S]*Загрузить/);
  });

  it("разрешает upload/remove только при совместном RBAC профиля и файлов", async () => {
    const html = await render(AgentProfilePanel, {
      modelValue: {
        name: "Аналитик",
        purpose: "Проверять данные",
        roleDescription: "Работает с фактами",
      },
      roleName: "Аналитик",
      avatarUrl: "/api/v1/artifacts/art_avatar01/content?purpose=PREVIEW",
      avatarAsset: { state: "AVAILABLE", code: "avatar_asset" },
      canEdit: true,
      busy: false,
      dirty: false,
    });

    const uploadLabelPosition = html.indexOf("Загрузить изображение");
    const uploadButton = html.slice(
      html.lastIndexOf("<button", uploadLabelPosition),
      html.indexOf(">", html.lastIndexOf("<button", uploadLabelPosition)) + 1,
    );
    expect(uploadLabelPosition).toBeGreaterThan(0);
    expect(uploadButton).not.toContain("disabled");
    expect(html).not.toContain("avatar_asset:");
    expect(html).not.toContain('type="url"');
    expect(html).not.toContain("Создать с Kodex");
  });

  it("показывает server-owned каталог переменных и использованные значения", async () => {
    const html = await render(AgentInstructionsPanel, {
      modelValue: "# Роль\nОбработай {{run.ref}} в {{project.ref}}.",
      projectRef: "project_sales",
      state: "DRAFT",
      validationMessages: [],
      canEdit: true,
      canValidate: true,
      canPublish: false,
      busy: false,
      dirty: true,
    });

    expect(html).toContain("{{ .run.ref }}");
    expect(html).toContain("{{ .project.ref }}");
    expect(html).toContain("Template variables");
    expect(html).toContain("Авторитетный каталог");
    expect(html).toContain('role="combobox"');
  });

  it("показывает явные save, validate, publish и rollback команды", async () => {
    const instructions = await render(AgentInstructionsPanel, {
      modelValue: "# Роль",
      projectRef: "project_sales",
      state: "VALID",
      validationMessages: [],
      canEdit: true,
      canValidate: true,
      canPublish: true,
      busy: false,
      dirty: false,
    });
    expect(instructions).toContain("Сохранить черновик");
    expect(instructions).toContain("Проверить инструкции");
    expect(instructions).toContain("Опубликовать инструкции");
    expect(instructions).not.toContain("textarea");

    const history = await render(InstructionHistory, {
      versions: [
        {
          ref: "ins_previous",
          version: 1,
          revision: 1,
          state: "PUBLISHED",
          content: "# Предыдущая роль",
          validationMessages: [],
          createdAt: "2026-08-29T10:00:00Z",
          publishedAt: "2026-08-29T10:00:00Z",
        },
      ],
      currentRef: "ins_current",
      canRollback: true,
      busy: false,
    });
    expect(history).toContain("Вернуть опубликованную версию");
  });
});
