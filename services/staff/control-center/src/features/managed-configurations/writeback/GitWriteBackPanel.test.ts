import { afterEach, expect, it, vi } from "vitest";
import { createSSRApp } from "vue";
import { renderToString } from "vue/server-renderer";
import { createI18n } from "vue-i18n";
import GitWriteBackPanel from "./GitWriteBackPanel.vue";
import { writeBackFixture, memoryStorage } from "./fixtures";
import { writeBackMessages } from "./messages";
import { WriteBackController } from "./controller";

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
it.each(["ru", "en"] as const)(
  "отрисовывает %s без raw keys и не предлагает обычную правку UI-owned объекта",
  async (locale) => {
    vi.useFakeTimers();
    vi.stubGlobal("sessionStorage", memoryStorage());
    const { configuration } = await writeBackFixture();
    const app = createSSRApp(GitWriteBackPanel, {
      configuration: { ...configuration, managedBy: "UI" },
      disabled: true,
    });
    app.use(createI18n({ legacy: false, locale, messages: writeBackMessages }));
    const html = await renderToString(app);
    expect(html).toContain(writeBackMessages[locale].wb.title);
    expect(html).toContain(writeBackMessages[locale].wb.disabled.git);
    expect(html).not.toContain("wb.");
    expect(html).not.toContain("<textarea");
    expect(html).not.toContain('type="checkbox"');
  },
);
it("локальные словари содержат одинаковые terminal/disabled/failure keys", () => {
  function keys(value: Record<string, unknown>, prefix = ""): string[] {
    return Object.entries(value)
      .flatMap(([key, entry]) =>
        typeof entry === "object" && entry
          ? keys(entry as Record<string, unknown>, prefix + key + ".")
          : [prefix + key],
      )
      .sort();
  }
  expect(keys(writeBackMessages.ru)).toEqual(keys(writeBackMessages.en));
});
it("план требует отдельный approval и показывает unknown как read-only состояние", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("sessionStorage", memoryStorage());
  const { configuration, view } = await writeBackFixture();
  vi.spyOn(WriteBackController.prototype, "history").mockImplementation(
    function (this: WriteBackController) {
      this.view = view;
      this.loaded = true;
      return Promise.resolve();
    },
  );
  const app = createSSRApp(GitWriteBackPanel, { configuration });
  app.use(
    createI18n({ legacy: false, locale: "ru", messages: writeBackMessages }),
  );
  const html = await renderToString(app);
  expect(html).toContain(view.proposal.baseCommitSha);
  expect(html).toContain('type="checkbox"');
  expect(html).toMatch(
    /<button[^>]*disabled[^>]*>\s*Подтвердить и создать PR\/MR/,
  );
  view.proposal.state = "UNKNOWN_OUTCOME";
  const unknown = createSSRApp(GitWriteBackPanel, { configuration });
  unknown.use(
    createI18n({ legacy: false, locale: "ru", messages: writeBackMessages }),
  );
  const readOnly = await renderToString(unknown);
  expect(readOnly).toContain(writeBackMessages.ru.wb.unknownEffect);
  expect(readOnly).not.toContain('type="checkbox"');
});
