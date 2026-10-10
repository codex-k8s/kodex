import { expect, it } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { captureSetupState } from "@/test-utils/setup-harness";
import Component from "./RuntimeEnvironmentToolsEditor.vue";

function renderTools(
  disabled: boolean,
  locale = "ru",
  availability: { inventoryAvailable?: boolean; loading?: boolean } = {},
) {
  const tools = Array.from({ length: 38 }, (_, index) => ({
    name: `Инструмент ${String(index)}`,
    command: `tool-${String(index)}`,
    description: `Описание ${String(index)}`,
    usageHint: `Подсказка ${String(index)}`,
  }));
  const props = {
    tools,
    catalog:
      availability.inventoryAvailable === false
        ? []
        : tools.map((tool) => ({ name: tool.command, version: "1.2.3" })),
    disabled,
    imageSelected: true,
    inventoryAvailable: availability.inventoryAvailable ?? true,
    loading: availability.loading ?? false,
  };
  const app = createSSRApp({ render: () => h(Component, props) });
  app.use(
    createI18n({
      legacy: false,
      locale,
      messages: {
        [locale]: {
          common: {
            edit: locale === "ru" ? "Изменить" : "Edit",
            description: "Описание",
            selectedCount: "Выбрано: {count}",
            loading: "Загрузка",
          },
          runtime: {
            verifiedTools: "Проверенные инструменты",
            verifiedToolsHelp: "Проверенный каталог",
            selectedToolsCount: "{selected} / {total}",
            toolDisplayName: "Название",
            toolCommand: "Команда",
            toolUsageHint: "Подсказка",
            imageInventoryUnavailable: "Каталог недоступен",
          },
        },
      },
    }),
  );
  return { html: renderToString(app), tools };
}

it.each([{ inventoryAvailable: false }, { loading: true }])(
  "не показывает выдуманный знаменатель при неизвестном каталоге %j",
  async (availability) => {
    const { html, tools } = renderTools(false, "ru", availability);
    const before = structuredClone(tools);
    const rendered = await html;
    expect(rendered).toContain("Выбрано: 38");
    expect(rendered).not.toContain("38 / 0");
    expect(rendered).not.toContain("38 / 38");
    expect(rendered).not.toContain("<article");
    expect(tools).toEqual(before);
  },
);

it.each(["ru", "en"])(
  "сохраняет все 38 выбранных инструментов, прячет все поля под индивидуальное раскрытие (%s)",
  async (locale) => {
    const { html, tools } = renderTools(false, locale);
    const before = structuredClone(tools);
    const rendered = await html;
    expect(rendered).toContain('role="region"');
    expect(rendered).toContain('tabindex="0"');
    expect(rendered).toContain('aria-label="Проверенные инструменты"');
    expect(rendered).toContain("38 / 38");
    const rows = [
      ...rendered.matchAll(/<article\b[^>]*>([\s\S]*?)<\/article>/g),
    ];
    expect(rows).toHaveLength(38);
    for (const [index, row] of rows.entries()) {
      const body = row[1] ?? "";
      const suffix = String(index);
      expect(body).toMatch(/type="checkbox"[^>]*checked/);
      expect(body).toMatch(new RegExp(`<code\\b[^>]*>tool-${suffix}</code>`));
      expect(body).toContain("1.2.3");
      const details = /<details\b([^>]*)>([\s\S]*?)<\/details>/.exec(body);
      expect(details).not.toBeNull();
      if (!details) throw new Error("Expected individual tool disclosure");
      expect(details[1]).not.toMatch(/\bopen\b/);
      expect(details[2]).toContain(
        `aria-label="${locale === "ru" ? "Изменить" : "Edit"}: tool-${suffix}"`,
      );
      expect(details[2]).toContain(`value="Инструмент ${suffix}"`);
      expect(details[2]).toMatch(
        new RegExp(`value="tool-${suffix}"[^>]*readonly`),
      );
      expect(details[2]).toContain(`Описание ${suffix}`);
      expect(details[2]).toContain(`Подсказка ${suffix}`);
      // Вне раскрытия остаётся только checkbox, а не постоянно видимые поля.
      expect(body.replace(details[0], "").match(/<input\b/g)).toHaveLength(1);
    }
    expect(tools).toEqual(before);
  },
);

it("оставляет все выбранные инструменты заблокированными во время сохранения", async () => {
  const { html } = renderTools(true);
  const rendered = await html;
  expect([
    ...rendered.matchAll(/type="checkbox"[^>]*checked[^>]*disabled/g),
  ]).toHaveLength(38);
  expect([...rendered.matchAll(/<textarea\b[^>]*disabled/g)]).toHaveLength(76);
});

it("ограничивает высоту общего каталога, не обрезая данные до пяти элементов", async () => {
  const source = (await import("./RuntimeEnvironmentToolsEditor.vue?raw"))
    .default;
  const catalogStyles = [...source.matchAll(/\.tool-catalog\s*\{([^}]+)\}/g)];
  expect(
    catalogStyles.some(
      (style) =>
        style[1]?.includes("max-height: 360px") &&
        style[1].includes("overflow-y: auto"),
    ),
  ).toBe(true);
  expect(source).toContain("(tool, index) in catalog");
});

it("сворачивает заполненные метаданные, но показывает обязательное незаполненное описание", async () => {
  const tools = [
    {
      name: "Git",
      command: "git",
      description: "Проверка дерева",
      usageHint: "status",
    },
    { name: "Node", command: "node", description: "", usageHint: "--version" },
  ];
  const before = structuredClone(tools);
  const state = await captureSetupState(Component, undefined, {
    tools,
    catalog: [
      { name: "git", version: "2" },
      { name: "node", version: "24" },
    ],
    disabled: false,
    imageSelected: true,
    inventoryAvailable: true,
  });
  const expanded = state.expandedTools as Map<string, boolean>;
  expect(expanded.get("git")).toBe(false);
  expect(expanded.get("node")).toBe(true);
  expect(tools).toEqual(before);
});

it("не разрешает команду при недоступном inventory даже при переданном каталоге", async () => {
  const state = await captureSetupState(Component, undefined, {
    tools: [],
    catalog: [{ name: "git", version: "2" }],
    disabled: false,
    imageSelected: true,
    inventoryAvailable: false,
  });
  // Недоступный inventory закрывает обработчик до любого пользовательского эффекта.
  const source = await import("./RuntimeEnvironmentToolsEditor.vue?raw");
  expect(source.default).toContain(
    "props.disabled || !props.inventoryAvailable",
  );
  expect(source.default).toContain("runtime.imageInventoryUnavailable");
  expect(state).toHaveProperty("toggle");
});
