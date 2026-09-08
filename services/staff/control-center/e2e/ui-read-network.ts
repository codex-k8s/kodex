import {
  DocumentNavigation,
  type NavigationAction,
} from "./ui-document-navigation";
import {
  SessionRequestDiagnostics,
  type SessionProofStage,
} from "./session-request-diagnostics";
import type { Page, Request } from "@playwright/test";

export const uiReadIdentityHeader = "x-kodex-e2e-read-id";
export interface ReadSignalEvent {
  phase: "start" | "abort" | "reject";
  id: string;
  address: string;
  method: string;
  intentional: boolean;
  at: number;
}
const identity =
  /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}:[1-9][0-9]{0,6}$/;
const cancelledCodes = new Set([
  "net::ERR_ABORTED",
  "NS_BINDING_ABORTED",
  "Load request cancelled",
  "cancelled",
]);
// Private runtime correlation; адреса, identity и тексты ошибок в evidence не выходят.
export class ReadNetworkCorrelator<T extends object> {
  private readonly diagnostics: SessionRequestDiagnostics<T>;
  private readonly documents = new DocumentNavigation<T>();
  private stage: SessionProofStage = "INITIAL_READY";
  constructor(origin = "https://fixture.invalid") {
    this.diagnostics = new SessionRequestDiagnostics<T>(origin);
  }
  setStage(stage: SessionProofStage): void {
    this.stage = stage;
  }
  private readonly signals = new Map<
    string,
    {
      address: string;
      method: string;
      start?: number;
      abort?: number;
      rejected: boolean;
      invalid: boolean;
    }
  >();
  private readonly requests = new Map<
    T,
    {
      id?: string;
      address: string;
      method: string;
      sequence: number;
      terminal?: number;
      failed?: number;
      code?: string;
    }
  >();
  private readonly ids = new Map<string, Set<T>>();
  private events = 0;
  private overflow = 0;
  observe(event: ReadSignalEvent): void {
    if (++this.events > 4096) {
      this.overflow++;
      return;
    }
    if (
      !identity.test(event.id) ||
      !Number.isSafeInteger(event.at) ||
      event.at < 1
    )
      return;
    const item = this.signals.get(event.id) ?? {
      address: event.address,
      method: event.method,
      rejected: false,
      invalid: false,
    };
    if (
      item.address !== event.address ||
      item.method !== event.method ||
      (event.phase === "start" && item.start !== undefined)
    )
      item.invalid = true;
    if (event.phase === "start") item.start = event.at;
    if (event.phase === "abort" && event.intentional) item.abort ??= event.at;
    if (event.phase === "reject" && event.intentional) item.rejected = true;
    this.signals.set(event.id, item);
  }
  request(
    request: T,
    address: string,
    method: string,
    id?: string,
    resourceType = "other",
    mainFrame = true,
  ): void {
    if (this.requests.has(request)) return;
    if (this.requests.size >= 4096) {
      this.overflow++;
      return;
    }
    this.documents.request(request, mainFrame);
    this.diagnostics.start(request, {
      url: address,
      method,
      resourceType,
      stage: this.stage,
      tab: 0,
    });
    this.requests.set(request, {
      id,
      address,
      method,
      sequence: this.requests.size + 1,
    });
    if (id && identity.test(id)) {
      const requests = this.ids.get(id) ?? new Set<T>();
      requests.add(request);
      this.ids.set(id, requests);
    }
  }
  terminal(request: T, at = Date.now()): void {
    const item = this.requests.get(request);
    if (item) item.terminal = at;
    this.documents.terminal(request, at);
  }
  failed(request: T, code: string, at = Date.now()): void {
    const item = this.requests.get(request);
    if (item) {
      this.documents.terminal(request, at);
      this.diagnostics.failed(request, this.stage, code, at);
      item.failed = at;
      item.terminal = at;
      item.code = code;
    }
  }
  documentObserved(id: string, at = Date.now()): void {
    this.documents.document(id, at);
  }
  navigation(action: NavigationAction = "GOTO", at = Date.now()): number {
    return this.documents.begin(action, at);
  }
  navigationFinished(
    id: number,
    complete: boolean,
    at = Date.now(),
    sameDocument = false,
  ): void {
    this.documents.end(id, complete, at, sameDocument);
  }
  confirmed(request: T): boolean {
    const item = this.requests.get(request);
    if (
      !item ||
      item.failed === undefined ||
      !cancelledCodes.has(item.code ?? "") ||
      this.overflow ||
      this.documents.overflowCount() ||
      (item.id !== undefined && this.ids.get(item.id)?.size !== 1)
    )
      return false;
    if (this.documents.confirmed(request)) return true;
    const signal = item.id ? this.signals.get(item.id) : undefined;
    return (
      !!signal &&
      !signal.invalid &&
      signal.start !== undefined &&
      signal.start <= item.failed &&
      signal.address === item.address &&
      signal.method === item.method &&
      signal.abort !== undefined &&
      signal.abort <= item.failed &&
      signal.rejected
    );
  }
  safeDiagnostics() {
    const snapshot = this.diagnostics.snapshot();
    return {
      ...snapshot,
      failures: snapshot.failures.map((failure) => {
        const entry = [...this.requests.entries()].find(
          ([, item]) => item.sequence === failure.requestSequence,
        );
        const signal = entry?.[1].id
          ? this.signals.get(entry[1].id)
          : undefined;
        return {
          ...failure,
          exactCancellation: entry ? this.confirmed(entry[0]) : false,
          nativeIdentityKnown: !!signal && !signal.invalid,
          nativeAbortObserved: signal?.abort !== undefined,
          nativeRejected: signal?.rejected ?? false,
          navigationIntentObserved: entry
            ? this.documents.evidence(entry[0]).documentNavigationIntent
            : false,
          ...(entry ? this.documents.evidence(entry[0]) : {}),
        };
      }),
    };
  }
  snapshot() {
    const failed = [...this.requests.entries()].filter(
      ([, item]) => item.failed !== undefined,
    );
    return {
      observedRequests: this.requests.size,
      signalEvents: Math.min(this.events, 4096),
      overflow: this.overflow + this.documents.overflowCount(),
      rawFailedRequests: failed.length,
      confirmedCancellations: failed.filter(([request]) =>
        this.confirmed(request),
      ).length,
      unexplainedFailures: failed.filter(
        ([request]) => !this.confirmed(request),
      ).length,
    };
  }
}
export async function installReadNetworkObserver(
  page: Page,
  origin: string,
): Promise<ReadNetworkCorrelator<Request>> {
  const parsed = new URL(origin);
  if (
    parsed.origin !== origin ||
    !(
      parsed.protocol === "https:" ||
      (parsed.protocol === "http:" && parsed.hostname === "127.0.0.1")
    )
  )
    throw new Error("Exact UI diagnostic origin required");
  const observer = new ReadNetworkCorrelator<Request>(origin);
  await page.exposeBinding(
    "__kodexUIReadSignal",
    ({ frame }, raw: ReadSignalEvent | null) => {
      if (
        frame !== page.mainFrame() ||
        !raw ||
        !["start", "abort", "reject"].includes(raw.phase) ||
        typeof raw.id !== "string" ||
        typeof raw.address !== "string" ||
        typeof raw.method !== "string" ||
        typeof raw.intentional !== "boolean"
      )
        return;
      try {
        if (new URL(raw.address).origin !== origin) return;
      } catch {
        return;
      }
      observer.observe({
        phase: raw.phase,
        id: raw.id,
        address: raw.address,
        method: raw.method,
        intentional: raw.intentional,
        at: raw.at,
      });
    },
  );
  await page.exposeBinding(
    "__kodexUIReadDocument",
    (
      { frame },
      value: { id?: unknown; at?: unknown; origin?: unknown } | null,
    ) => {
      if (
        frame !== page.mainFrame() ||
        !value ||
        value.origin !== origin ||
        typeof value.id !== "string" ||
        !Number.isSafeInteger(value.at)
      )
        return;
      observer.documentObserved(value.id, Number(value.at));
    },
  );
  await page.addInitScript(
    ({ origin, header }) => {
      const original = window.fetch.bind(window),
        documentID = crypto.randomUUID();
      if (location.origin === origin)
        void (
          window as unknown as {
            __kodexUIReadDocument(value: {
              id: string;
              at: number;
              origin: string;
            }): Promise<void>;
          }
        )
          .__kodexUIReadDocument({
            id: documentID,
            at: Date.now(),
            origin: location.origin,
          })
          .catch(() => undefined);
      let sequence = 0;
      window.fetch = (input, init) => {
        let address: URL;
        try {
          address = new URL(
            input instanceof Request ? input.url : String(input),
            location.href,
          );
        } catch {
          return original(input, init);
        }
        const method = (
          init?.method ?? (input instanceof Request ? input.method : "GET")
        ).toUpperCase();
        const read =
          ["GET", "HEAD"].includes(method) ||
          (method === "POST" &&
            address.pathname ===
              "/api/v1/administration/access/effective-access/query");
        if (
          location.origin !== origin ||
          address.origin !== origin ||
          !address.pathname.startsWith("/api/v1/") ||
          !read
        )
          return original(input, init);
        const headers = new Headers(
          init?.headers ??
            (input instanceof Request ? input.headers : undefined),
        );
        if (headers.has(header))
          throw new Error("Readonly diagnostic identity is reserved");
        const id = documentID + ":" + String(++sequence);
        headers.set(header, id);
        const signal =
          init?.signal === null
            ? null
            : (init?.signal ??
              (input instanceof Request ? input.signal : undefined));
        const emit = (
          phase: "start" | "abort" | "reject",
          rejection = false,
        ) => {
          const reason: unknown = signal?.reason;
          void (
            window as unknown as {
              __kodexUIReadSignal(event: ReadSignalEvent): Promise<void>;
            }
          )
            .__kodexUIReadSignal({
              phase,
              id,
              address: address.href,
              method,
              intentional:
                phase === "reject"
                  ? rejection
                  : reason instanceof DOMException &&
                    reason.name === "AbortError",
              at: Date.now(),
            })
            .catch(() => undefined);
        };
        emit("start");
        if (signal?.aborted) emit("abort");
        else
          signal?.addEventListener("abort", () => emit("abort"), {
            once: true,
          });
        const pending = original(input, { ...init, headers });
        void pending.catch((error: unknown) =>
          emit(
            "reject",
            error instanceof DOMException && error.name === "AbortError",
          ),
        );
        return pending;
      };
    },
    { origin, header: uiReadIdentityHeader },
  );
  page.on("request", (request) => {
    let mainFrame = false;
    try {
      mainFrame =
        !request.isNavigationRequest() && request.frame() === page.mainFrame();
    } catch {
      /* Service worker/frame уже недоступен: без подтверждения. */
    }
    if (new URL(request.url()).origin === origin)
      observer.request(
        request,
        request.url(),
        request.method(),
        request.headers()[uiReadIdentityHeader],
        request.resourceType(),
        mainFrame,
      );
  });
  page.on("requestfinished", (request) => observer.terminal(request));
  page.on("requestfailed", (request) =>
    observer.failed(request, request.failure()?.errorText ?? "UNKNOWN"),
  );
  const goto = page.goto.bind(page),
    reload = page.reload.bind(page),
    close = page.close.bind(page);
  page.goto = async (...args: Parameters<typeof goto>) => {
    const id = observer.navigation("GOTO");
    try {
      const value = await goto(...args);
      observer.navigationFinished(
        id,
        value !== null,
        Date.now(),
        value === null,
      );
      return value;
    } catch (error) {
      observer.navigationFinished(id, false);
      throw error;
    }
  };
  page.reload = async (...args: Parameters<typeof reload>) => {
    const id = observer.navigation("RELOAD");
    try {
      const value = await reload(...args);
      observer.navigationFinished(
        id,
        value !== null,
        Date.now(),
        value === null,
      );
      return value;
    } catch (error) {
      observer.navigationFinished(id, false);
      throw error;
    }
  };
  page.close = async (...args: Parameters<typeof close>) => {
    if (page.isClosed()) return close(...args);
    const id = observer.navigation("CLOSE");
    try {
      await close(...args);
      observer.navigationFinished(id, page.isClosed());
    } catch (error) {
      observer.navigationFinished(id, false);
      throw error;
    }
  };
  return observer;
}
