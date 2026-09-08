import { ssrContextKey } from "vue";
import { effectScope, nextTick, ref, shallowRef, type Ref } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createRenderer, defineComponent, type SetupContext } from "vue";
import AsyncEntityPicker from "./AsyncEntityPicker.vue";
import { reactive } from "vue";
import {
  useAsyncEntityCollection,
  useCursorInfiniteScroll,
  type AsyncEntityLoadRequest,
} from "./async-entity-picker";
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
describe("смена контекста async selector", () => {
  it("отменяет старую страницу и игнорирует loader, не соблюдающий abort", async () => {
    vi.useFakeTimers();
    let finish!: (value: { items: { id: string; label: string }[] }) => void;
    const first = vi.fn(
      (request: AsyncEntityLoadRequest) =>
        new Promise<{ items: { id: string; label: string }[] }>((resolve) => {
          void request;
          finish = resolve;
        }),
    );
    const second = vi
      .fn()
      .mockResolvedValue({ items: [{ id: "new", label: "Новый" }] });
    const props = reactive({
      contextKey: "project_one",
      loadItems: first,
      modelValue: "old",
    });
    let state!: Record<string, unknown>;
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
    const source = AsyncEntityPicker as unknown as {
      setup(props: unknown, context: SetupContext): Record<string, unknown>;
    };
    const app = renderer.createApp(
      defineComponent({
        setup(_props, context) {
          state = source.setup(props, context);
          return () => null;
        },
      }),
    );
    app.provide(ssrContextKey, {});
    app.mount({});
    await vi.advanceTimersByTimeAsync(500);
    props.contextKey = "project_two";
    props.loadItems = second;
    await vi.advanceTimersByTimeAsync(0);
    expect(first.mock.calls[0]?.[0].signal.aborted).toBe(true);
    finish({ items: [{ id: "old", label: "Старый" }] });
    await vi.advanceTimersByTimeAsync(0);
    expect(
      (state.items as Ref<{ id: string }[]>).value.map((item) => item.id),
    ).toEqual(["new"]);
    expect((state.query as Ref<string>).value).toBe("");
    app.unmount();
  });
  it("cancel очищает cursor и запрещает догрузку после закрытия", async () => {
    vi.useFakeTimers();
    const loader = vi.fn().mockResolvedValue({
      items: [{ id: "one", label: "Один" }],
      nextCursor: "next",
      total: 10,
    });
    const scope = effectScope();
    const collection = scope.run(() => useAsyncEntityCollection(loader));
    if (!collection) throw new Error("Missing collection");
    await vi.runAllTimersAsync();
    collection.cancel(true);
    await collection.loadMore();
    expect(loader).toHaveBeenCalledOnce();
    expect(collection.items.value).toEqual([]);
    expect(collection.total.value).toBeUndefined();
    scope.stop();
  });
  it("наблюдает уже установленный sentinel и не читает скрытую историю", async () => {
    const callbacks: IntersectionObserverCallback[] = [];
    const observe = vi.fn();
    const disconnect = vi.fn();
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(callback: IntersectionObserverCallback) {
          callbacks.push(callback);
        }
        observe = observe;
        disconnect = disconnect;
      },
    );
    const enabled = ref(true);
    const root = shallowRef({} as HTMLElement);
    const sentinel = shallowRef({} as Element);
    const more = vi.fn();
    const scope = effectScope();
    scope.run(() =>
      useCursorInfiniteScroll({ root, sentinel, enabled, loadMore: more }),
    );
    expect(observe).toHaveBeenCalledOnce();
    enabled.value = false;
    await nextTick();
    callbacks[0]?.(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );
    expect(more).not.toHaveBeenCalled();
    enabled.value = true;
    await nextTick();
    expect(observe).toHaveBeenCalledTimes(2);
    scope.stop();
    expect(disconnect).toHaveBeenCalledTimes(2);
  });
});
