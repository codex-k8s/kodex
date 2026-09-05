import {
  computed,
  onScopeDispose,
  ref,
  shallowRef,
  toValue,
  watch,
  type MaybeRefOrGetter,
  type Ref,
} from "vue";
import { AppProblem } from "@/shared/api/problem";

export interface AsyncEntityPickerItem {
  id: string;
  label: string;
  description?: string;
  disabled?: boolean;
}

export interface AsyncEntityLoadRequest {
  query: string;
  cursor?: string;
  signal: AbortSignal;
}

export interface AsyncEntityPage<T extends AsyncEntityPickerItem> {
  items: readonly T[];
  nextCursor?: string | null;
}

export type AsyncEntityLoader<T extends AsyncEntityPickerItem> = (
  request: AsyncEntityLoadRequest,
) => Promise<AsyncEntityPage<T>>;

export type AsyncEntityPickerPhase =
  | "initial-loading"
  | "ready"
  | "empty"
  | "error";

export interface AsyncEntityCollectionOptions {
  debounceMs?: number;
  immediate?: boolean;
}

export interface VirtualWindow {
  startIndex: number;
  endIndex: number;
  paddingBefore: number;
  paddingAfter: number;
}

export function virtualWindow(options: {
  itemCount: number;
  columns?: number;
  itemHeight: number;
  scrollTop: number;
  viewportHeight: number;
  overscan?: number;
}): VirtualWindow {
  const itemCount = Math.max(0, Math.floor(options.itemCount));
  const columns = Math.max(1, Math.floor(options.columns ?? 1));
  const itemHeight = Math.max(1, options.itemHeight);
  const rowCount = Math.ceil(itemCount / columns);
  const overscan = Math.max(0, Math.floor(options.overscan ?? 2));
  const firstVisibleRow = Math.min(
    Math.max(0, rowCount - 1),
    Math.floor(Math.max(0, options.scrollTop) / itemHeight),
  );
  const visibleRowCount = Math.max(
    1,
    Math.ceil(Math.max(0, options.viewportHeight) / itemHeight),
  );
  const startRow = Math.max(0, firstVisibleRow - overscan);
  const endRow = Math.min(
    rowCount,
    firstVisibleRow + visibleRowCount + overscan,
  );

  return {
    startIndex: Math.min(itemCount, startRow * columns),
    endIndex: Math.min(itemCount, endRow * columns),
    paddingBefore: startRow * itemHeight,
    paddingAfter: Math.max(0, (rowCount - endRow) * itemHeight),
  };
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
function isObject(value: unknown): boolean {
  return value !== null && typeof value === "object";
}

function mergePage<T extends AsyncEntityPickerItem>(
  current: readonly T[],
  incoming: readonly T[],
): T[] {
  const result = [...current];
  const ids = new Set(result.map((item) => item.id));
  for (const item of incoming) {
    if (
      !isObject(item) ||
      typeof item.id !== "string" ||
      !item.id ||
      ids.has(item.id) ||
      typeof item.label !== "string"
    )
      throw new Error("Invalid or duplicate catalog item");
    ids.add(item.id);
    result.push(item);
  }
  return result;
}

export function useAsyncEntityCollection<T extends AsyncEntityPickerItem>(
  loader: AsyncEntityLoader<T>,
  options: AsyncEntityCollectionOptions = {},
) {
  const query = ref("");
  const items = shallowRef<T[]>([]);
  const nextCursor = ref<string | null>(null);
  const initialLoading = ref(false);
  const loadingMore = ref(false);
  const error = shallowRef<unknown>();
  const hasLoaded = ref(false);
  const debounceMs = Math.max(0, options.debounceMs ?? 500);
  const cursors = new Set<string>();

  let generation = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let controller: AbortController | undefined;
  let disposed = false;

  const phase = computed<AsyncEntityPickerPhase>(() => {
    if (initialLoading.value) return "initial-loading";
    if (error.value !== undefined && items.value.length === 0) return "error";
    if (hasLoaded.value && items.value.length === 0) return "empty";
    return "ready";
  });
  const hasMore = computed(() => nextCursor.value !== null);
  const loadMoreError = computed(
    () => error.value !== undefined && items.value.length > 0,
  );

  function cancelPending(): void {
    if (timer) clearTimeout(timer);
    timer = undefined;
    controller?.abort();
    controller = undefined;
  }

  async function loadPage(append: boolean, expectedGeneration: number) {
    if (disposed || expectedGeneration !== generation) return;
    if (append) loadingMore.value = true;
    else initialLoading.value = true;
    error.value = undefined;
    const requestController = new AbortController();
    controller = requestController;

    try {
      const page = await loader({
        query: query.value,
        cursor: append ? (nextCursor.value ?? undefined) : undefined,
        signal: requestController.signal,
      });
      if (requestController.signal.aborted || expectedGeneration !== generation)
        return;
      if (!isObject(page) || !Array.isArray(page.items))
        throw new Error("Invalid catalog page");
      const cursor = page.nextCursor === "" ? null : (page.nextCursor ?? null);
      if (
        cursor !== null &&
        (typeof cursor !== "string" || cursors.has(cursor))
      )
        throw new Error("Repeated or invalid catalog cursor");
      const merged = mergePage<T>(
        append ? items.value : [],
        page.items as readonly T[],
      );
      if (cursor) cursors.add(cursor);
      items.value = merged;
      nextCursor.value = cursor;
      hasLoaded.value = true;
    } catch (loadError) {
      if (
        requestController.signal.aborted ||
        expectedGeneration !== generation ||
        isAbortError(loadError)
      )
        return;
      if (
        append &&
        loadError instanceof AppProblem &&
        loadError.status === 412
      ) {
        schedule(0);
        return;
      }
      error.value = loadError;
      hasLoaded.value = true;
    } finally {
      if (expectedGeneration === generation) {
        initialLoading.value = false;
        loadingMore.value = false;
      }
      if (controller === requestController) controller = undefined;
    }
  }

  function schedule(delay = debounceMs): void {
    cancelPending();
    generation += 1;
    cursors.clear();
    const expectedGeneration = generation;
    items.value = [];
    nextCursor.value = null;
    error.value = undefined;
    hasLoaded.value = false;
    initialLoading.value = true;
    timer = setTimeout(() => {
      timer = undefined;
      void loadPage(false, expectedGeneration);
    }, delay);
  }

  function refresh(): void {
    schedule(0);
  }

  function cancel(): void {
    generation += 1;
    cancelPending();
    initialLoading.value = false;
    loadingMore.value = false;
  }

  async function loadMore(): Promise<void> {
    if (
      disposed ||
      initialLoading.value ||
      loadingMore.value ||
      nextCursor.value === null
    )
      return;
    await loadPage(true, generation);
  }

  const stopQueryWatch = watch(query, () => schedule(), {
    flush: "sync",
    immediate: options.immediate ?? true,
  });

  function dispose(): void {
    disposed = true;
    generation += 1;
    stopQueryWatch();
    cancelPending();
  }

  onScopeDispose(dispose);

  return {
    error,
    hasMore,
    initialLoading,
    items,
    loadMore,
    loadMoreError,
    loadingMore,
    nextCursor,
    phase,
    query,
    refresh,
    cancel,
  };
}

export function nearScrollEnd(
  element: Pick<HTMLElement, "clientHeight" | "scrollHeight" | "scrollTop">,
  threshold = 96,
): boolean {
  return (
    element.scrollHeight - element.scrollTop - element.clientHeight <= threshold
  );
}

export function createCursorIntersectionHandler(
  enabled: () => boolean,
  loadMore: () => void | Promise<void>,
): IntersectionObserverCallback {
  return (entries) => {
    if (!enabled() || !entries.some((entry) => entry.isIntersecting)) return;
    void loadMore();
  };
}

export interface CursorInfiniteScrollOptions {
  root: Ref<HTMLElement | null | undefined>;
  sentinel: Ref<Element | null | undefined>;
  enabled: MaybeRefOrGetter<boolean>;
  loadMore: () => void | Promise<void>;
  rootMargin?: string;
}

export function useCursorInfiniteScroll(
  options: CursorInfiniteScrollOptions,
): void {
  let observer: IntersectionObserver | undefined;

  function disconnect(): void {
    observer?.disconnect();
    observer = undefined;
  }

  function reconnect(): void {
    disconnect();
    if (
      typeof IntersectionObserver === "undefined" ||
      !options.sentinel.value ||
      !toValue(options.enabled)
    )
      return;
    observer = new IntersectionObserver(
      createCursorIntersectionHandler(
        () => toValue(options.enabled),
        options.loadMore,
      ),
      {
        root: options.root.value ?? null,
        rootMargin: options.rootMargin ?? "0px 0px 120px",
      },
    );
    observer.observe(options.sentinel.value);
  }

  const stopWatch = watch(
    () => [
      options.root.value,
      options.sentinel.value,
      toValue(options.enabled),
    ],
    reconnect,
    { flush: "post" },
  );

  onScopeDispose(() => {
    stopWatch();
    disconnect();
  });
}

export interface AsyncEntityOption {
  ref: string;
  title: string;
  description?: string;
  meta?: string;
  disabled?: boolean;
  disabledReason?: string;
}

export interface AsyncEntityOptionPage {
  items: AsyncEntityOption[];
  nextPageToken?: string;
}
