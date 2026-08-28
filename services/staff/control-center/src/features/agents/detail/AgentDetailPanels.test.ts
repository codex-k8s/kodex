import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import AgentApplyState from "@/features/agents/detail/AgentApplyState.vue";
import AgentInstructionsPanel from "@/features/agents/detail/AgentInstructionsPanel.vue";
import AgentRuntimePanel from "@/features/agents/detail/AgentRuntimePanel.vue";
import CodeEditorSurface from "@/features/agents/detail/CodeEditorSurface.vue";

const messages = {
  ru: {
    common: {
      details: "Подробнее",
      open: "Открыть",
      save: "Сохранить",
      unavailable: "Недоступно",
      unknownStatus: "Неизвестно",
    },
    agents: {
      instructions: "Инструкции",
      validate: "Проверить инструкции",
      publish: "Опубликовать инструкции",
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

  it("рендерит monospace editor с gutter и безопасной подсветкой", async () => {
    const html = await render(CodeEditorSurface, {
      modelValue: '# Роль\nmodel = "gpt-5.1"\n{{run.task}}',
      language: "markdown",
      label: "Инструкции",
      readonly: true,
    });

    expect(html).toContain("code-editor__gutter");
    expect(html).toContain("code-editor__token--variable");
    expect(html).toContain("textarea");
    expect(html).toContain("readonly");
    expect(html).not.toContain("v-html");
  });

  it("связывает provider/model/profile с одним runtimeRef и блокирует overlay mutation", async () => {
    const html = await render(AgentRuntimePanel, {
      modelValue: "runtime_safe",
      runtimes: [
        {
          ref: "runtime_safe",
          name: "Безопасный",
          revision: "runtime-v1",
          ready: true,
          provider: "openai-codex",
          model: "gpt-5.1",
        },
      ],
      canEdit: true,
      busy: false,
      dirty: false,
    });

    expect(html.match(/<select/g)).toHaveLength(3);
    expect(html).toContain("openai-codex");
    expect(html).toContain("gpt-5.1");
    expect(html).toContain("Overlay config.toml");
    expect(html).toContain("config.toml mutation");
    expect(html).toMatch(/<button[^>]*disabled[^>]*>\s*Сохранить\s*<\/button>/);
  });

  it("показывает переменные из текста, но не открывает отсутствующий API-каталог", async () => {
    const html = await render(AgentInstructionsPanel, {
      modelValue: "# Роль\nВыполни {{run.task}} для {{project.name}}.",
      state: "DRAFT",
      validationMessages: [],
      canEdit: true,
      canValidate: true,
      canPublish: false,
      busy: false,
      dirty: true,
    });

    expect(html).toContain("{{run.task}}");
    expect(html).toContain("{{project.name}}");
    expect(html).toContain("Каталог разрешённых переменных не представлен API");
    expect(html).toMatch(/<button[^>]*disabled[^>]*>[\s\S]*Template variables/);
  });
});
