import type { Page } from "@playwright/test";

export const syntheticFetchIDHeader = "x-kodex-synthetic-fetch-id";

export interface SyntheticFetchEvent {
  phase: "start" | "abort";
  id: string;
  url: string;
}

// Fixture-only header связывает сигнал с request; method/body/signal/credentials сохраняются.
export async function installSyntheticAbortObserver(
  page: Page,
  report: (event: SyntheticFetchEvent) => void,
  origin = "https://kodex.test",
): Promise<void> {
  const fixtureOrigin = new URL(origin);
  if (
    fixtureOrigin.origin !== origin ||
    (origin !== "https://kodex.test" &&
      !(
        fixtureOrigin.protocol === "http:" &&
        fixtureOrigin.hostname === "127.0.0.1" &&
        fixtureOrigin.port
      ))
  )
    throw new Error("Synthetic fixture origin required");
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
  await page.addInitScript(
    ({ expectedOrigin, headerName }) => {
      const fetch = window.fetch.bind(window);
      const documentID = crypto.randomUUID();
      let sequence = 0;
      window.fetch = (input, init) => {
        const signal =
          init?.signal === null
            ? null
            : (init?.signal ??
              (input instanceof Request ? input.signal : undefined));
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
          location.origin === expectedOrigin &&
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
          const headers = new Headers(
            init?.headers ??
              (input instanceof Request ? input.headers : undefined),
          );
          if (headers.has(headerName))
            throw new Error("Synthetic fetch identity header is reserved");
          headers.set(headerName, id);
          emit("start");
          // Fetch завершается на headers; signal остаётся значимым до конца body.
          // Listener живёт в пределах изолированной страницы, identity не переиспользуется.
          if (signal?.aborted) emit("abort");
          else
            signal?.addEventListener("abort", () => emit("abort"), {
              once: true,
            });
          return fetch(input, { ...init, headers });
        }
        return fetch(input, init);
      };
    },
    { expectedOrigin: origin, headerName: syntheticFetchIDHeader },
  );
}
