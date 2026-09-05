import { describe, expect, it, vi } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import SafeSummary from "./SafeSummary.vue";
import SafeMarkdown from "./SafeMarkdown.vue";
import SafeStructuredData from "./SafeStructuredData.vue";
import { serverMessageKey } from "./server-message";
describe("Closed server message keys", () => {
  it.each(["ru", "en"] as const)(
    "переводит generic result до обработки markdown в %s",
    async (locale) => {
      i18n.global.locale.value = locale;
      const content = "i18n:INTERACTION_DELIVERY_APPROVAL_REQUIRED";
      const app = createSSRApp({
        render: () =>
          h("main", [
            h(SafeSummary, { content }),
            h(SafeMarkdown, { content }),
            h(SafeStructuredData, { value: { message: content } }),
          ]),
      });
      app.use(i18n);
      const html = await renderToString(app);
      const translated = i18n.global.t(
        "serverMessages.INTERACTION_DELIVERY_APPROVAL_REQUIRED",
      );
      expect(html.split(translated)).toHaveLength(4);
      expect(html).not.toContain("i18n:");
      i18n.global.locale.value = "ru";
    },
  );
  it.each([
    "INTERACTION_AUTHORITY_CHANGED",
    "INTERACTION_DELIVERY_GATE_TITLE",
    "INTERACTION_DELIVERY_GATE_PROMPT",
    "INTERACTION_DELIVERY_APPROVAL_REQUIRED",
  ])("переводит только явный серверный префикс %s", (key) => {
    expect(serverMessageKey(`i18n:${key}`)).toBe(`serverMessages.${key}`);
    expect(serverMessageKey(key)).toBeUndefined();
  });
  it.each([
    "i18n:common.delete",
    "i18n:UNKNOWN",
    "Owner content i18n:INTERACTION_DELIVERY_GATE_TITLE",
  ])("сохраняет произвольный текст %s", (value) => {
    expect(serverMessageKey(value)).toBeUndefined();
  });
});
