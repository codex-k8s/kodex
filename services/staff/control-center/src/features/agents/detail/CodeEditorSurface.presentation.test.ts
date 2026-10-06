import { readFileSync } from "node:fs";
import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";
import CodeEditorSurface from "./CodeEditorSurface.vue";

async function render(props: InstanceType<typeof CodeEditorSurface>["$props"]) {
  const app = createSSRApp({ render: () => h(CodeEditorSurface, props) });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          app: { editorKeyboard: "Редактор" },
          voice: { start: "Голосовой ввод" },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("Компактная presentation общего редактора и runtime", () => {
  it("header не выдаёт help за code: короткий language badge, полный доступный help/tooltip", async () => {
    const description =
      "Черновик проверяется сервером и применяется только после публикации.";
    const html = await render({
      modelValue: 'model_reasoning_effort = "medium"',
      language: "toml",
      label: "Overlay config.toml",
      description,
    });
    const bar = html.match(/class="code-editor__bar"[\s\S]*?<\/div>/)?.[0];
    expect(bar).toContain("Overlay config.toml");
    expect(bar).toMatch(/<code[^>]*>\s*TOML\s*<\/code>/);
    expect(bar).not.toMatch(/<code[^>]*>\s*Черновик/);
    expect(bar).toContain(`title="${description}"`);
    expect(bar).toMatch(
      /<span[^>]*id="[^"]+-help"[^>]*class="sr-only"[^>]*>\s*Черновик/,
    );
  });

  it("readonly и diagnostics остаются видимыми и объявляемыми независимо от compact help", async () => {
    const html = await render({
      modelValue: "",
      language: "markdown",
      label: "Инструкции",
      description: "Помощь",
      readonly: true,
      validationMessages: ["Ошибка конфигурации"],
      diagnostics: [{ line: 1, column: 1, message: "Ошибка конфигурации" }],
    });
    expect(html).toContain("code-editor--readonly");
    expect(html).toMatch(/<code[^>]*>\s*Markdown\s*<\/code>/);
    expect(html).toContain('aria-live="polite"');
    const diagnostic = html.match(
      /<span[^>]*class="code-editor__validation"[\s\S]*?<\/span>/,
    )?.[0];
    expect(diagnostic).toContain("Ошибка конфигурации");
    expect(diagnostic).not.toContain("sr-only");
  });

  it("header/layout допускают узкий контейнер и переносят полный label/diagnostics", () => {
    const source = readFileSync(
      new URL("./CodeEditorSurface.vue", import.meta.url),
      "utf8",
    );
    expect(source).toMatch(/\.code-editor \{\s*min-width: 0;/);
    expect(source).toMatch(
      /\.code-editor__bar strong \{\s*min-width: 0;\s*overflow-wrap: anywhere;/,
    );
    expect(source).toMatch(
      /\.code-editor__bar > svg,\s*\.code-editor__bar code \{\s*flex-shrink: 0;/,
    );
    expect(source).toMatch(/\.code-editor__foot \{\s*flex-wrap: wrap;/);
    expect(source).toContain(
      "describedBy: displayMessages.value.length ? validationId : helpId",
    );
  });

  it("technical effort help раскрывается штатно, cost и repair остаются сразу видимыми", () => {
    const source = readFileSync(
      new URL("./AgentRuntimePanel.vue", import.meta.url),
      "utf8",
    );
    const help = source.match(
      /<details class="runtime-effort-help">([\s\S]*?)<\/details>/,
    )?.[0];
    expect(help).toBeDefined();
    expect(help).toContain('$t("common.details")');
    expect(help).toContain('$t("runtimeOverlay.effortHelp",');
    expect(help).toContain("model: view.configuration.model");
    expect(help).not.toContain(" open");
    expect(help).not.toContain("runtimeOverlay.effortCost");
    expect(help).not.toContain("runtimeOverlay.repairBeforeEffort");
    expect(source).toContain('<label :for="reasoningEffortId">');
    expect(source).toContain('@change="chooseEffort"');
    expect(source).toContain('$t("runtimeOverlay.effortCost")');
    expect(source).toContain('<small v-if="selectedEffort === undefined">');
  });

  it("Runtime/Overlay heading отделяет badge от полного текста на узком экране", () => {
    const source = readFileSync(
      new URL("./AgentRuntimePanel.vue", import.meta.url),
      "utf8",
    );
    expect(source).toMatch(
      /\.runtime-panel__head,\s*\.overlay-panel__head \{\s*display: flex;\s*min-width: 0;\s*flex-wrap: wrap;/,
    );
    expect(source).toMatch(
      /\.runtime-panel__head > div,\s*\.overlay-panel__head > div \{\s*min-width: 0;\s*flex: 1 1 220px;\s*overflow-wrap: anywhere;/,
    );
    expect(source).toMatch(
      /\.runtime-panel__head > \.status-badge,\s*\.overlay-panel__head > \.status-badge \{\s*align-self: flex-start;\s*white-space: normal;\s*overflow-wrap: anywhere;/,
    );
    const mobile = source.slice(source.indexOf("@media (max-width: 640px)"));
    expect(mobile).toMatch(
      /\.runtime-panel__head,\s*\.overlay-panel__head \{\s*flex-direction: column;/,
    );
    expect(mobile).toMatch(
      /\.runtime-panel__head > div,\s*\.overlay-panel__head > div \{\s*width: 100%;\s*flex: none;/,
    );
    expect(source).toContain("<p>{{ copy.runtime.overlayHelp }}</p>");
    expect(source).toContain(
      ":state=\"overlayDirty ? 'DRAFT' : overlayState\"",
    );
  });
});
