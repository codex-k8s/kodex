import type { Page } from "@playwright/test";

// Наблюдает сигнал; исходные fetch input/init, Promise и результат не заменяются.
export async function installSyntheticAbortObserver(
  page: Page,
  report: (url: string) => void,
  origin = "https://kodex.test",
): Promise<void> {
  await page.exposeBinding(
    "__kodexSyntheticAbortedFetch",
    ({ frame }, address: unknown) => {
      if (frame !== page.mainFrame() || typeof address !== "string") return;
      const url = new URL(address);
      if (url.origin === origin && url.pathname.startsWith("/api/v1/"))
        report(url.href);
    },
  );
  await page.addInitScript((expectedOrigin) => {
    const fetch = window.fetch.bind(window);
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
      const onAbort = () => {
        if (!observedURL) return;
        const binding = (
          window as unknown as {
            __kodexSyntheticAbortedFetch: (url: string) => Promise<void>;
          }
        ).__kodexSyntheticAbortedFetch;
        void binding(observedURL).catch(() => undefined);
      };
      if (observedURL)
        signal?.addEventListener("abort", onAbort, { once: true });
      const result = fetch(input, init);
      // Удаление наблюдателя предотвращает отнесение позднего abort к новому запросу.
      const cleanup = () => signal?.removeEventListener("abort", onAbort);
      void result.then(cleanup, cleanup);
      return result;
    };
  }, origin);
}
