import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import { AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

describe("ProblemNotice", () => {
  it("отличает используемый объект от конфликта версии", async () => {
    const app = createSSRApp({
      render: () =>
        h(ProblemNotice, {
          problem: new AppProblem({
            status: 409,
            code: "RESOURCE_IN_USE",
            retryable: false,
            kind: "conflict",
            title: "Сначала уберите связанные назначения",
          }),
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            common: {
              resourceInUse: "Объект используется",
              conflict: "Состояние изменилось",
              retry: "Повторить",
            },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Объект используется");
    expect(html).toContain("Сначала уберите связанные назначения");
    expect(html).not.toContain("Состояние изменилось");
  });
});
