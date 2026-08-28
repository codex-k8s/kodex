import { renderToString } from "@vue/server-renderer";
import { createSSRApp } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";

import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";

describe("AsyncEntityPicker", () => {
  it("показывает понятное имя выбранной сущности без внутреннего ref", async () => {
    const app = createSSRApp(AsyncEntityPicker, {
      modelValue: "renv_internal_ref",
      selected: {
        ref: "renv_internal_ref",
        title: "Офисные документы",
        description: "rev 4 · готово",
      },
      loadPage: vi.fn(),
      placeholder: "Выберите окружение",
      searchPlaceholder: "Поиск окружений",
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            common: { loading: "Загрузка", retry: "Повторить", empty: "Пусто" },
            errors: { default: "Ошибка" },
            runtime: { pickerShown: "Показано: {count}", pickerScroll: "Ещё" },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Офисные документы");
    expect(html).toContain("rev 4 · готово");
    expect(html).not.toContain("renv_internal_ref");
    expect(html).toContain('role="combobox"');
  });
});
