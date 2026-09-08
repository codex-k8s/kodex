import { createHash } from "node:crypto";
import type { Page } from "@playwright/test";

const classes = [
  "Error",
  "TypeError",
  "ReferenceError",
  "SyntaxError",
  "RangeError",
  "URIError",
  "EvalError",
  "AggregateError",
  "DOMException",
] as const;
const stages = [
  "PREFLIGHT",
  "INITIAL_READY",
  "NATURAL_RENEWAL",
  "READBACK",
  "COMPLETE",
  "UI_STEP",
  "BETWEEN_STEPS",
  "CLEANUP",
  "UNKNOWN",
] as const;
type Stage = (typeof stages)[number];
const codes = new Map([
  [
    "ResizeObserver loop completed with undelivered notifications.",
    "RESIZE_OBSERVER_UNDELIVERED",
  ],
  ["ResizeObserver loop limit exceeded", "RESIZE_OBSERVER_LIMIT"],
]);
const eventLimit = 32;
const documentLimit = 512;
const timeLimit = 86_400_000;
function integer(value: number, maximum: number): number {
  return Number.isSafeInteger(value) && value >= 0 && value <= maximum
    ? value
    : 0;
}
function field(value: unknown, key: string, limit: number) {
  try {
    const raw: unknown =
      value && typeof value === "object" ? Reflect.get(value, key) : undefined;
    if (typeof raw !== "string")
      return { text: "", present: false, truncated: false };
    return {
      text: raw.slice(0, limit),
      present: true,
      truncated: raw.length > limit,
    };
  } catch {
    return { text: "", present: false, truncated: false };
  }
}
const digest = (value: string) =>
  createHash("sha256").update(value).digest("hex");
function location(stack: string, origin: string) {
  // Разбираем только ограниченный frame suffix; имя функции и URL никогда не выдаются.
  for (const frame of stack.split("\n").slice(0, 8)) {
    const match =
      /(?:^|@|\()(https?:\/\/[^\s()]+):(\d{1,8}):(\d{1,8})\)?$/.exec(
        frame.trim(),
      );
    if (!match) continue;
    try {
      const url = new URL(match[1] ?? "");
      const line = integer(Number(match[2]), 10_000_000);
      const column = integer(Number(match[3]), 10_000_000);
      if (!line || !column || url.username || url.password) continue;
      const category =
        url.origin !== origin
          ? "EXTERNAL"
          : /^\/assets\/[^/]+\.js$/.test(url.pathname)
            ? "APPLICATION_ASSET"
            : /^\/(src|node_modules)\//.test(url.pathname)
              ? "DEV_MODULE"
              : "APPLICATION_OTHER";
      return { category, line, column, sourcePathSHA256: digest(url.pathname) };
    } catch {
      /* Неизвестный frame остаётся только в digest. */
    }
  }
  return { category: "UNKNOWN", line: 0, column: 0, sourcePathSHA256: "" };
}
export class PageErrorDiagnostics {
  private readonly origin: string;
  private readonly started = Date.now();
  private total = 0;
  private overflow = 0;
  private events: ReturnType<PageErrorDiagnostics["project"]>[] = [];
  private stepSequence = 0;
  private activeStep = 0;
  private stage: Stage = "PREFLIGHT";
  constructor(origin: string) {
    this.origin = new URL(origin).origin;
  }
  beginStep(): number {
    if (this.stepSequence >= 10000) {
      this.observationOverflow();
      this.activeStep = 0;
    } else this.activeStep = ++this.stepSequence;
    this.stage = "UI_STEP";
    return this.activeStep;
  }
  endStep(): void {
    this.activeStep = 0;
    this.stage = "BETWEEN_STEPS";
  }
  setStage(stage: string): void {
    this.activeStep = 0;
    this.stage = stages.includes(stage as Stage) ? (stage as Stage) : "UNKNOWN";
  }
  private project(error: unknown, page: number, document: number, now: number) {
    const name = field(error, "name", 128);
    const message = field(error, "message", 4096);
    const stack = field(error, "stack", 16384);
    const truncated = name.truncated || message.truncated || stack.truncated;
    if (truncated) this.overflow++;
    return {
      sequence: this.total,
      page: integer(page, 3),
      pageKnown: Number.isSafeInteger(page) && page >= 0 && page <= 3,
      observedDocument: integer(document, documentLimit),
      // pageerror не сообщает документ происхождения: текущий DCL — только контекст наблюдения.
      documentAttribution: "OBSERVATION_ONLY" as const,
      stepSequence: integer(this.activeStep, 10000),
      stage: this.stage,
      elapsedMs: integer(Math.floor(now - this.started), timeLimit),
      errorClass:
        classes.includes(name.text as (typeof classes)[number]) &&
        !name.truncated
          ? name.text
          : "UNKNOWN",
      code: !message.truncated
        ? (codes.get(message.text) ?? "UNKNOWN")
        : "UNKNOWN",
      messageSHA256: message.present ? digest(message.text) : "",
      stackSHA256: stack.present ? digest(stack.text) : "",
      digestScope: truncated ? "BOUNDED_PREFIX" : "FULL",
      messagePresent: message.present,
      stackPresent: stack.present,
      sourceAttribution: "UNVERIFIED_STACK" as const,
      ...location(stack.text, this.origin),
    };
  }
  observe(
    error: unknown,
    page: number,
    document: number,
    now = Date.now(),
  ): void {
    this.total++;
    if (this.events.length >= eventLimit) {
      this.overflow++;
      return;
    }
    this.events.push(this.project(error, page, document, now));
  }
  observationOverflow(): void {
    this.overflow++;
  }
  snapshot() {
    return {
      total: this.total,
      overflow: this.overflow,
      eventLimit,
      events: this.events.map((event) => ({ ...event })),
    };
  }
  failed(): boolean {
    return this.total > 0 || this.overflow > 0;
  }
}
export function installPageErrorDiagnostics(
  page: Page,
  reporter: PageErrorDiagnostics,
  index: number,
  stage?: () => string,
): void {
  let document = 0;
  let nextDocument = 0;
  page.on("request", (request) => {
    if (!request.isNavigationRequest()) return;
    try {
      if (request.frame() === page.mainFrame()) document = 0;
    } catch {
      document = 0;
    }
  });
  page.on("domcontentloaded", () => {
    if (nextDocument >= documentLimit) {
      reporter.observationOverflow();
      document = 0;
    } else document = ++nextDocument;
  });
  page.on("pageerror", (error) => {
    if (stage) reporter.setStage(stage());
    reporter.observe(error, index, document);
  });
}
