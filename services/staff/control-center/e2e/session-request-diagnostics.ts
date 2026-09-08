import { createHash } from "node:crypto";

export type SessionProofStage =
  | "PREFLIGHT"
  | "INITIAL_READY"
  | "NATURAL_RENEWAL"
  | "READBACK"
  | "COMPLETE";
type Input = {
  url: string;
  method: string;
  resourceType: string;
  stage: SessionProofStage;
  tab: number;
};
const methods = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"];
const resources = [
  "document",
  "stylesheet",
  "image",
  "media",
  "font",
  "script",
  "texttrack",
  "xhr",
  "fetch",
  "eventsource",
  "websocket",
  "manifest",
  "other",
];
const errors = new Map([
  ["net::ERR_ABORTED", "CHROMIUM_ABORTED"],
  ["NS_BINDING_ABORTED", "FIREFOX_ABORTED"],
  ["Load request cancelled", "WEBKIT_CANCELLED"],
  ["net::ERR_FAILED", "NETWORK_FAILED"],
  ["net::ERR_CONNECTION_RESET", "CONNECTION_RESET"],
  ["net::ERR_CONNECTION_CLOSED", "CONNECTION_CLOSED"],
  ["net::ERR_BLOCKED_BY_CLIENT", "BLOCKED_BY_CLIENT"],
]);
const routes: Record<string, string> = {
  "/api/v1/session": "SESSION",
  "/api/v1/session/ticket": "SESSION_TICKET",
  "/api/v1/bootstrap": "BOOTSTRAP",
  "/api/v1/projects": "PROJECT_CATALOG",
  "/api/v1/assistant-conversations": "ASSISTANT_HISTORY",
  "/api/v1/system-assistant": "SYSTEM_ASSISTANT",
};
function routeCategory(address: string, origin: string): string {
  try {
    const url = new URL(address);
    if (url.origin !== origin) return "FOREIGN_ORIGIN";
    const known = routes[url.pathname];
    if (known) return known;
    if (url.pathname.startsWith("/api/v1/")) return "OTHER_API";
    if (url.pathname.startsWith("/assets/")) return "APPLICATION_ASSET";
    if (url.pathname.startsWith("/fonts/")) return "FONT_ASSET";
    if (url.pathname === "/config/runtime-config.json") return "RUNTIME_CONFIG";
    return "APPLICATION_ROUTE";
  } catch {
    return "INVALID_ADDRESS";
  }
}
type Started = {
  requestSequence: number;
  route: string;
  method: string;
  resourceType: string;
  startedStage: SessionProofStage;
  tab: number;
  startedAt: number;
};
type Failed = Omit<Started, "startedAt"> & {
  identityKnown: boolean;
  failedStage: SessionProofStage;
  elapsedMs: number;
  code: string;
  errorSHA256: string;
};

// WeakMap связывает два события именно одного Playwright Request, без изменения wire.
// Даже подтверждённая отмена здесь остаётся failure: helper ничего не подавляет.
export class SessionRequestDiagnostics<T extends object> {
  private sequence = 0;
  private readonly requests = new WeakMap<T, Started>();
  private readonly failures: Failed[] = [];
  private overflow = 0;
  constructor(private readonly origin: string) {}
  start(request: T, input: Input, now = Date.now()): void {
    if (this.requests.has(request)) return;
    this.requests.set(request, {
      requestSequence: ++this.sequence,
      route: routeCategory(input.url, this.origin),
      method: methods.includes(input.method) ? input.method : "OTHER",
      resourceType: resources.includes(input.resourceType)
        ? input.resourceType
        : "other",
      startedStage: input.stage,
      tab: input.tab === 0 || input.tab === 1 ? input.tab : -1,
      startedAt: now,
    });
  }
  failed(
    request: T,
    stage: SessionProofStage,
    error: string,
    now = Date.now(),
  ): void {
    if (this.failures.length >= 32) {
      this.overflow++;
      return;
    }
    const selected = this.requests.get(request);
    const { startedAt, ...safe } = selected ?? {
      requestSequence: 0,
      route: "UNOBSERVED",
      method: "OTHER",
      resourceType: "other",
      startedStage: stage,
      tab: -1,
      startedAt: now,
    };
    this.failures.push({
      ...safe,
      identityKnown: !!selected,
      failedStage: stage,
      elapsedMs: Math.max(0, Math.round(now - startedAt)),
      code: errors.get(error) ?? "UNKNOWN",
      errorSHA256: createHash("sha256").update(error).digest("hex"),
    });
  }
  snapshot() {
    return {
      requestsObserved: this.sequence,
      overflow: this.overflow,
      failures: structuredClone(this.failures),
    };
  }
}
