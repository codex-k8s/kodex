import { describe, expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { environmentReadinessMessage } from "./environment-readiness-message";
import { i18n } from "@/app/i18n";

describe("Environment readiness messages", () => {
  it.each(["ru", "en"] as const)(
    "переводит закрытые owner причины в %s без substring authority",
    (locale) => {
      const t = (key: string) => i18n.global.t(key, {}, { locale });
      for (const code of [
        "ENVIRONMENT_NOT_ACTIVE",
        "PUBLISHED_VERSION_MISSING",
        "PROMOTED_IMAGE_MISSING",
        "ROLE_RUNTIME_CONTRACT_STALE",
      ]) {
        const text = environmentReadinessMessage(code, t);
        expect(text).not.toContain(code);
        expect(text).not.toContain("runtime.");
        expect(text).not.toEqual(t("runtime.environmentReadinessUnknown"));
      }
      for (const code of [
        "FUTURE_PROMOTED_IMAGE_MISSING",
        "i18n:ROLE_RUNTIME_CONTRACT_STALE",
        "unknown",
      ]) {
        expect(environmentReadinessMessage(code, t)).toBe(
          t("runtime.environmentReadinessUnknown"),
        );
      }
    },
  );
});
