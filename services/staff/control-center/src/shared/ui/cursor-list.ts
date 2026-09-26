import {
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  toValue,
  watch,
  type MaybeRefOrGetter,
  type Ref,
} from "vue";

export interface AdaptiveCursorPageSizeOptions {
  container: Ref<HTMLElement | null | undefined>;
  itemSelector: string;
  itemCount: MaybeRefOrGetter<number>;
  estimatedViewportHeight?: number;
  estimatedItemHeight: number;
  estimatedColumns?: number;
  minimum?: number;
  maximum?: number;
  overscan?: number;
}

export function adaptiveCursorPageSize(
  viewportHeight: number,
  itemHeight: number,
  columns = 1,
  minimum = 8,
  maximum = 100,
  overscan = 1.25,
): number {
  const safeHeight = Math.max(1, itemHeight);
  const visible = Math.ceil(Math.max(1, viewportHeight) / safeHeight);
  return Math.min(
    maximum,
    Math.max(minimum, Math.ceil(visible * Math.max(1, columns) * overscan)),
  );
}

export function useAdaptiveCursorPageSize(
  options: AdaptiveCursorPageSizeOptions,
): Ref<number> {
  const pageSize = ref(
    adaptiveCursorPageSize(
      options.estimatedViewportHeight ??
        (typeof window === "undefined" ? 720 : window.innerHeight),
      options.estimatedItemHeight,
      options.estimatedColumns,
      options.minimum,
      options.maximum,
      options.overscan,
    ),
  );
  let observer: ResizeObserver | undefined;

  function measure(): void {
    const container = options.container.value;
    if (!container || typeof window === "undefined") return;
    const bounds = container.getBoundingClientRect();
    const viewportHeight = Math.max(
      160,
      Math.min(
        container.clientHeight || window.innerHeight,
        window.innerHeight - Math.max(0, bounds.top) - 24,
      ),
    );
    const elements = [
      ...container.querySelectorAll<HTMLElement>(options.itemSelector),
    ]
      .map((element) => ({ element, bounds: element.getBoundingClientRect() }))
      .filter(({ bounds: item }) => item.width > 0 && item.height > 0)
      .slice(0, 20);
    const first = elements[0]?.bounds;
    const itemHeight = first?.height ?? options.estimatedItemHeight;
    const columns = first
      ? Math.max(
          1,
          elements.filter(
            ({ bounds: item }) => Math.abs(item.top - first.top) < 2,
          ).length,
        )
      : (options.estimatedColumns ?? 1);
    pageSize.value = adaptiveCursorPageSize(
      viewportHeight,
      itemHeight,
      columns,
      options.minimum,
      options.maximum,
      options.overscan,
    );
  }

  function scheduleMeasure(): void {
    void nextTick(measure);
  }

  onMounted(() => {
    scheduleMeasure();
    if (typeof window !== "undefined")
      window.addEventListener("resize", scheduleMeasure, { passive: true });
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(scheduleMeasure);
      if (options.container.value) observer.observe(options.container.value);
    }
  });
  const stopContainerWatch = watch(
    () => options.container.value,
    (container, previousContainer) => {
      if (observer && container !== previousContainer) {
        observer.disconnect();
        if (container) observer.observe(container);
      }
      scheduleMeasure();
    },
    { flush: "post" },
  );
  const stopItemWatch = watch(
    () => toValue(options.itemCount),
    scheduleMeasure,
    { flush: "post" },
  );
  onBeforeUnmount(() => {
    stopContainerWatch();
    stopItemWatch();
    observer?.disconnect();
    if (typeof window !== "undefined")
      window.removeEventListener("resize", scheduleMeasure);
  });
  return pageSize;
}
