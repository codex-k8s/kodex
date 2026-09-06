import { createSSRApp, h } from "vue";
import { renderToString, type SSRContext } from "@vue/server-renderer";
import { describe, expect, it, vi } from "vitest";

import DismissiblePopover from "@/shared/ui/DismissiblePopover.vue";
import {
  canDismissPopover,
  calculatePopoverPosition,
  restorePopoverFocus,
  shouldRestorePopoverFocus,
} from "@/shared/ui/dismissible-popover";

describe("dismissible popover policy", () => {
  it("раздельно применяет outside и Escape policy", () => {
    expect(
      canDismissPopover("escape", {
        closeOnEscape: true,
        closeOnOutside: false,
      }),
    ).toBe(true);
    expect(
      canDismissPopover("outside", {
        closeOnEscape: true,
        closeOnOutside: false,
      }),
    ).toBe(false);
  });

  it("возвращает фокус после Escape, но не перехватывает outside click", () => {
    const focus = vi.fn();
    const target = { isConnected: true, focus } as unknown as HTMLElement;

    restorePopoverFocus(target, shouldRestorePopoverFocus("escape"));
    restorePopoverFocus(target, shouldRestorePopoverFocus("outside"));

    expect(focus).toHaveBeenCalledTimes(1);
  });
});

describe("calculatePopoverPosition", () => {
  it("не оставляет сжатый dropdown под якорем при наличии места сверху", () => {
    for (const panelHeight of [0, 50, 220]) {
      const position = calculatePopoverPosition({
        anchor: { top: 740, bottom: 790, left: 20, right: 370, width: 350 },
        panelWidth: 350,
        panelHeight,
        viewportWidth: 390,
        viewportHeight: 860,
        placement: "bottom-start",
      });
      expect(position.side).toBe("top");
      expect(position.maxHeight).toBeGreaterThan(220);
      expect(position.top).toBeGreaterThanOrEqual(8);
    }
  });
  it("удерживает popover внутри viewport по горизонтали", () => {
    const position = calculatePopoverPosition({
      anchor: { bottom: 60, left: 290, right: 320, top: 30, width: 30 },
      panelHeight: 120,
      panelWidth: 180,
      placement: "bottom-start",
      viewportHeight: 480,
      viewportWidth: 320,
    });

    expect(position.left).toBe(132);
    expect(position.side).toBe("bottom");
  });

  it("переворачивает panel вверх при нехватке места снизу", () => {
    const position = calculatePopoverPosition({
      anchor: { bottom: 460, left: 40, right: 120, top: 430, width: 80 },
      panelHeight: 200,
      panelWidth: 220,
      placement: "bottom-start",
      viewportHeight: 480,
      viewportWidth: 640,
    });

    expect(position.side).toBe("top");
    expect(position.top).toBe(224);
    expect(position.maxHeight).toBe(416);
  });

  it("учитывает end alignment и ограничивает высоту доступным viewport", () => {
    const position = calculatePopoverPosition({
      anchor: { bottom: 84, left: 240, right: 304, top: 52, width: 64 },
      panelHeight: 600,
      panelWidth: 280,
      placement: "bottom-end",
      viewportHeight: 300,
      viewportWidth: 320,
    });

    expect(position.left).toBe(24);
    expect(position.side).toBe("bottom");
    expect(position.maxHeight).toBe(202);
    expect(position.top).toBe(90);
  });
});

describe("DismissiblePopover", () => {
  it("публикует доступные trigger attributes и teleport panel", async () => {
    const app = createSSRApp({
      render: () =>
        h(
          DismissiblePopover,
          {
            ariaLabel: "Доступные действия",
            block: true,
            contained: true,
            open: true,
            role: "menu",
          },
          {
            default: () => h("button", { type: "button" }, "Действие"),
            trigger: ({ attrs }: { attrs: Record<string, unknown> }) =>
              h("button", { ...attrs, type: "button" }, "Открыть"),
          },
        ),
    });
    const context: SSRContext = {};

    const html = await renderToString(app, context);
    const teleported = context.teleports?.body ?? "";

    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain('aria-haspopup="menu"');
    expect(html).toContain("dismissible-popover__anchor--block");
    expect(teleported).toContain('role="menu"');
    expect(teleported).toContain('aria-label="Доступные действия"');
    expect(teleported).toContain("dismissible-popover--contained");
  });
});
