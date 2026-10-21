import type { Page } from "@playwright/test";
import {
  boundedField,
  digest,
  ErrorDiagnosticJournal,
  type ProjectionJournal,
  sourceCategory,
  trackObservedDocument,
} from "./error-diagnostic-journal";

const codes = new Map([
  ["Service worker registration failed", "SERVICE_WORKER_REGISTRATION"],
  ["Control Center bootstrap failed", "APPLICATION_BOOTSTRAP"],
]);
function location(value: unknown, origin: string) {
  const source = boundedField(value, "url", 4096);
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
      category: sourceCategory(url, origin),
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
function projectConsoleError(
  journal: ProjectionJournal,
  input: unknown,
  page: number,
  document: number,
  now: number,
) {
  const message = boundedField(input, "message", 4096);
  let rawLocation: unknown;
  try {
    rawLocation =
      input && typeof input === "object"
        ? Reflect.get(input, "location")
        : undefined;
  } catch {
    /* Закрытая неизвестная location. */
  }
  const source = location(rawLocation, journal.origin);
  const truncated = message.truncated || source.sourceTruncated;
  if (truncated) journal.observationOverflow();
  const prefix =
    /^(TypeError|ReferenceError|SyntaxError|RangeError|URIError|EvalError|AggregateError|DOMException|Error):(?: |$)/.exec(
      message.text,
    )?.[1];
  return {
    ...journal.context(page, document, now),
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

// jscpd:ignore-start
// Узкая типизированная обёртка связывает console-проекцию с общим журналом.
// Универсальный обработчик браузерного события скрыл бы происхождение location.
export class ConsoleErrorDiagnostics extends ErrorDiagnosticJournal<
  ReturnType<typeof projectConsoleError>
> {
  observe(
    error: unknown,
    page: number,
    document: number,
    now = Date.now(),
  ): void {
    this.record(() => projectConsoleError(this, error, page, document, now));
  }
}
// jscpd:ignore-end
export function installConsoleErrorDiagnostics(
  page: Page,
  reporter: ConsoleErrorDiagnostics,
  index: number,
  stage?: () => string,
): void {
  const observedDocument = trackObservedDocument(page, reporter);
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    if (stage) reporter.setStage(stage());
    // args()/JSHandle не читаются: произвольные объекты и DOM не сериализуются.
    try {
      reporter.observe(
        { message: message.text(), location: message.location() },
        index,
        observedDocument(),
      );
    } catch {
      reporter.observe(undefined, index, observedDocument());
    }
  });
}
