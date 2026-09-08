import type { Page } from "@playwright/test";
import { SyntheticFetchCorrelator } from "./synthetic-fetch-correlator";

export const sessionBootstrapIDHeader = "x-kodex-e2e-fetch-id";
export type BootstrapSignalEvent = {
  phase: "start" | "abort" | "reject";
  id: string;
  intentional: boolean;
  observedAt: number;
};

export class SessionBootstrapCorrelator<T extends object> {
  private readonly correlator = new SyntheticFetchCorrelator<T>();
  private events = 0;
  private overflow = 0;
  private readonly identities = new WeakMap<T, string>();
  private readonly failedAt = new WeakMap<T, number>();
  private readonly abortedAt = new Map<string, number>();
  private readonly rejected = new Set<string>();
  constructor(private readonly address: string) {}
  observe(event: BootstrapSignalEvent): void {
    if (++this.events > 4096) {
      this.overflow++;
      return;
    }
    if (!Number.isSafeInteger(event.observedAt) || event.observedAt < 1) return;
    if (
      event.phase === "abort" &&
      event.intentional &&
      !this.abortedAt.has(event.id)
    )
      this.abortedAt.set(event.id, event.observedAt);
    if (event.phase === "reject") {
      if (event.intentional) this.rejected.add(event.id);
      return;
    }
    if (event.phase === "start" || event.intentional)
      this.correlator.observe({
        phase: event.phase,
        id: event.id,
        url: this.address,
      });
  }
  request(
    request: T,
    address: string,
    method: string,
    identity?: string,
  ): void {
    if (address === this.address && method === "GET") {
      this.correlator.request(request, address, identity);
      if (identity) this.identities.set(request, identity);
    }
  }
  failed(request: T, at = Date.now()): void {
    this.failedAt.set(request, at);
  }
  cancelled(request: T): boolean {
    const identity = this.identities.get(request),
      aborted = identity ? this.abortedAt.get(identity) : undefined,
      failed = this.failedAt.get(request);
    return (
      this.overflow === 0 &&
      identity !== undefined &&
      this.rejected.has(identity) &&
      aborted !== undefined &&
      failed !== undefined &&
      aborted <= failed &&
      this.correlator.cancelled(request)
    );
  }
  snapshot() {
    return { events: Math.min(this.events, 4096), overflow: this.overflow };
  }
}

// Только диагностический opt-in N: неавторитетная identity одного bootstrap GET.
// Заголовки авторизации, signal, credentials, body и URL сохраняются.
export async function installSessionBootstrapObserver(
  page: Page,
  origin: string,
  report: (event: BootstrapSignalEvent) => void,
): Promise<void> {
  if (new URL(origin).origin !== origin)
    throw new Error("Exact bootstrap origin required");
  await page.exposeBinding(
    "__kodexSessionBootstrapSignal",
    ({ frame }, event: BootstrapSignalEvent | null) => {
      if (
        frame !== page.mainFrame() ||
        !event ||
        !["start", "abort", "reject"].includes(event.phase) ||
        typeof event.id !== "string" ||
        typeof event.intentional !== "boolean"
      )
        return;
      report({
        phase: event.phase,
        id: event.id,
        intentional: event.intentional,
        observedAt: event.observedAt,
      });
    },
  );
  await page.addInitScript(
    ({ origin, header }) => {
      const originalFetch = window.fetch.bind(window),
        documentID = crypto.randomUUID();
      let sequence = 0;
      window.fetch = (input, init) => {
        let address: URL;
        try {
          address = new URL(
            input instanceof Request ? input.url : String(input),
            location.href,
          );
        } catch {
          return originalFetch(input, init);
        }
        const method = (
          init?.method ?? (input instanceof Request ? input.method : "GET")
        ).toUpperCase();
        if (
          location.origin !== origin ||
          address.href !== `${origin}/api/v1/bootstrap` ||
          method !== "GET"
        )
          return originalFetch(input, init);
        const headers = new Headers(
          init?.headers ??
            (input instanceof Request ? input.headers : undefined),
        );
        if (headers.has(header))
          throw new Error("Bootstrap diagnostic identity is reserved");
        const id = `${documentID}:${String(++sequence)}`;
        headers.set(header, id);
        const signal =
          init?.signal === null
            ? null
            : (init?.signal ??
              (input instanceof Request ? input.signal : undefined));
        const emit = (
          phase: "start" | "abort" | "reject",
          rejectedAsAbort = false,
        ) => {
          const reason: unknown = signal?.reason;
          const intentional =
            phase === "reject"
              ? rejectedAsAbort
              : reason instanceof DOMException && reason.name === "AbortError";
          void (
            window as unknown as {
              __kodexSessionBootstrapSignal(
                event: BootstrapSignalEvent,
              ): Promise<void>;
            }
          )
            .__kodexSessionBootstrapSignal({
              phase,
              id,
              intentional,
              observedAt: Date.now(),
            })
            .catch(() => undefined);
        };
        emit("start");
        if (signal?.aborted) emit("abort");
        else
          signal?.addEventListener("abort", () => emit("abort"), {
            once: true,
          });
        const pending = originalFetch(input, { ...init, headers });
        void pending.catch((error: unknown) =>
          emit(
            "reject",
            error instanceof DOMException && error.name === "AbortError",
          ),
        );
        return pending;
      };
    },
    { origin, header: sessionBootstrapIDHeader },
  );
}
