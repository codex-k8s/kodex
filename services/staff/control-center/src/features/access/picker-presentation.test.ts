import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import { createPickerLabels, selection } from "./picker-presentation";

describe("подписи и выбор в элементах доступа", () => {
  it("учитывает смену локали после создания функции подписей", () => {
    const i18n = createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          common: { loading: "Загрузка", empty: "Пусто", retry: "Повтор" },
          errors: { default: "Ошибка" },
        },
        en: {
          common: { loading: "Loading", empty: "Empty", retry: "Retry" },
          errors: { default: "Error" },
        },
      },
    });
    const labels = createPickerLabels((key) => i18n.global.t(key));
    expect(labels("Проект", "Поиск")).toEqual({
      label: "Проект",
      searchPlaceholder: "Поиск",
      loading: "Загрузка",
      loadingMore: "Загрузка",
      empty: "Пусто",
      error: "Ошибка",
      retry: "Повтор",
    });
    i18n.global.locale.value = "en";
    expect(labels("Project", "Search")).toEqual({
      label: "Project",
      searchPlaceholder: "Search",
      loading: "Loading",
      loadingMore: "Loading",
      empty: "Empty",
      error: "Error",
      retry: "Retry",
    });
  });

  it("принимает только одиночное значение выбора", () => {
    expect(selection("ref")).toBe("ref");
    expect(selection("")).toBe("");
    expect(selection(null)).toBe("");
    expect(selection(["ref"])).toBe("");
  });
});
