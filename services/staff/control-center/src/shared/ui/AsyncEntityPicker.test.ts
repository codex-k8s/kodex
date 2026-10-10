import { readFileSync } from "node:fs";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  effectScope,
  h,
  nextTick,
  reactive,
  ssrContextKey,
  type ComponentPublicInstance,
  type Ref,
  type SetupContext,
} from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { afterEach, describe, expect, it, vi } from "vitest";

import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import { AppProblem } from "@/shared/api/problem";
import {
  createCursorIntersectionHandler,
  nearScrollEnd,
  useAsyncEntityCollection,
  virtualWindow,
  type AsyncEntityLoadRequest,
  type AsyncEntityPickerItem,
  type AsyncEntityPage,
} from "@/shared/ui/async-entity-picker";

interface TestItem extends AsyncEntityPickerItem {
  revision: number;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

afterEach(() => {
  vi.useRealTimers();
});

describe("useAsyncEntityCollection", () => {
  it("явный refresh сохраняет snapshot при UNAVAILABLE и заменяет его после retry", async () => {
    vi.useFakeTimers();
    const unavailable = new AppProblem({
      status: 503,
      code: "UNAVAILABLE",
      kind: "unavailable",
      retryable: true,
    });
    const loader = vi
      .fn()
      .mockResolvedValueOnce({
        items: [{ id: "old", label: "Прежний" }],
        total: 1,
        nextCursor: "old-cursor",
      })
      .mockRejectedValueOnce(unavailable)
      .mockResolvedValueOnce({
        items: [{ id: "new", label: "Новый" }],
        total: 1,
      });
    const scope = effectScope();
    const collection = scope.run(() => useAsyncEntityCollection(loader));
    if (!collection) throw new Error("Missing collection");
    await vi.runAllTimersAsync();
    collection.refreshPreservingItems();
    expect(collection.items.value.map((item) => item.id)).toEqual(["old"]);
    expect(collection.total.value).toBe(1);
    expect(collection.hasMore.value).toBe(false);
    await vi.runAllTimersAsync();
    expect(collection.items.value.map((item) => item.id)).toEqual(["old"]);
    expect(collection.error.value).toBe(unavailable);
    collection.refreshPreservingItems();
    await vi.runAllTimersAsync();
    expect(collection.items.value.map((item) => item.id)).toEqual(["new"]);
    expect(collection.error.value).toBeUndefined();
    collection.query.value = "другой";
    expect(collection.items.value).toEqual([]);
    scope.stop();
  });

  it.each(["unauthorized", "forbidden", "not-found"] as const)(
    "явный refresh очищает snapshot после %s",
    async (kind) => {
      vi.useFakeTimers();
      const loader = vi
        .fn()
        .mockResolvedValueOnce({
          items: [{ id: "old", label: "Прежний" }],
          total: 1,
        })
        .mockRejectedValueOnce(
          new AppProblem({
            status: 403,
            code: "FORBIDDEN",
            kind,
            retryable: false,
          }),
        );
      const scope = effectScope();
      const collection = scope.run(() => useAsyncEntityCollection(loader));
      if (!collection) throw new Error("Missing collection");
      await vi.runAllTimersAsync();
      collection.refreshPreservingItems();
      await vi.runAllTimersAsync();
      expect(collection.items.value).toEqual([]);
      expect(collection.total.value).toBeUndefined();
      expect(collection.phase.value).toBe("error");
      scope.stop();
    },
  );

  it("свежий snapshot отменяет сохраняющий refresh и не принимает его поздний ответ", async () => {
    vi.useFakeTimers();
    const old = deferred<AsyncEntityPage<TestItem>>();
    const loader = vi.fn().mockReturnValue(old.promise);
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection<TestItem>(loader, { immediate: false }),
    );
    if (!collection) throw new Error("Missing collection");
    collection.refreshPreservingItems();
    await vi.advanceTimersByTimeAsync(0);
    collection.applySnapshot({
      items: [{ id: "fresh", label: "Свежий", revision: 2 }],
    });
    const request = loader.mock.calls[0]?.[0] as AsyncEntityLoadRequest;
    expect(request.signal.aborted).toBe(true);
    old.resolve({ items: [{ id: "late", label: "Поздний", revision: 1 }] });
    await vi.runAllTimersAsync();
    expect(collection.items.value.map((item) => item.id)).toEqual(["fresh"]);
    scope.stop();
  });

  it("сохраняет серверный total и сбрасывает его вместе с query snapshot", async () => {
    vi.useFakeTimers();
    const loader = vi
      .fn()
      .mockResolvedValueOnce({
        items: [{ id: "one", label: "Один" }],
        total: 81,
        nextCursor: "next",
      })
      .mockResolvedValueOnce({ items: [], total: 0 });
    const scope = effectScope();
    const collection = scope.run(() => useAsyncEntityCollection(loader));
    if (!collection) throw new Error("Missing collection");
    await vi.runAllTimersAsync();
    expect(collection.total.value).toBe(81);
    collection.query.value = "новый";
    expect(collection.total.value).toBeUndefined();
    await vi.runAllTimersAsync();
    expect(collection.total.value).toBe(0);
    scope.stop();
  });
  it("отбрасывает собранные страницы при уменьшившемся total и перечитывает первый snapshot", async () => {
    vi.useFakeTimers();
    const loader = vi
      .fn()
      .mockResolvedValueOnce({
        items: [{ id: "one", label: "Один" }],
        total: 2,
        nextCursor: "next",
      })
      .mockResolvedValueOnce({ items: [{ id: "two", label: "Два" }], total: 1 })
      .mockResolvedValueOnce({
        items: [{ id: "fresh", label: "Свежий" }],
        total: 1,
      });
    const scope = effectScope();
    const collection = scope.run(() => useAsyncEntityCollection(loader));
    if (!collection) throw new Error("Missing collection");
    await vi.runAllTimersAsync();
    await collection.loadMore();
    await vi.runAllTimersAsync();
    expect(loader).toHaveBeenCalledTimes(3);
    expect(collection.items.value.map((item) => item.id)).toEqual(["fresh"]);
    expect(collection.total.value).toBe(1);
    scope.stop();
  });
  it("сбрасывает stale snapshot при догрузке и не зацикливает отказ первой страницы", async () => {
    vi.useFakeTimers();
    const conflict = new AppProblem({
      status: 412,
      code: "VERSION_OR_STATE_CONFLICT",
      kind: "conflict",
      retryable: true,
    });
    const loader = vi
      .fn()
      .mockResolvedValueOnce({
        items: [{ id: "old", label: "Старый" }],
        nextCursor: "snapshot",
      })
      .mockRejectedValueOnce(conflict)
      .mockRejectedValueOnce(conflict);
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection(loader, { debounceMs: 0 }),
    );
    if (!collection) throw new Error("Missing collection scope");
    await vi.runAllTimersAsync();
    await collection.loadMore();
    await vi.runAllTimersAsync();
    expect(loader).toHaveBeenCalledTimes(3);
    expect(
      loader.mock.calls.map(
        (call) => (call[0] as AsyncEntityLoadRequest).cursor,
      ),
    ).toEqual([undefined, "snapshot", undefined]);
    expect(collection.items.value).toEqual([]);
    expect(collection.phase.value).toBe("error");
    scope.stop();
  });
  it("отменяет незавершённый запрос и допускает новое открытие", async () => {
    vi.useFakeTimers();
    const first = deferred<AsyncEntityPage<TestItem>>();
    const loader = vi
      .fn<
        (request: AsyncEntityLoadRequest) => Promise<AsyncEntityPage<TestItem>>
      >()
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce({
        items: [{ id: "new", label: "Новое", revision: 2 }],
      });
    const scope = effectScope();
    const collection = scope.run(() => useAsyncEntityCollection(loader));
    if (!collection) throw new Error("Missing collection");
    await vi.advanceTimersByTimeAsync(500);
    collection.cancel();
    expect(loader.mock.calls[0]?.[0].signal.aborted).toBe(true);
    first.resolve({ items: [{ id: "old", label: "Устаревшее", revision: 1 }] });
    await vi.advanceTimersByTimeAsync(1);
    expect(collection.items.value).toEqual([]);
    collection.refresh();
    await vi.advanceTimersByTimeAsync(1);
    expect(collection.items.value.map((item) => item.id)).toEqual(["new"]);
    scope.stop();
  });
  it("по умолчанию ждёт 500 ms и атомарно отклоняет повторный cursor", async () => {
    vi.useFakeTimers();
    const loader = vi
      .fn()
      .mockResolvedValueOnce({
        items: [{ id: "one", label: "Один" }],
        nextCursor: "same",
      })
      .mockResolvedValueOnce({
        items: [{ id: "two", label: "Два" }],
        nextCursor: "same",
      });
    const scope = effectScope();
    const collection = scope.run(() => useAsyncEntityCollection(loader));
    if (!collection) throw new Error("Missing collection");
    await vi.advanceTimersByTimeAsync(499);
    expect(loader).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    await collection.loadMore();
    expect(collection.items.value.map((item) => item.id)).toEqual(["one"]);
    expect(collection.loadMoreError.value).toBe(true);
    scope.stop();
  });

  it.each([
    null,
    {},
    { items: [null] },
    { items: [], nextCursor: 0 },
    {
      items: [
        { id: "one", label: "Один" },
        { id: "one", label: "Дубликат" },
      ],
    },
  ])("отклоняет поврежденную страницу %j", async (page) => {
    vi.useFakeTimers();
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection(vi.fn().mockResolvedValue(page)),
    );
    if (!collection) throw new Error("Missing collection");
    await vi.advanceTimersByTimeAsync(500);
    expect(collection.phase.value).toBe("error");
    expect(collection.items.value).toEqual([]);
    scope.stop();
  });
  it("debounce-ит серверный поиск и публикует только актуальный ответ", async () => {
    vi.useFakeTimers();
    const first = deferred<AsyncEntityPage<TestItem>>();
    const second = deferred<AsyncEntityPage<TestItem>>();
    const loader = vi
      .fn<
        (request: AsyncEntityLoadRequest) => Promise<AsyncEntityPage<TestItem>>
      >()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection(loader, { debounceMs: 250, immediate: false }),
    );
    if (!collection) throw new Error("collection was not created");

    collection.query.value = "первый";
    await vi.advanceTimersByTimeAsync(250);
    expect(loader).toHaveBeenCalledTimes(1);

    collection.query.value = "второй";
    await vi.advanceTimersByTimeAsync(249);
    expect(loader).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(loader).toHaveBeenCalledTimes(2);

    first.resolve({
      items: [{ id: "old", label: "Старый", revision: 1 }],
    });
    second.resolve({
      items: [{ id: "new", label: "Новый", revision: 2 }],
    });
    await Promise.all([first.promise, second.promise]);
    await nextTick();

    expect(collection.items.value.map((item) => item.id)).toEqual(["new"]);
    expect(collection.phase.value).toBe("ready");
    scope.stop();
  });

  it("добавляет cursor-страницу и не запускает append дважды", async () => {
    vi.useFakeTimers();
    const append = deferred<AsyncEntityPage<TestItem>>();
    const loader = vi
      .fn<
        (request: AsyncEntityLoadRequest) => Promise<AsyncEntityPage<TestItem>>
      >()
      .mockResolvedValueOnce({
        items: [{ id: "one", label: "Один", revision: 1 }],
        nextCursor: "cursor-2",
      })
      .mockReturnValueOnce(append.promise);
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection(loader, { debounceMs: 0, immediate: false }),
    );
    if (!collection) throw new Error("collection was not created");

    collection.query.value = "каталог";
    await vi.advanceTimersByTimeAsync(0);
    await nextTick();
    expect(collection.hasMore.value).toBe(true);
    expect(loader).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ cursor: undefined, query: "каталог" }),
    );

    const firstAppend = collection.loadMore();
    const duplicateAppend = collection.loadMore();
    expect(loader).toHaveBeenCalledTimes(2);
    expect(loader).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ cursor: "cursor-2", query: "каталог" }),
    );
    append.resolve({
      items: [{ id: "two", label: "Два", revision: 1 }],
    });
    await Promise.all([firstAppend, duplicateAppend]);

    expect(collection.items.value).toEqual([
      { id: "one", label: "Один", revision: 1 },
      { id: "two", label: "Два", revision: 1 },
    ]);
    expect(collection.hasMore.value).toBe(false);
    scope.stop();
  });

  it("различает ошибку, пустой результат и retry", async () => {
    vi.useFakeTimers();
    const loader = vi
      .fn<
        (request: AsyncEntityLoadRequest) => Promise<AsyncEntityPage<TestItem>>
      >()
      .mockRejectedValueOnce(new Error("unavailable"))
      .mockResolvedValueOnce({ items: [] });
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection(loader, { debounceMs: 0, immediate: false }),
    );
    if (!collection) throw new Error("collection was not created");

    collection.query.value = "ошибка";
    await vi.advanceTimersByTimeAsync(0);
    expect(collection.phase.value).toBe("error");

    collection.refresh();
    await vi.advanceTimersByTimeAsync(0);
    expect(collection.phase.value).toBe("empty");
    scope.stop();
  });

  it("сохраняет загруженные элементы при ошибке cursor-страницы", async () => {
    vi.useFakeTimers();
    const loader = vi
      .fn<
        (request: AsyncEntityLoadRequest) => Promise<AsyncEntityPage<TestItem>>
      >()
      .mockResolvedValueOnce({
        items: [{ id: "one", label: "Один", revision: 1 }],
        nextCursor: "cursor-2",
      })
      .mockRejectedValueOnce(new Error("next page unavailable"))
      .mockResolvedValueOnce({
        items: [{ id: "two", label: "Два", revision: 1 }],
      });
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection(loader, { debounceMs: 0, immediate: false }),
    );
    if (!collection) throw new Error("collection was not created");

    collection.query.value = "каталог";
    await vi.advanceTimersByTimeAsync(0);
    await collection.loadMore();

    expect(collection.items.value.map((item) => item.id)).toEqual(["one"]);
    expect(collection.loadMoreError.value).toBe(true);
    expect(collection.phase.value).toBe("ready");

    await collection.loadMore();
    expect(collection.items.value.map((item) => item.id)).toEqual([
      "one",
      "two",
    ]);
    expect(collection.loadMoreError.value).toBe(false);
    scope.stop();
  });

  it("принимает realtime-снимок и продолжает cursor-догрузку", async () => {
    const loader = vi.fn().mockResolvedValue({
      items: [{ id: "two", label: "Два", revision: 1 }],
    });
    const scope = effectScope();
    const collection = scope.run(() =>
      useAsyncEntityCollection<TestItem>(loader, { immediate: false }),
    );
    if (!collection) throw new Error("collection was not created");

    collection.applySnapshot({
      items: [{ id: "one", label: "Один", revision: 1 }],
      nextCursor: "cursor-2",
    });
    expect(collection.phase.value).toBe("ready");
    expect(collection.hasMore.value).toBe(true);

    collection.upsert({ id: "one", label: "Один v2", revision: 2 });
    await collection.loadMore();
    expect(loader).toHaveBeenCalledWith(
      expect.objectContaining({ cursor: "cursor-2" }),
    );
    expect(collection.items.value.map((item) => item.label)).toEqual([
      "Один v2",
      "Два",
    ]);

    collection.remove("one");
    expect(collection.items.value.map((item) => item.id)).toEqual(["two"]);
    scope.stop();
  });
});

describe("cursor infinite scroll", () => {
  it("запрашивает следующую страницу только для видимого sentinel", () => {
    const loadMore = vi.fn();
    const enabled = vi.fn(() => true);
    const handler = createCursorIntersectionHandler(enabled, loadMore);

    handler(
      [{ isIntersecting: false } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );
    handler(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );

    expect(loadMore).toHaveBeenCalledTimes(1);
    expect(
      nearScrollEnd({ clientHeight: 200, scrollHeight: 500, scrollTop: 205 }),
    ).toBe(true);
    expect(
      nearScrollEnd({ clientHeight: 200, scrollHeight: 500, scrollTop: 100 }),
    ).toBe(false);
  });
});

describe("virtual window", () => {
  it("ограничивает DOM видимыми строками списка с overscan", () => {
    expect(
      virtualWindow({
        itemCount: 1_000,
        itemHeight: 64,
        scrollTop: 6_400,
        viewportHeight: 384,
        overscan: 2,
      }),
    ).toEqual({
      startIndex: 98,
      endIndex: 108,
      paddingBefore: 6_272,
      paddingAfter: 57_088,
    });
  });

  it("виртуализирует сетку целыми строками и сохраняет общий размер", () => {
    const window = virtualWindow({
      itemCount: 101,
      columns: 3,
      itemHeight: 198,
      scrollTop: 1_980,
      viewportHeight: 396,
      overscan: 1,
    });

    expect(window.startIndex % 3).toBe(0);
    expect(window.endIndex - window.startIndex).toBeLessThanOrEqual(12);
    expect(window).toEqual({
      startIndex: 27,
      endIndex: 39,
      paddingBefore: 1_782,
      paddingAfter: 4_158,
    });
  });

  it("ограничивает окно последними строками после сокращения выборки", () => {
    expect(
      virtualWindow({
        itemCount: 7,
        columns: 3,
        itemHeight: 198,
        scrollTop: 20_000,
        viewportHeight: 396,
        overscan: 1,
      }),
    ).toEqual({
      startIndex: 3,
      endIndex: 7,
      paddingBefore: 198,
      paddingAfter: 0,
    });
  });
});

describe("AsyncEntityPicker", () => {
  function mountKeyboardPicker(loader: ReturnType<typeof vi.fn>) {
    const props = reactive({ loadPage: loader, disabled: false });
    interface State {
      handlePopoverOpen(value: boolean): void;
      handleTriggerArrowDown(): void;
      handlePopoverReady(): void;
      loadMore(): Promise<void>;
      items: Ref<{ id: string }[]>;
      activeIndex: Ref<number>;
      open: Ref<boolean>;
      searchInput: Ref<HTMLInputElement | undefined>;
    }
    let state!: State;
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
      setup(props: unknown, context: SetupContext): State;
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
    return { state, props, unmount: () => app.unmount() };
  }

  it("повторное открытие сохраняет шесть строк, догруженный хвост и cursor", async () => {
    vi.useFakeTimers();
    const firstPage = Array.from({ length: 6 }, (_, index) => ({
      ref: `agent_${String(index)}`,
      title: `Сотрудник ${String(index)}`,
    }));
    const loader = vi
      .fn()
      .mockResolvedValueOnce({ items: firstPage, nextPageToken: "tail" })
      .mockResolvedValueOnce({
        items: [{ ref: "developer", title: "Разработчик" }],
        nextPageToken: "last",
      })
      .mockResolvedValueOnce({ items: [] });
    const picker = mountKeyboardPicker(loader);
    try {
      picker.state.handlePopoverOpen(true);
      await vi.runAllTimersAsync();
      await picker.state.loadMore();
      await nextTick();
      picker.state.activeIndex.value = 6;
      for (let index = 0; index < 6; index += 1)
        picker.state.handlePopoverOpen(true);
      picker.state.searchInput.value = {
        focus: vi.fn(),
      } as unknown as HTMLInputElement;
      for (let index = 0; index < 6; index += 1)
        picker.state.handleTriggerArrowDown();
      await vi.runAllTimersAsync();
      expect(loader).toHaveBeenCalledTimes(2);
      expect(picker.state.items.value.map((item) => item.id)).toEqual([
        ...firstPage.map((item) => item.ref),
        "developer",
      ]);
      expect(picker.state.activeIndex.value).toBe(6);
      await picker.state.loadMore();
      expect(loader.mock.calls[2]?.[1]).toBe("last");
    } finally {
      picker.unmount();
    }
  });

  it("ArrowDown фокусирует поиск без повторного чтения и не фокусирует закрытый или disabled dropdown", async () => {
    vi.useFakeTimers();
    const loader = vi.fn().mockResolvedValue({ items: [] });
    const picker = mountKeyboardPicker(loader);
    const focus = vi.fn();
    picker.state.searchInput.value = { focus } as unknown as HTMLInputElement;
    try {
      picker.state.handleTriggerArrowDown();
      expect(focus).not.toHaveBeenCalled();
      picker.state.handlePopoverReady();
      await vi.runAllTimersAsync();
      expect(focus).toHaveBeenCalledOnce();
      for (let index = 0; index < 6; index += 1)
        picker.state.handleTriggerArrowDown();
      await vi.runAllTimersAsync();
      expect(loader).toHaveBeenCalledOnce();
      expect(focus).toHaveBeenCalledTimes(7);
      picker.state.handlePopoverOpen(false);
      picker.state.handleTriggerArrowDown();
      picker.state.handlePopoverOpen(false);
      picker.state.handlePopoverReady();
      expect(focus).toHaveBeenCalledTimes(7);
      picker.props.disabled = true;
      picker.state.handleTriggerArrowDown();
      picker.state.handlePopoverReady();
      await vi.runAllTimersAsync();
      expect(picker.state.open.value).toBe(false);
      expect(loader).toHaveBeenCalledOnce();
      expect(focus).toHaveBeenCalledTimes(7);
      const source = readFileSync(
        new URL("./AsyncEntityPicker.vue", import.meta.url),
        "utf8",
      );
      expect(source).toContain(
        '@keydown.down.prevent="handleTriggerArrowDown"',
      );
      expect(source).toContain('@click="toggle"');
    } finally {
      picker.unmount();
    }
  });

  it("ограничивает dropdown пятью строками и сохраняет явную высоту и inline", async () => {
    for (const [inline, requestedHeight, expectedHeight] of [
      [false, undefined, 348],
      [false, 480, 480],
      [true, undefined, undefined],
    ] as const) {
      const heights: (number | undefined)[] = [];
      const app = createSSRApp(AsyncEntityPicker, {
        ...(inline
          ? { loadItems: () => Promise.resolve({ items: [] }) }
          : { loadPage: vi.fn() }),
        popoverMaxHeight: requestedHeight,
        labels: {
          label: "Выбор сущности",
          searchPlaceholder: "Найти",
          loading: "Загрузка",
          loadingMore: "Загружаем ещё",
          empty: "Пусто",
          error: "Ошибка",
          retry: "Повторить",
        },
      });
      app.mixin({
        created(this: ComponentPublicInstance) {
          if (this.$options.__name === "DismissiblePopover")
            heights.push((this.$props as { maxHeight?: number }).maxHeight);
        },
      });

      const html = await renderToString(app);

      expect(heights).toEqual(inline ? [] : [expectedHeight]);
      if (inline) expect(html).toContain("async-picker--inline");
      const source = readFileSync(
        new URL("./AsyncEntityPicker.vue", import.meta.url),
        "utf8",
      );
      expect(source).toContain(':max-height="popoverMaxHeight"');
      expect(source).toMatch(
        /\.async-picker__options \{[^}]*min-height: 0;[^}]*overflow-y: auto;/,
      );
    }
  });

  it("передаёт стабильный id элементов loadItems в dropdown-режиме", () => {
    const componentSource = readFileSync(
      new URL("./AsyncEntityPicker.vue", import.meta.url),
      "utf8",
    );
    expect(componentSource).toContain('emit("update:modelValue", item.id)');
    expect(componentSource).not.toContain("!isOption(item.source)");
  });

  it("рендерит доступный listbox и начальное состояние загрузки", async () => {
    const app = createSSRApp({
      render: () =>
        h(AsyncEntityPicker, {
          labels: {
            label: "Выбор сущности",
            searchPlaceholder: "Найти",
            loading: "Загрузка",
            loadingMore: "Загружаем ещё",
            empty: "Ничего не найдено",
            error: "Ошибка загрузки",
            retry: "Повторить",
          },
          loadItems: () => Promise.resolve({ items: [] }),
          modelValue: null,
        }),
    });

    const html = await renderToString(app);

    expect(html).toContain('role="listbox"');
    expect(html).toContain('role="combobox"');
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain('aria-label="Выбор сущности"');
    expect(html).toContain("Загрузка");
  });

  it("показывает понятное имя выбранной сущности без внутреннего ref", async () => {
    const app = createSSRApp(AsyncEntityPicker, {
      modelValue: "renv_internal_ref",
      selected: {
        ref: "renv_internal_ref",
        title: "Офисные документы",
        description: "rev 4 · готово",
      },
      loadPage: vi.fn(),
      triggerLabel: "Рабочее окружение",
      placeholder: "Выберите окружение",
      searchPlaceholder: "Поиск окружений",
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            common: {
              loading: "Загрузка",
              retry: "Повторить",
              empty: "Пусто",
              clearSelection: "Очистить выбор",
            },
            errors: { default: "Ошибка" },
            runtime: { pickerShown: "Показано: {count}", pickerScroll: "Ещё" },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Офисные документы");
    expect(html).not.toContain("rev 4 · готово");
    expect(html).not.toContain("<small");
    expect(html).not.toContain("renv_internal_ref");
    expect(html).toContain('aria-haspopup="dialog"');
    expect(html).toContain('aria-label="Рабочее окружение"');
  });

  it("закрытый селектор использует общую компактную высоту поля", () => {
    const source = readFileSync(
      new URL("./AsyncEntityPicker.vue", import.meta.url),
      "utf8",
    );
    const triggerStyle = source.match(
      /\.async-picker__trigger \{([^}]+)\}/,
    )?.[1];
    expect(triggerStyle).toContain("height: var(--control-height, 32px)");
    expect(triggerStyle).toContain("padding: 6px 9px");
  });

  it("не показывает очистку выбора для пустого строкового значения", async () => {
    const app = createSSRApp(AsyncEntityPicker, {
      modelValue: "",
      loadPage: vi.fn(),
      triggerLabel: "Цель",
      placeholder: "Выберите цель",
      searchPlaceholder: "Найти цель",
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            common: {
              loading: "Загрузка",
              retry: "Повторить",
              empty: "Пусто",
              clearSelection: "Очистить выбор",
            },
            errors: { default: "Ошибка" },
            runtime: { pickerShown: "Показано: {count}", pickerScroll: "Ещё" },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Выберите цель");
    expect(html).not.toContain('title="Очистить выбор"');
  });
});
