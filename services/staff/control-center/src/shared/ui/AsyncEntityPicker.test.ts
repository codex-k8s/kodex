import { createSSRApp, effectScope, h, nextTick } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { afterEach, describe, expect, it, vi } from "vitest";

import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
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

  it("добавляет cursor-страницу без дублей и не запускает append дважды", async () => {
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
      items: [
        { id: "one", label: "Один обновлён", revision: 2 },
        { id: "two", label: "Два", revision: 1 },
      ],
    });
    await Promise.all([firstAppend, duplicateAppend]);

    expect(collection.items.value).toEqual([
      { id: "one", label: "Один обновлён", revision: 2 },
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
            common: { loading: "Загрузка", retry: "Повторить", empty: "Пусто" },
            errors: { default: "Ошибка" },
            runtime: { pickerShown: "Показано: {count}", pickerScroll: "Ещё" },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Офисные документы");
    expect(html).toContain("rev 4 · готово");
    expect(html).not.toContain("renv_internal_ref");
    expect(html).toContain('aria-haspopup="dialog"');
    expect(html).toContain('aria-label="Рабочее окружение"');
  });
});
