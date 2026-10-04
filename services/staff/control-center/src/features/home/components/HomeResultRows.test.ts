import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createRenderer,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type ComputedRef,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import type { HomeResultItem } from "../result-catalog";
import HomeResultRows from "./HomeResultRows.vue";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

const template = readFileSync(
  new URL("./HomeResultRows.vue", import.meta.url),
  "utf8",
);

describe("HomeResultRows", () => {
  it("показывает краткое безопасное описание вместо полного результата запуска", () => {
    expect(template).toContain(
      ':content="visibleDescription(item)" :maximum-length="140"',
    );
    expect(template).not.toContain("{{ item.description }}");
  });
});

interface RowsState {
  visibleItems: ComputedRef<HomeResultItem[]>;
  hasMore: ComputedRef<boolean>;
  root: Ref<HTMLElement | undefined>;
  sentinel: Ref<HTMLElement | undefined>;
}

function items(count: number): HomeResultItem[] {
  return Array.from({ length: count }, (_, index) => ({
    ref: `run_fixture_${String(index)}`,
    title: `Запуск ${String(index)}`,
    description: "Текущая работа",
    state: "RUNNING",
    to: `/runs/run_fixture_${String(index)}`,
  }));
}

const disposers: (() => void)[] = [];
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
  vi.unstubAllGlobals();
});

async function mountRows(initialItems = items(12), dashboard = true) {
  const observers: {
    callback: IntersectionObserverCallback;
    options?: IntersectionObserverInit;
    disconnect: ReturnType<typeof vi.fn>;
  }[] = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(
        callback: IntersectionObserverCallback,
        options?: IntersectionObserverInit,
      ) {
        observers.push({ callback, options, disconnect: this.disconnect });
      }
      disconnect = vi.fn();
      observe = vi.fn();
    },
  );
  const props = reactive({
    items: initialItems,
    loading: false,
    dashboard,
    more: undefined as string | undefined,
  });
  type RowsProps = typeof props;
  const emit = vi.fn();
  let state!: RowsState;
  const component = HomeResultRows as unknown as {
    setup(props: RowsProps, context: SetupContext): RowsState;
  };
  const renderer = createRenderer<object, object>({
    patchProp() {},
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
  const app = renderer.createApp(
    defineComponent({
      setup(_props, context) {
        state = component.setup(props, { ...context, emit });
        return () => null;
      },
    }),
  );
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: { ru: {} },
    }),
  );
  app.provide(ssrContextKey, {});
  app.mount({});
  const dispose = () => app.unmount();
  disposers.push(dispose);
  state.sentinel.value = {} as HTMLElement;
  state.root.value = {
    contains: (value: unknown) => value === state.sentinel.value,
  } as HTMLElement;
  await nextTick();
  const intersect = () => {
    const observer = observers.at(-1);
    if (!observer) throw new Error("Synthetic scroll observer is missing");
    observer.callback(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );
  };
  return { props, state, emit, observers, intersect, dispose };
}

describe("Компактные каталоги Главной", () => {
  it("показывает пять записей и раскрывает следующую пятёрку внутри собственного scroll root", async () => {
    const { state, observers, intersect, emit, props } = await mountRows();
    expect(state.visibleItems.value).toEqual(props.items.slice(0, 5));
    expect(state.hasMore.value).toBe(true);
    expect(observers.at(-1)?.options?.root).toBe(state.root.value);
    intersect();
    await nextTick();
    expect(state.visibleItems.value).toEqual(props.items.slice(0, 10));
    intersect();
    await nextTick();
    expect(state.visibleItems.value).toEqual(props.items);
    expect(state.hasMore.value).toBe(false);
    expect(props.items).toHaveLength(12);
    expect(emit).not.toHaveBeenCalled();
  });

  it("обновляет изначально пустой каталог при realtime поступлении записей", async () => {
    const { state, props } = await mountRows([]);
    expect(state.visibleItems.value).toEqual([]);
    expect(state.hasMore.value).toBe(false);
    props.items = items(9);
    await nextTick();
    expect(state.visibleItems.value).toHaveLength(5);
    expect(state.hasMore.value).toBe(true);
  });

  it("не сохраняет удалённые realtime записи и не сбрасывает уже раскрытую порцию", async () => {
    const { state, props, intersect } = await mountRows();
    intersect();
    props.items = props.items.slice(3);
    await nextTick();
    expect(state.visibleItems.value).toEqual(props.items);
    expect(state.visibleItems.value[0]?.ref).toBe("run_fixture_3");
    expect(state.hasMore.value).toBe(false);
    props.items = items(13);
    await nextTick();
    expect(state.visibleItems.value).toHaveLength(10);
    expect(state.hasMore.value).toBe(true);
  });

  it("блокирует дозагрузку в loading и отключает observer при unmount", async () => {
    const { state, props, observers, intersect, dispose } = await mountRows();
    props.loading = true;
    intersect();
    await nextTick();
    expect(state.visibleItems.value).toHaveLength(5);
    props.loading = false;
    await nextTick();
    const observer = observers.at(-1);
    dispose();
    expect(observer?.disconnect).toHaveBeenCalledOnce();
    disposers.pop();
  });

  it("сохраняет обычный cursor-каталог без локального dashboard ограничения", async () => {
    const { state, props, observers, intersect, emit } = await mountRows(
      items(12),
      false,
    );
    expect(state.visibleItems.value).toHaveLength(12);
    expect(state.hasMore.value).toBe(false);
    props.more = "cursor_fixture";
    await nextTick();
    expect(state.hasMore.value).toBe(true);
    expect(observers.at(-1)?.options?.rootMargin).toBe("0px 0px 120px");
    intersect();
    expect(emit).toHaveBeenCalledWith("more", 8);
    expect(state.visibleItems.value).toHaveLength(12);
  });

  it("ограничивает viewport пятью стабильными строками без пустого footer и polling", () => {
    const style = template.match(
      /\.home-result-rows--dashboard \{([^}]+)\}/,
    )?.[1];
    const row = template.match(
      /\.home-result-rows--dashboard \.home-result-row \{([^}]+)\}/,
    )?.[1];
    expect(style).toContain("max-height: min(460px, 55vh)");
    expect(style).toContain("overflow-y: auto");
    expect(style).toContain("scrollbar-gutter: stable");
    expect(row).toContain("height: 92px");
    expect(template).toContain('v-if="hasMore"');
    expect(template).not.toContain("<footer");
    expect(template).not.toContain("setInterval");
  });
});
