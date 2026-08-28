import { createSSRApp, h } from "vue";
import { renderToString, type SSRContext } from "@vue/server-renderer";
import { describe, expect, it, vi } from "vitest";

import OverlayPanel from "@/shared/ui/OverlayPanel.vue";
import {
  canDismissOverlay,
  restoreOverlayFocus,
} from "@/shared/ui/overlay-panel";

describe("OverlayPanel policy", () => {
  it("не закрывает busy overlay и учитывает раздельные политики", () => {
    expect(
      canDismissOverlay("escape", {
        busy: true,
        closeOnEscape: true,
        closeOnOutside: true,
      }),
    ).toBe(false);
    expect(
      canDismissOverlay("escape", {
        busy: false,
        closeOnEscape: false,
        closeOnOutside: true,
      }),
    ).toBe(false);
    expect(
      canDismissOverlay("outside", {
        busy: false,
        closeOnEscape: false,
        closeOnOutside: true,
      }),
    ).toBe(true);
    expect(
      canDismissOverlay("button", {
        busy: false,
        closeOnEscape: false,
        closeOnOutside: false,
      }),
    ).toBe(true);
  });

  it("возвращает фокус только живому trigger", () => {
    const focus = vi.fn();
    restoreOverlayFocus({ isConnected: true, focus } as unknown as HTMLElement);
    restoreOverlayFocus({
      isConnected: false,
      focus,
    } as unknown as HTMLElement);
    expect(focus).toHaveBeenCalledTimes(1);
  });
});

describe("OverlayPanel markup", () => {
  it("телепортирует responsive dialog с доступным именем", async () => {
    const app = createSSRApp({
      render: () =>
        h(
          OverlayPanel,
          {
            ariaLabel: "Контекстная панель",
            closeLabel: "Закрыть",
            mode: "responsive",
            open: true,
          },
          {
            default: () => h("p", "Содержимое"),
            header: () => h("h2", "Заголовок"),
          },
        ),
    });
    const context: SSRContext = {};

    await renderToString(app, context);
    const html = context.teleports?.body ?? "";

    expect(html).toContain('role="dialog"');
    expect(html).toContain('aria-label="Контекстная панель"');
    expect(html).toContain("overlay-panel--responsive");
    expect(html).toContain("Содержимое");
  });
});
