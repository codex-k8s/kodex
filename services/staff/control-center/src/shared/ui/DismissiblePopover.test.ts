import {
  createRenderer,
  createSSRApp,
  defineComponent,
  h,
  markRaw,
  nextTick,
  reactive,
  ssrContextKey,
  type CSSProperties,
  type Ref,
  type SetupContext,
} from "vue";
import { renderToString, type SSRContext } from "@vue/server-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";

import DismissiblePopover from "@/shared/ui/DismissiblePopover.vue";
import {
  canDismissPopover,
  calculatePopoverPosition,
  restorePopoverFocus,
  shouldRestorePopoverFocus,
} from "@/shared/ui/dismissible-popover";

afterEach(() => vi.unstubAllGlobals());

function mountPositionedPopover() {
  class Element {
    parent: Element | null = null;
    children: Element[] = [];
    style: Record<string, unknown> = {};
    attributes: Record<string, unknown> = {};
    clientHeight = 200;
    constructor(readonly tag = "div") {
      markRaw(this);
    }
    get isConnected(): boolean {
      return this === body || Boolean(this.parent?.isConnected);
    }
    closest() {
      return null;
    }
    contains(node: Element): boolean {
      return (
        node === this || this.children.some((child) => child.contains(node))
      );
    }
    getBoundingClientRect() {
      return {
        top: 40,
        bottom: 72,
        left: 20,
        right: 320,
        width: 300,
        height: 32,
      };
    }
    querySelectorAll() {
      return this.children.flatMap((child): Element[] => [
        ...(child.tag === "input" || child.tag === "button" ? [child] : []),
        ...child.querySelectorAll(),
      ]);
    }
    get hidden(): boolean {
      return this.style.visibility === "hidden" || Boolean(this.parent?.hidden);
    }
    focus() {
      if (this.hidden) return;
      documentState.activeElement = this;
    }
  }
  const body = new Element("body");
  const documentState = {
    body,
    activeElement: null as Element | null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  };
  vi.stubGlobal("document", documentState);
  vi.stubGlobal("HTMLElement", Element);
  vi.stubGlobal("window", {
    innerWidth: 390,
    innerHeight: 844,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  });
  const renderer = createRenderer<Element, Element>({
    patchProp(node, key, _previous, value: unknown) {
      if (key === "style")
        node.style = { ...(value as Record<string, unknown>) };
      else node.attributes[key] = value;
    },
    insert(node, parent, anchor) {
      if (node.parent)
        node.parent.children.splice(node.parent.children.indexOf(node), 1);
      node.parent = parent;
      const index = anchor ? parent.children.indexOf(anchor) : -1;
      parent.children.splice(
        index < 0 ? parent.children.length : index,
        0,
        node,
      );
    },
    remove(node) {
      node.parent?.children.splice(node.parent.children.indexOf(node), 1);
      node.parent = null;
    },
    createElement: (tag) => new Element(tag),
    createText: () => new Element("text"),
    createComment: () => new Element("comment"),
    setText() {},
    setElementText() {},
    parentNode: (node) => node.parent,
    nextSibling: (node) =>
      node.parent?.children[node.parent.children.indexOf(node) + 1] ?? null,
    querySelector: () => body,
  });
  const props = reactive({
    open: false,
    role: "dialog",
    placement: "bottom-start",
    width: "md",
    focusOnOpen: true,
    teleportTo: "body",
  });
  const ready = vi.fn(() => {
    const panel = body.children.find(
      (node) => node.attributes.role === "dialog",
    );
    expect(panel?.style.visibility).toBe("visible");
    expect(documentState.activeElement?.tag).toBe("input");
  });
  const source = DismissiblePopover as unknown as {
    setup(
      props: unknown,
      context: SetupContext,
    ): {
      anchor: Ref<Element | undefined>;
      panel: Ref<Element | undefined>;
      panelStyle: Ref<CSSProperties>;
    };
  };
  const app = renderer.createApp(
    defineComponent({
      emits: ["ready"],
      setup(_props, context) {
        const state = source.setup(props, context);
        return () => [
          h("span", { ref: state.anchor }),
          props.open
            ? h(
                "div",
                {
                  ref: state.panel,
                  role: "dialog",
                  style: state.panelStyle.value,
                },
                h("input", { type: "search" }),
              )
            : null,
        ];
      },
    }),
    { onReady: ready },
  );
  app.provide(ssrContextKey, {});
  app.mount(body);
  return { props, ready, documentState, unmount: () => app.unmount() };
}

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
  it("сообщает ready только после позиционирования и видимого DOM", async () => {
    const popover = mountPositionedPopover();
    try {
      popover.props.open = true;
      expect(popover.ready).not.toHaveBeenCalled();
      await nextTick();
      expect(popover.ready).not.toHaveBeenCalled();
      expect(popover.documentState.activeElement).toBeNull();
      await nextTick();
      await nextTick();
      expect(popover.ready).toHaveBeenCalledOnce();
    } finally {
      popover.unmount();
    }
  });

  it("закрытие до видимого DOM отменяет поздний ready и focus", async () => {
    const popover = mountPositionedPopover();
    try {
      popover.props.open = true;
      await nextTick();
      popover.props.open = false;
      await nextTick();
      await nextTick();
      expect(popover.ready).not.toHaveBeenCalled();
      expect(popover.documentState.activeElement).toBeNull();
    } finally {
      popover.unmount();
    }
  });

  it("публикует доступные trigger attributes и teleport panel", async () => {
    const app = createSSRApp({
      render: () =>
        h(
          DismissiblePopover,
          {
            ariaLabel: "Доступные действия",
            block: true,
            contained: true,
            maxHeight: 440,
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
    expect(teleported).toContain("max-height:440px");
  });
});
