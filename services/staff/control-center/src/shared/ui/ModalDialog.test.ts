import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import ModalDialog from "@/shared/ui/ModalDialog.vue";

type DialogSize = "sm" | "md" | "lg" | "xl" | "full";

function renderDialog(size: DialogSize, busy = false): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(
        ModalDialog,
        { busy, size, title: "Проверка размера" },
        { default: () => h("p", "Содержимое") },
      ),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: { ru: { common: { close: "Закрыть" } } },
    }),
  );
  return renderToString(app);
}

describe("ModalDialog", () => {
  it.each(["sm", "md", "lg", "xl", "full"] as const)(
    "публикует размер %s через устойчивый modifier",
    async (size) => {
      const html = await renderDialog(size);

      expect(html).toContain(`modal--${size}`);
      expect(html).toContain('role="dialog"');
      expect(html).toContain('aria-modal="true"');
      expect(html).toContain("Проверка размера");
    },
  );

  it("блокирует close control и объявляет busy state", async () => {
    const html = await renderDialog("md", true);

    expect(html).toContain('aria-busy="true"');
    expect(html).toContain("disabled");
    expect(html).toContain('aria-label="Закрыть"');
  });
});
