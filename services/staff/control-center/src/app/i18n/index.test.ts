import { describe, expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "./index";
function entries(value: unknown, prefix = ""): [string, string][] {
  if (typeof value === "string") return [[prefix, value]];
  if (!value || typeof value !== "object") return [];
  return Object.entries(value).flatMap(([key, child]) =>
    entries(child, prefix ? `${prefix}.${key}` : key),
  );
}
describe("Control Center translations", () => {
  it("имеет явные переводы каждого русского ключа без скрытого fallback в английский интерфейс", () => {
    const ru = entries(i18n.global.getLocaleMessage("ru"));
    const en = entries(i18n.global.getLocaleMessage("en"));
    const englishKeys = new Set(en.map(([key]) => key));
    expect
      .soft(ru.map(([key]) => key).filter((key) => !englishKeys.has(key)))
      .toEqual([]);
    expect
      .soft(
        en
          .filter(
            ([key, value]) =>
              key !== "common.russian" && /[А-Яа-яЁё]/u.test(value),
          )
          .map(([key]) => key),
      )
      .toEqual([]);
  });
});
