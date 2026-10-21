import type { Page } from "@playwright/test";
import {
  boundedField,
  boundedInteger,
  digest,
  ErrorDiagnosticJournal,
  type ProjectionJournal,
  sourceCategory,
  trackObservedDocument,
} from "./error-diagnostic-journal";

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
const codes = new Map([
  [
    "ResizeObserver loop completed with undelivered notifications.",
    "RESIZE_OBSERVER_UNDELIVERED",
  ],
  ["ResizeObserver loop limit exceeded", "RESIZE_OBSERVER_LIMIT"],
]);
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
      const line = boundedInteger(Number(match[2]), 10_000_000);
      const column = boundedInteger(Number(match[3]), 10_000_000);
      if (!line || !column || url.username || url.password) continue;
      const category = sourceCategory(url, origin);
      return { category, line, column, sourcePathSHA256: digest(url.pathname) };
    } catch {
      /* Неизвестный frame остаётся только в digest. */
    }
  }
  return { category: "UNKNOWN", line: 0, column: 0, sourcePathSHA256: "" };
}
function projectPageError(
  journal: ProjectionJournal,
  error: unknown,
  page: number,
  document: number,
  now: number,
) {
  const name = boundedField(error, "name", 128);
  const message = boundedField(error, "message", 4096);
  const stack = boundedField(error, "stack", 16384);
  const truncated = name.truncated || message.truncated || stack.truncated;
  if (truncated) journal.observationOverflow();
  return {
    ...journal.context(page, document, now),
    errorClass:
      classes.includes(name.text as (typeof classes)[number]) && !name.truncated
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
    ...location(stack.text, journal.origin),
  };
}

// jscpd:ignore-start
// Узкая типизированная обёртка связывает pageerror-проекцию с общим журналом.
// Универсальный обработчик браузерного события скрыл бы недоверенность stack.
export class PageErrorDiagnostics extends ErrorDiagnosticJournal<
  ReturnType<typeof projectPageError>
> {
  observe(
    error: unknown,
    page: number,
    document: number,
    now = Date.now(),
  ): void {
    this.record(() => projectPageError(this, error, page, document, now));
  }
}
// jscpd:ignore-end
export function installPageErrorDiagnostics(
  page: Page,
  reporter: PageErrorDiagnostics,
  index: number,
  stage?: () => string,
): void {
  const observedDocument = trackObservedDocument(page, reporter);
  page.on("pageerror", (error) => {
    if (stage) reporter.setStage(stage());
    reporter.observe(error, index, observedDocument());
  });
}
