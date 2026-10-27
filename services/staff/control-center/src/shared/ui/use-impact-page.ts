import { onBeforeUnmount, watch, type Ref } from "vue";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { appendImpactPage, type ImpactPageSlice } from "./impact-page";

interface ImpactPageLoaderOptions<Page extends ImpactPageSlice> {
  page: Ref<Page | undefined>;
  query: Ref<string>;
  loading: Ref<boolean>;
  problem: Ref<AppProblem | undefined>;
  busy: () => boolean | undefined;
  identity: () => readonly unknown[];
  read: (signal: AbortSignal, token?: string) => Promise<Page>;
  clearSelection: () => void;
  afterLoad?: (next: Page) => void;
}

// Владеет только локальным циклом чтения уже проверяемых feature-адаптером
// страниц: отменой, поколением ответа, поиском400ms и защитой пагинации.
// Вызывается синхронно в setup после подготовки всех зависимостей: первый
// identity watch сразу запускает read. Выбор строк и scope/plan eligibility
// остаются у caller; afterLoad получает только новую страницу после append.
export function useImpactPageLoader<Page extends ImpactPageSlice>(
  options: ImpactPageLoaderOptions<Page>,
) {
  const { page, query, loading, problem } = options;
  let controller: AbortController | undefined;
  let generation = 0;
  let debounce: ReturnType<typeof setTimeout> | undefined;
  const cursors = new Set<string>();

  async function load(more = false): Promise<void> {
    if (
      options.busy() ||
      (more && (loading.value || !page.value?.nextPageToken))
    )
      return;
    clearTimeout(debounce);
    const previous = page.value;
    const current = ++generation;
    controller?.abort();
    const active = new AbortController();
    controller = active;
    loading.value = true;
    problem.value = undefined;
    if (!more) {
      page.value = undefined;
      options.clearSelection();
      cursors.clear();
    }
    try {
      const next = await options.read(
        active.signal,
        more ? previous?.nextPageToken : undefined,
      );
      if (current !== generation) return;
      if (more && previous) {
        page.value = appendImpactPage(previous, next, cursors);
      } else page.value = next;
      options.afterLoad?.(next);
    } catch (error) {
      if (current === generation && !active.signal.aborted)
        problem.value = asProblem(error);
    } finally {
      if (current === generation) loading.value = false;
    }
  }

  watch(
    options.identity,
    () => {
      void load();
    },
    { immediate: true },
  );
  watch(query, () => {
    controller?.abort();
    generation++;
    clearTimeout(debounce);
    page.value = undefined;
    options.clearSelection();
    loading.value = true;
    debounce = setTimeout(() => {
      void load();
    }, 400);
  });
  watch(options.busy, (busy, previous) => {
    if (!busy && previous) void load();
  });
  onBeforeUnmount(() => {
    controller?.abort();
    generation++;
    clearTimeout(debounce);
  });
  return { load };
}
