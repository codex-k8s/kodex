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
  it("сохраняет literal intent без перевода токенов, статусов и скрытия похожих refs", async () => {
    const app = createSSRApp({
      render: () =>
        h(SafeStructuredData, {
          literal: true,
          value: {
            fields: [
              { key: "title", value: "i18n:INTERACTION_AUTHORITY_CHANGED" },
              { key: "body", value: "agt_literal123" },
            ],
            state: "READY",
            enabled: false,
          },
        }),
    });
    app.use(i18n);
    const html = await renderToString(app);
    expect(html).toContain("i18n:INTERACTION_AUTHORITY_CHANGED");
    expect(html).toContain("agt_literal123");
    expect(html).toContain("READY");
    expect(html).toContain("false");
    expect(html).not.toContain("status-badge");
  });
  it.each(["ru", "en"] as const)(
    "не раскрывает неизвестный server token в %s",
    async (locale) => {
      i18n.global.locale.value = locale;
      const content = "i18n:UNKNOWN_FUTURE_SERVER_MESSAGE";
      const app = createSSRApp({
        render: () =>
          h("main", [
            h(SafeSummary, { content }),
            h(SafeMarkdown, { content }),
            h(SafeStructuredData, { value: { message: content } }),
            h(SafeSummary, { content: "Owner content i18n:UNKNOWN" }),
          ]),
      });
      app.use(i18n);
      const html = await renderToString(app);
      expect(
        html.split(i18n.global.t("serverMessages.unsupported")),
      ).toHaveLength(4);
      expect(html).not.toContain(content);
      expect(html).toContain("Owner content i18n:UNKNOWN");
      i18n.global.locale.value = "ru";
    },
  );
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
    "GATE_CONSEQUENCE_CONTINUE",
    "GATE_CONSEQUENCE_EXTERNAL_EFFECT",
    "GATE_CONSEQUENCE_REJECT_RUN",
    "GATE_CONSEQUENCE_REJECT_EFFECT",
    "GATE_CONSEQUENCE_CANCEL_RUN",
    "GATE_CONSEQUENCE_CANCEL_EFFECT",
    "GATE_CONSEQUENCE_REQUEST_CHANGES",
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
