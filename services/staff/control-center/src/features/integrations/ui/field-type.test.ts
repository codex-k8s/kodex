import { describe, expect, it, vi } from "vitest";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import { integrationFieldTypeKey } from "./field-type";

describe("закрытые типы полей каталога", () => {
  it.each([
    ["TEXT", "строка", "string"],
    ["URL", "URL", "URL"],
    ["STRING_LIST", "список строк", "list of strings"],
    ["INTEGER", "целое число", "integer"],
    ["BOOLEAN", "логическое значение", "boolean"],
  ] as const)("переводит тип %s без подмены строкой", (type, ru, en) => {
    const key = integrationFieldTypeKey(type);
    expect(i18n.global.te(key, "ru")).toBe(true);
    expect(i18n.global.te(key, "en")).toBe(true);
    expect(i18n.global.t(key, {}, { locale: "ru" })).toBe(ru);
    expect(i18n.global.t(key, {}, { locale: "en" })).toBe(en);
  });
});
