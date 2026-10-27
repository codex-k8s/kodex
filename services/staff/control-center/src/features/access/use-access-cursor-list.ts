import type { Ref } from "vue";

import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";

// AccessCursorListOptions задаёт предметные границы и действие конкретного списка.
interface AccessCursorListOptions {
  listRoot: Ref<HTMLElement | undefined>;
  sentinel: Ref<HTMLElement | undefined>;
  itemSelector: string;
  itemCount: () => number;
  estimatedItemHeight: number;
  enabled: () => boolean;
  loadMore: (pageSize: number) => void;
}

// useAccessCursorList связывает измерение страницы и наблюдение за её концом.
// Параметры, допустимость загрузки и серверный запрос принадлежат компоненту.
export function useAccessCursorList(options: AccessCursorListOptions) {
  const pageSize = useAdaptiveCursorPageSize({
    container: options.listRoot,
    itemSelector: options.itemSelector,
    itemCount: options.itemCount,
    estimatedItemHeight: options.estimatedItemHeight,
  });
  useCursorInfiniteScroll({
    root: options.listRoot,
    sentinel: options.sentinel,
    enabled: options.enabled,
    loadMore: () => options.loadMore(pageSize.value),
  });
  return pageSize;
}
