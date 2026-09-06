import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";
import { createI18n } from "vue-i18n";
import PromptTargetPreview from "./PromptTargetPreview.vue";
import PromptScopeFields from "@/features/managed-configurations/PromptScopeFields.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

describe("Prompt nullable errors", () => {
  it("не объявляет ошибкой исходное состояние preview и scope, сохраняя намеренный fallback", async () => {
    const i18n = createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      fallbackWarn: false,
      messages: {
        ru: {
          common: { error: "Ошибка" },
          errors: { default: "Не удалось выполнить действие" },
        },
      },
    });
    const app = createSSRApp({
      render: () =>
        h("main", [
          h(PromptTargetPreview, { disabled: false }),
          h(PromptScopeFields, {
            template: "",
            projectRef: "project",
            disabled: false,
          }),
        ]),
    });
    app.use(i18n);
    expect(await renderToString(app)).not.toContain('role="alert"');
    const fallback = createSSRApp({ render: () => h(ProblemNotice) });
    fallback.use(i18n);
    expect(await renderToString(fallback)).toContain('role="alert"');
  });
});
