import { readFileSync } from "node:fs";
import { computed, ssrContextKey, watch } from "vue";
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
  it("files observer использует текущие scalar filters и authoritative revision без append state", () => {
    const source = readFileSync(
      new URL("../../features/files/FilesWorkspace.vue", import.meta.url),
      "utf8",
    );
    const context = source.slice(
      source.indexOf("const infiniteScrollContext = computed"),
      source.indexOf("function formatBytes"),
    );
    for (const field of [
      "props.projectRef",
      "collectionTab.value",
      "kind.value",
      "scanState.value",
      "source.value",
      "query.value",
      "artifactRealtimeRevision.value",
    ])
      expect(context).toContain(field);
    expect(context).toContain("contextKey: infiniteScrollContext");
    expect(context).not.toContain("items.value");
  });
  it("очистка query перенаблюдает snapshot даже при схлопнувшемся null→same cursor", async () => {
    const observe = vi.fn();
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        observe = observe;
        disconnect() {}
      },
    );
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection(vi.fn(), { immediate: false }),
    );
    if (!collection) throw new Error("Missing collection");
    const snapshot = {
      items: [{ id: "prefix", label: "Префикс" }],
      nextCursor: "next",
    };
    collection.applySnapshot(snapshot);
    const contextKey = computed(() => collection.query.value);
    scope.run(() => {
      watch(collection.query, () => collection.applySnapshot(snapshot), {
        flush: "sync",
      });
      useCursorInfiniteScroll({
        sentinel: shallowRef({} as Element),
        enabled: collection.hasMore,
        contextKey,
        loadMore: collection.loadMore,
      });
    });
    collection.query.value = "search";
    await nextTick();
    expect(collection.hasMore.value).toBe(true);
    expect(observe).toHaveBeenCalledTimes(2);
    collection.query.value = "";
    await nextTick();
    expect(collection.hasMore.value).toBe(true);
    expect(observe).toHaveBeenCalledTimes(3);
    scope.stop();
  });
  it("перенаблюдает новый scope при прежних sentinel/cursor, не повторяет append или duplicate snapshot", async () => {
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
    let finishOld!: (page: { items: { id: string; label: string }[] }) => void;
    const loader = vi.fn((request: AsyncEntityLoadRequest) => {
      void request;
      if (loader.mock.calls.length === 1)
        return new Promise<{ items: { id: string; label: string }[] }>(
          (resolve) => {
            finishOld = resolve;
          },
        );
      return Promise.resolve({
        items: [{ id: "tail", label: "Хвост" }],
        nextCursor: "last",
      });
    });
    const scope = effectScope();
    const contextKey = ref("FILES");
    const sentinel = shallowRef({} as Element);
    const collection = scope.run(() =>
      useAsyncEntityCollection(loader, { immediate: false }),
    );
    if (!collection) throw new Error("Missing collection");
    const snapshot = {
      items: [{ id: "prefix", label: "Префикс" }],
      nextCursor: "next",
    };
    collection.applySnapshot(snapshot);
    scope.run(() =>
      useCursorInfiniteScroll({
        sentinel,
        enabled: collection.hasMore,
        contextKey,
        loadMore: collection.loadMore,
      }),
    );
    const intersect = () => [
      { isIntersecting: true } as IntersectionObserverEntry,
    ];
    callbacks[0]?.(intersect(), {} as IntersectionObserver);
    expect(loader).toHaveBeenCalledOnce();
    collection.applySnapshot(snapshot);
    contextKey.value = "RESULTS";
    await nextTick();
    expect(loader.mock.calls[0]?.[0].signal.aborted).toBe(true);
    expect(observe).toHaveBeenCalledTimes(2);
    finishOld({ items: [{ id: "obsolete", label: "Устаревший" }] });
    await Promise.resolve();
    callbacks[0]?.(intersect(), {} as IntersectionObserver);
    expect(loader).toHaveBeenCalledOnce();
    callbacks[1]?.(intersect(), {} as IntersectionObserver);
    await Promise.resolve();
    expect(collection.items.value.map((item) => item.id)).toEqual([
      "prefix",
      "tail",
    ]);
    await nextTick();
    expect(observe).toHaveBeenCalledTimes(2);
    collection.applySnapshot(snapshot);
    await nextTick();
    expect(observe).toHaveBeenCalledTimes(2);
    collection.cancel(true);
    await nextTick();
    callbacks[1]?.(intersect(), {} as IntersectionObserver);
    expect(loader).toHaveBeenCalledTimes(2);
    scope.stop();
    expect(disconnect).toHaveBeenCalledTimes(2);
    callbacks[1]?.(intersect(), {} as IntersectionObserver);
    expect(loader).toHaveBeenCalledTimes(2);
  });
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
    const roots: (Element | Document | null)[] = [];
    const observe = vi.fn();
    const disconnect = vi.fn();
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(
          callback: IntersectionObserverCallback,
          init?: IntersectionObserverInit,
        ) {
          callbacks.push(callback);
          roots.push(init?.root ?? null);
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
    expect(roots).toEqual([null]);
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
    expect(roots).toEqual([null, null]);
    scope.stop();
    expect(disconnect).toHaveBeenCalledTimes(2);
  });
  it("сохраняет локальный root, только когда sentinel находится внутри него", () => {
    const roots: (Element | Document | null)[] = [];
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(
          _callback: IntersectionObserverCallback,
          init?: IntersectionObserverInit,
        ) {
          roots.push(init?.root ?? null);
        }
        observe() {}
        disconnect() {}
      },
    );
    const sentinel = {} as Element;
    const containedRoot = {
      contains: (target: Node) => target === sentinel,
    } as HTMLElement;
    const scope = effectScope();
    scope.run(() =>
      useCursorInfiniteScroll({
        root: shallowRef(containedRoot),
        sentinel: shallowRef(sentinel),
        enabled: true,
        loadMore: vi.fn(),
      }),
    );
    expect(roots).toEqual([containedRoot]);
    scope.stop();
  });
});
