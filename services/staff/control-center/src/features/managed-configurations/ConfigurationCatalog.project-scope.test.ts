import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/shared/ui/ProblemNotice.vue", () => ({
  default: defineComponent({ setup: () => () => h("div") }),
}));

import ConfigurationCatalog from "./ConfigurationCatalog.vue";

async function render(
  kind: "PROMPT_TEMPLATE" | "ROLE_IMAGE" | "INTEGRATION_DEFINITION",
  projectRef?: string,
) {
  const app = createSSRApp({
    render: () => h(ConfigurationCatalog, { kind, projectRef }),
  });
  app.component(
    "RouterLink",
    defineComponent({
      props: { to: { type: Object, required: true } },
      setup:
        (_props, { slots }) =>
        () =>
          h("a", slots.default?.()),
    }),
  );
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      messages: {
        ru: {
          common: { create: "Создать", search: "Поиск" },
          managed: { projectRequired: "Выберите проект", more: "Ещё" },
          catalog: { expand: "Развернуть" },
        },
      },
    }),
  );
  return renderToString(app);
}

describe("Project scope configuration catalog", () => {
  it.each(["PROMPT_TEMPLATE", "ROLE_IMAGE"] as const)(
    "объясняет недоступный create для %s без проекта",
    async (kind) => {
      const html = await render(kind);
      expect(html).toContain("disabled");
      expect(html).toContain(
        'aria-describedby="managed-catalog-project-required"',
      );
      expect(html).toContain("Выберите проект");
      expect(html).not.toContain("<a");
    },
  );

  it("оставляет organization IntegrationDefinition доступной без проекта", async () => {
    const html = await render("INTEGRATION_DEFINITION");
    expect(html).toContain("<a");
    expect(html).not.toContain("managed-catalog-project-required");
  });

  it("открывает project-scoped create после точного выбора", async () => {
    const html = await render("PROMPT_TEMPLATE", "project_fixture");
    expect(html).toContain("<a");
    expect(html).not.toContain("managed-catalog-project-required");
  });
});
