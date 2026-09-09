import type { Page, Response } from "@playwright/test";

const invalidMetadata = "Session boundary metadata is invalid";
const maximumDelta = 366 * 86_400_000;
const eventLimit = 128;

export function safeSessionBoundary(value: unknown) {
  if (!value || typeof value !== "object") throw new Error(invalidMetadata);
  const data = value as Record<string, unknown>;
  const timestamp = (key: string) => {
    if (typeof data[key] !== "string") throw new Error(invalidMetadata);
    const parsed = Date.parse(data[key]);
    if (!Number.isFinite(parsed)) throw new Error(invalidMetadata);
    return parsed;
  };
  const now = timestamp("serverTime");
  const delta = (key: string) => {
    const result = timestamp(key) - now;
    if (!Number.isSafeInteger(result) || Math.abs(result) > maximumDelta)
      throw new Error(invalidMetadata);
    return result;
  };
  if (
    !Number.isSafeInteger(data.version) ||
    Number(data.version) < 1 ||
    !["BACKEND_REFRESH", "REAUTHENTICATION"].includes(String(data.renewalMode))
  )
    throw new Error(invalidMetadata);
  return {
    version: Number(data.version),
    renewalMode: data.renewalMode as "BACKEND_REFRESH" | "REAUTHENTICATION",
    accessRemainingMs: delta("accessExpiresAt"),
    idleRemainingMs: delta("expiresAt"),
    absoluteRemainingMs: delta("absoluteExpiresAt"),
    renewInMs: delta("renewAfter"),
  };
}

export function sessionPreflight(status: number, body: unknown) {
  if (status !== 200) throw new Error("Session preflight was not accepted");
  const boundary = safeSessionBoundary(body);
  if (
    Math.min(boundary.idleRemainingMs, boundary.absoluteRemainingMs) <= 0 ||
    (boundary.renewalMode === "REAUTHENTICATION" &&
      boundary.accessRemainingMs <= 0)
  )
    throw new Error("Session preflight boundary has expired");
  return boundary;
}

type Boundary = ReturnType<typeof safeSessionBoundary>;
type Category =
  | "SESSION_READ"
  | "SESSION_RENEW"
  | "SESSION_TICKET"
  | "PREFLIGHT"
  | "UNKNOWN";
interface Event {
  sequence: number;
  timestampUTC: string;
  category: Category;
  status: number;
  metadata: "VALID" | "INVALID" | "NOT_APPLICABLE";
  boundary?: Boundary;
}

export class SessionBoundaryDiagnostics {
  private events: Event[] = [];
  private sequence = 0;
  private overflow = 0;
  private pending = new Set<Promise<void>>();
  private stopped = false;
  observe(
    category: Category,
    status: number,
    body?: unknown,
    received?: Pick<Event, "sequence" | "timestampUTC">,
  ): void {
    if (this.events.length >= eventLimit) {
      this.overflow++;
      return;
    }
    let boundary: Boundary | undefined;
    let metadata: Event["metadata"] = "NOT_APPLICABLE";
    if (
      ["PREFLIGHT", "SESSION_READ", "SESSION_RENEW"].includes(category) &&
      status === 200
    ) {
      try {
        boundary = safeSessionBoundary(body);
        metadata = "VALID";
      } catch {
        metadata = "INVALID";
      }
    }
    this.events.push({
      sequence: received?.sequence ?? ++this.sequence,
      timestampUTC: received?.timestampUTC ?? new Date().toISOString(),
      category,
      status:
        Number.isSafeInteger(status) && status >= 100 && status <= 599
          ? status
          : 0,
      metadata,
      ...(boundary ? { boundary } : {}),
    });
  }
  snapshot() {
    return {
      eventLimit,
      overflow: this.overflow,
      events: [...this.events]
        .sort((a, b) => a.sequence - b.sequence)
        .map((event) => ({
          ...event,
          ...(event.boundary ? { boundary: { ...event.boundary } } : {}),
        })),
    };
  }
  install(page: Page, origin: string): () => Promise<void> {
    const listener = (response: Response) => {
      if (this.stopped) return;
      const url = new URL(response.url());
      if (url.origin !== origin) return;
      const method = response.request().method();
      const category =
        url.pathname === "/api/v1/session" && method === "GET"
          ? "SESSION_READ"
          : url.pathname === "/api/v1/session" && method === "PUT"
            ? "SESSION_RENEW"
            : url.pathname === "/api/v1/session/ticket" && method === "POST"
              ? "SESSION_TICKET"
              : undefined;
      if (!category) return;
      if (this.events.length + this.pending.size >= eventLimit) {
        this.overflow++;
        return;
      }
      const received = {
        sequence: ++this.sequence,
        timestampUTC: new Date().toISOString(),
      };
      const task = (async () => {
        // Только session metadata; ticket, headers, cookie и произвольные errors не читаются.
        const body =
          category !== "SESSION_TICKET" && response.status() === 200
            ? await response
                .text()
                .then((text) => {
                  if (text.length > 16_384) return undefined;
                  try {
                    return JSON.parse(text) as unknown;
                  } catch {
                    return undefined;
                  }
                })
                .catch(() => undefined)
            : undefined;
        this.observe(category, response.status(), body, received);
      })();
      this.pending.add(task);
      void task.finally(() => this.pending.delete(task));
    };
    page.on("response", listener);
    return async () => {
      this.stopped = true;
      page.off("response", listener);
      await Promise.all(this.pending);
    };
  }
}
