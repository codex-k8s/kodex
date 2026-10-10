import { createSSRApp } from "vue";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import {
  fileOperationFixture,
  appliedFilePlanFixture,
} from "../project-file-plan.fixtures";
import Summary from "./AssistantProjectFileRevisionSummary.vue";
import Card from "./AssistantProjectFileRevisionCard.vue";
vi.mock("../project-file-readback", () => ({
  readAppliedProjectFileRevision: vi.fn(() => new Promise(() => {})),
}));

describe("compact file before/new comparison", () => {
  it("canonical receipt без завершённого readback ещё не показывает success", async () => {
    const app = createSSRApp(Card, {
      plan: appliedFilePlanFixture(),
      operationRef: "op_file",
    });
    app.use(i18n);
    const html = await renderToString(app);
    expect(html).toContain("Новая версия файла");
    expect(html).not.toContain("Версия файла сохранена");
    expect(html).not.toContain("staging-not-a-grant");
  });
  it.each([false, true])(
    "edited=%s не раскрывает staged locator или body",
    async (edited) => {
      const operation = fileOperationFixture();
      const app = createSSRApp(Summary, { operation, edited });
      app.use(i18n);
      const html = await renderToString(app);
      expect(html).toContain("document.md");
      expect(html).toContain("v1");
      expect(html).toContain("Будет назначена при применении");
      expect(html).not.toContain("staging-not-a-grant");
      expect(html).not.toContain("art_fixture");
      expect(html).not.toContain("arv_previous");
      expect(html.includes(String(operation.after.digest))).toBe(!edited);
      expect(html.includes("Дайджест будет проверен")).toBe(edited);
    },
  );
});
