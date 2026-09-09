import type { Page } from "@playwright/test";

export const syntheticFetchIDHeader = "x-kodex-synthetic-fetch-id";

export interface SyntheticFetchEvent {
  phase: "start" | "abort" | "headers" | "body" | "body-error" | "reject";
  id: string;
  url: string;
  signalGeneration?: number;
  signalAborted?: boolean;
  reasonClass?: string;
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
        !["start", "abort", "headers", "body", "body-error", "reject"].includes(
          event.phase,
        ) ||
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
      const responseText = Object.getOwnPropertyDescriptor(
        Response.prototype,
        "text",
      )?.value as ((this: Response) => Promise<string>) | undefined;
      if (!responseText) throw new Error("Response.text fixture unavailable");
      const documentID = crypto.randomUUID();
      let sequence = 0;
      let signalSequence = 0;
      const signalGenerations = new WeakMap<AbortSignal, number>();
      const fetchDetails = new Map<
        string,
        {
          url: string;
          signal: AbortSignal | null | undefined;
          signalGeneration: number;
        }
      >();
      Response.prototype.text = async function () {
        const id = this.headers.get(headerName);
        const details = id ? fetchDetails.get(id) : undefined;
        let value: string;
        try {
          value = await responseText.call(this);
        } catch (error) {
          if (id) {
            const binding = (
              window as unknown as {
                __kodexSyntheticAbortedFetch: (
                  event: SyntheticFetchEvent,
                ) => Promise<void>;
              }
            ).__kodexSyntheticAbortedFetch;
            await binding({
              phase: "body-error",
              id,
              url: details?.url ?? this.url,
              signalGeneration: details?.signalGeneration ?? 0,
              signalAborted: details?.signal?.aborted ?? false,
              reasonClass: error instanceof Error ? error.name : "UNKNOWN",
            });
            fetchDetails.delete(id);
          }
          throw error;
        }
        if (id) {
          const url = details?.url ?? this.url;
          fetchDetails.delete(id);
          const binding = (
            window as unknown as {
              __kodexSyntheticAbortedFetch: (
                event: SyntheticFetchEvent,
              ) => Promise<void>;
            }
          ).__kodexSyntheticAbortedFetch;
          await binding({
            phase: "body",
            id,
            url,
            signalGeneration: details?.signalGeneration ?? 0,
            signalAborted: details?.signal?.aborted ?? false,
            reasonClass: "NONE",
          });
        }
        return value;
      };
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
        const signalGeneration = signal
          ? (signalGenerations.get(signal) ?? ++signalSequence)
          : 0;
        if (signal && !signalGenerations.has(signal))
          signalGenerations.set(signal, signalGeneration);
        const emit = (phase: SyntheticFetchEvent["phase"]) => {
          if (!observedURL) return;
          const binding = (
            window as unknown as {
              __kodexSyntheticAbortedFetch: (
                event: SyntheticFetchEvent,
              ) => Promise<void>;
            }
          ).__kodexSyntheticAbortedFetch;
          const reason: unknown = signal?.reason;
          void binding({
            phase,
            id,
            url: observedURL,
            signalGeneration,
            signalAborted: signal?.aborted ?? false,
            reasonClass:
              reason instanceof Error
                ? reason.name
                : signal?.aborted
                  ? "UNKNOWN"
                  : "NONE",
          }).catch(() => undefined);
        };
        if (observedURL) {
          fetchDetails.set(id, { url: observedURL, signal, signalGeneration });
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
          const operation = fetch(input, { ...init, headers });
          void operation.then(
            () => emit("headers"),
            () => {
              emit("reject");
              fetchDetails.delete(id);
            },
          );
          return operation;
        }
        return fetch(input, init);
      };
    },
    { expectedOrigin: origin, headerName: syntheticFetchIDHeader },
  );
}
