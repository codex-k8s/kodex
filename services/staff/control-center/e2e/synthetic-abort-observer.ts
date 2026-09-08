import type { Page } from "@playwright/test";

export interface SyntheticFetchEvent {
  phase: "start" | "abort";
  id: string;
  url: string;
}

// Наблюдает сигнал; исходные fetch input/init, Promise и результат не заменяются.
export async function installSyntheticAbortObserver(
  page: Page,
  report: (event: SyntheticFetchEvent) => void,
  origin = "https://kodex.test",
): Promise<void> {
  await page.exposeBinding(
    "__kodexSyntheticAbortedFetch",
    ({ frame }, event: SyntheticFetchEvent | null) => {
      if (
        frame !== page.mainFrame() ||
        !event ||
        !["start", "abort"].includes(event.phase) ||
        typeof event.id !== "string" ||
        typeof event.url !== "string"
      )
        return;
      const url = new URL(event.url);
      if (url.origin === origin && url.pathname.startsWith("/api/v1/"))
        report({ ...event, url: url.href });
    },
  );
  await page.addInitScript((expectedOrigin) => {
    const fetch = window.fetch.bind(window);
    const documentID = crypto.randomUUID();
    let sequence = 0;
    window.fetch = (input, init) => {
      const signal =
        init?.signal ?? (input instanceof Request ? input.signal : undefined);
      let address: URL | undefined;
      try {
        address = new URL(
          input instanceof Request ? input.url : String(input),
          location.href,
        );
      } catch {
        // Ошибку исходного input по-прежнему возвращает настоящий fetch.
      }
      const observedURL =
        address?.origin === expectedOrigin &&
        address.pathname.startsWith("/api/v1/")
          ? address.href
          : undefined;
      const id = `${documentID}:${String(++sequence)}`;
      const emit = (phase: "start" | "abort") => {
        if (!observedURL) return;
        const binding = (
          window as unknown as {
            __kodexSyntheticAbortedFetch: (
              event: SyntheticFetchEvent,
            ) => Promise<void>;
          }
        ).__kodexSyntheticAbortedFetch;
        void binding({ phase, id, url: observedURL }).catch(() => undefined);
      };
      if (observedURL) {
        emit("start");
        // Fetch завершается на headers; signal остаётся значимым до конца body.
        // Listener живёт в пределах изолированной страницы, identity не переиспользуется.
        if (signal?.aborted) emit("abort");
        else
          signal?.addEventListener("abort", () => emit("abort"), {
            once: true,
          });
      }
      return fetch(input, init);
    };
  }, origin);
}
