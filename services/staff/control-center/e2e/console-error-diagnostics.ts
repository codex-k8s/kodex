import { createHash } from "node:crypto";
import type { Page } from "@playwright/test";

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
  ["Service worker registration failed", "SERVICE_WORKER_REGISTRATION"],
  ["Control Center bootstrap failed", "APPLICATION_BOOTSTRAP"],
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
function location(value: unknown, origin: string) {
  const source = field(value, "url", 4096);
  const unknown = {
    category: "UNKNOWN",
    line: 0,
    column: 0,
    coordinatesKnown: false,
    sourcePathSHA256: "",
    sourceOriginSHA256: "",
    sourcePresent: source.present,
    sourceTruncated: source.truncated,
  };
  if (source.truncated) return unknown;
  try {
    const url = new URL(source.text);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password
    )
      return unknown;
    const line: unknown = Reflect.get(value as object, "line");
    const column: unknown = Reflect.get(value as object, "column");
    const valid = (n: unknown): n is number =>
      typeof n === "number" &&
      Number.isSafeInteger(n) &&
      n >= 0 &&
      n <= 10_000_000;
    return {
      ...unknown,
      category:
        url.origin !== origin
          ? "EXTERNAL"
          : /^\/assets\/[^/]+\.js$/.test(url.pathname)
            ? "APPLICATION_ASSET"
            : /^\/(src|node_modules)\//.test(url.pathname)
              ? "DEV_MODULE"
              : "APPLICATION_OTHER",
      line: valid(line) ? line : 0,
      column: valid(column) ? column : 0,
      coordinatesKnown: valid(line) && valid(column),
      sourcePathSHA256: digest(url.pathname),
      sourceOriginSHA256: digest(url.origin),
    };
  } catch {
    return unknown;
  }
}
export class ConsoleErrorDiagnostics {
  private readonly origin: string;
  private readonly started = Date.now();
  private total = 0;
  private overflow = 0;
  private events: ReturnType<ConsoleErrorDiagnostics["project"]>[] = [];
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
  private project(input: unknown, page: number, document: number, now: number) {
    const message = field(input, "message", 4096);
    let rawLocation: unknown;
    try {
      rawLocation =
        input && typeof input === "object"
          ? Reflect.get(input, "location")
          : undefined;
    } catch {
      /* Закрытая неизвестная location. */
    }
    const source = location(rawLocation, this.origin);
    const truncated = message.truncated || source.sourceTruncated;
    if (truncated) this.overflow++;
    const prefix =
      /^(TypeError|ReferenceError|SyntaxError|RangeError|URIError|EvalError|AggregateError|DOMException|Error):(?: |$)/.exec(
        message.text,
      )?.[1];
    return {
      sequence: this.total,
      page: integer(page, 3),
      pageKnown: Number.isSafeInteger(page) && page >= 0 && page <= 3,
      observedDocument: integer(document, documentLimit),
      // Текущий DCL — контекст наблюдения, а не доказательство документа происхождения.
      documentAttribution: "OBSERVATION_ONLY" as const,
      stepSequence: integer(this.activeStep, 10000),
      stage: this.stage,
      elapsedMs: integer(Math.floor(now - this.started), timeLimit),
      errorClass: !message.truncated ? (prefix ?? "UNKNOWN") : "UNKNOWN",
      classAttribution: "TEXT_ONLY" as const,
      code: !message.truncated
        ? (codes.get(message.text) ?? "UNKNOWN")
        : "UNKNOWN",
      messageSHA256: message.present ? digest(message.text) : "",
      digestScope: truncated ? "BOUNDED_PREFIX" : "FULL",
      messagePresent: message.present,
      sourceAttribution: "BROWSER_REPORTED" as const,
      coordinateBase: 0,
      ...source,
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
export function installConsoleErrorDiagnostics(
  page: Page,
  reporter: ConsoleErrorDiagnostics,
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
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    if (stage) reporter.setStage(stage());
    // args()/JSHandle не читаются: произвольные объекты и DOM не сериализуются.
    try {
      reporter.observe(
        { message: message.text(), location: message.location() },
        index,
        document,
      );
    } catch {
      reporter.observe(undefined, index, document);
    }
  });
}
