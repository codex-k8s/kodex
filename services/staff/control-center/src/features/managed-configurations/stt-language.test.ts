import { describe, expect, it } from "vitest";
import {
  additionalSttLanguages,
  primarySttLanguage,
  setAdditionalSttLanguages,
  setPrimarySttLanguage,
} from "./stt-language";

describe("STT language form", () => {
  it("показывает один основной код и сохраняет дополнительные языки отдельно", () => {
    const stt = {
      language: "",
      parameters: { languages: ["ru", "en"], keywords: ["Kodex"] },
    };
    expect(primarySttLanguage(stt, true)).toBe("ru");
    expect(additionalSttLanguages(stt)).toBe("en");
    expect(setPrimarySttLanguage(stt, true, "de")).toEqual({
      language: "",
      parameters: { languages: ["de", "en"], keywords: ["Kodex"] },
    });
    expect(setAdditionalSttLanguages(stt, "en\nen\nfr")).toEqual({
      language: "",
      parameters: { languages: ["ru", "en", "fr"], keywords: ["Kodex"] },
    });
  });

  it("при смене модели переносит hint между несовместимыми полями", () => {
    const multiple = {
      language: "",
      parameters: { languages: ["ru", "en"] },
    };
    const singular = setPrimarySttLanguage(multiple, false, "ru");
    expect(singular).toEqual({
      language: "ru",
      parameters: { languages: [] },
    });
    expect(primarySttLanguage(singular, false)).toBe("ru");
    expect(setPrimarySttLanguage(singular, true, "ru")).toEqual({
      language: "",
      parameters: { languages: ["ru"] },
    });
  });

  it("пустой код означает автоопределение без конкурирующих hints", () => {
    expect(
      setPrimarySttLanguage(
        { language: "ru", parameters: { languages: ["ru", "en"] } },
        true,
        "",
      ),
    ).toEqual({ language: "", parameters: { languages: [] } });
  });
});
