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
const eventLimit = 32;
export const documentLimit = 512;
const timeLimit = 86_400_000;

// Оба сборщика ограничивают поля до помещения данных браузера в журнал.
export function boundedInteger(value: number, maximum: number): number {
  return Number.isSafeInteger(value) && value >= 0 && value <= maximum
    ? value
    : 0;
}

export function boundedField(value: unknown, key: string, limit: number) {
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

export function digest(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

export function sourceCategory(url: URL, origin: string): string {
  return url.origin !== origin
    ? "EXTERNAL"
    : /^\/assets\/[^/]+\.js$/.test(url.pathname)
      ? "APPLICATION_ASSET"
      : /^\/(src|node_modules)\//.test(url.pathname)
        ? "DEV_MODULE"
        : "APPLICATION_OTHER";
}

// Журнал владеет порядком, стадией и ограниченным хранением событий.
// Каждый сборщик отдельно решает, какие поля браузера допустимо проецировать.
export class ErrorDiagnosticJournal<Event extends object> {
  readonly origin: string;
  private readonly started = Date.now();
  private total = 0;
  private overflow = 0;
  private events: Event[] = [];
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

  context(page: number, document: number, now: number) {
    return {
      sequence: this.total,
      page: boundedInteger(page, 3),
      pageKnown: Number.isSafeInteger(page) && page >= 0 && page <= 3,
      observedDocument: boundedInteger(document, documentLimit),
      // DCL задаёт контекст наблюдения, а не доказывает документ происхождения.
      documentAttribution: "OBSERVATION_ONLY" as const,
      stepSequence: boundedInteger(this.activeStep, 10000),
      stage: this.stage,
      elapsedMs: boundedInteger(Math.floor(now - this.started), timeLimit),
    };
  }

  protected record(project: () => Event): void {
    this.total++;
    if (this.events.length >= eventLimit) {
      this.overflow++;
      return;
    }
    this.events.push(project());
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

// Проекции видят только нормализованный origin, метаданные и счётчик переполнения.
export type ProjectionJournal = Pick<
  ErrorDiagnosticJournal<object>,
  "origin" | "context" | "observationOverflow"
>;

// Навигация и DOMContentLoaded отслеживают лишь наблюдаемый сейчас документ.
// Ни одно из событий не доказывает, какой документ породил последующую ошибку.
export function trackObservedDocument(
  page: Page,
  reporter: { observationOverflow(): void },
): () => number {
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
  return () => document;
}
