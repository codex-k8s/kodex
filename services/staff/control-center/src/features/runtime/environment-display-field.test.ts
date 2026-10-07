import { computed, ref } from "vue";
import { describe, expect, it, vi } from "vitest";
import { environmentDisplayField } from "./environment-display-field";

describe("локализованные поля окружения сохраняют исходную спецификацию", () => {
  it("чтение и повторный ввод перевода не переименовывают серверный marker", () => {
    const raw = ref("i18n:DEFAULT_RUNTIME_ENVIRONMENT");
    const locale = ref<"ru" | "en">("ru");
    const write = vi.fn((value: string) => (raw.value = value));
    const field = environmentDisplayField(
      () => raw.value,
      write,
      (value) =>
        value === "i18n:DEFAULT_RUNTIME_ENVIRONMENT"
          ? locale.value === "ru"
            ? "Основное окружение"
            : "Default environment"
          : value,
    );
    const preview = computed(() => field.value);
    expect(field.value).toBe("Основное окружение");
    expect(preview.value).toBe("Основное окружение");
    const russianDisplay = field.value;
    field.value = russianDisplay;
    locale.value = "en";
    expect(field.value).toBe("Default environment");
    const englishDisplay = field.value;
    field.value = englishDisplay;
    expect(write).not.toHaveBeenCalled();
    expect(raw.value).toBe("i18n:DEFAULT_RUNTIME_ENVIRONMENT");
    field.value = "My environment";
    expect(write).toHaveBeenCalledExactlyOnceWith("My environment");
    expect(field.value).toBe("My environment");
  });
  it("пустые и пользовательские значения не меняются скрытно", () => {
    const raw = ref("");
    const write = vi.fn((value: string) => (raw.value = value));
    const field = environmentDisplayField(
      () => raw.value,
      write,
      (value) => value,
    );
    expect(field.value).toBe("");
    field.value = "Описание окружения";
    expect(raw.value).toBe("Описание окружения");
    field.value = "";
    expect(raw.value).toBe("");
  });
});
