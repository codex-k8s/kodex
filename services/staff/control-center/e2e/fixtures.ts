import { expect, test as base, type Page } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";

export interface BrowserDiagnostics {
  readonly monitorPage: (page: Page) => void;
  readonly withExpectedNetworkInterruption: <T>(
    page: Page,
    action: () => Promise<T>,
  ) => Promise<T>;
}

export const test = base.extend<{
  browserDiagnostics: BrowserDiagnostics;
  freshOwnerSession: boolean;
}>({
  browserDiagnostics: [
    async ({ context, page }, use, testInfo) => {
      const failures = new Set<string>();
      const pendingRetryableFailures = new Map<string, string>();
      const monitoredPages = new WeakSet<Page>();
      const expectedNetworkInterruptions = new WeakMap<Page, number>();

      const networkInterruptionExpected = (target: Page): boolean =>
        (expectedNetworkInterruptions.get(target) ?? 0) > 0;

      const monitorPage = (target: Page): void => {
        if (monitoredPages.has(target)) return;
        monitoredPages.add(target);

        target.on("pageerror", (error) => {
          failures.add(
            `pageerror:${boundedToken(error.name)}:${boundedDiagnostic(error.message)}:${safeStackLocation(error.stack)}`,
          );
        });
        target.on("console", (message) => {
          if (
            message.type() === "error" &&
            !networkInterruptionExpected(target) &&
            !message.text().startsWith("Failed to load resource:")
          ) {
            const location = message.location().url || target.url();
            failures.add(
              `console:error:${safeURL(location)}:${boundedDiagnostic(message.text())}`,
            );
          }
        });
        target.on("requestfailed", (request) => {
          const errorText = request.failure()?.errorText;
          if (
            !networkInterruptionExpected(target) &&
            errorText !== "net::ERR_ABORTED" &&
            // Chromium может выдать эту ошибку при обновлении локального
            // сетевого сервиса; E2E-проверки всё равно требуют восстановления.
            errorText !== "net::ERR_NETWORK_CHANGED"
          ) {
            failures.add(
              `requestfailed:${boundedToken(request.method())}:${safeURL(request.url())}:${boundedDiagnostic(errorText ?? "unknown")}`,
            );
          }
        });
        target.on("response", (response) => {
          const request = response.request();
          const observation = observeHTTPResponse(
            pendingRetryableFailures,
            response.status(),
            request.method(),
            response.url(),
            request.headers()["idempotency-key"],
          );
          if (observation.failure) failures.add(observation.failure);
          if (observation.recovered) {
            testInfo.annotations.push({
              type: "bounded-http-recovery",
              description: observation.recovered,
            });
          }
        });
      };

      context.pages().forEach(monitorPage);
      context.on("page", monitorPage);
      monitorPage(page);

      await use({
        monitorPage,
        async withExpectedNetworkInterruption(target, action) {
          const depth = expectedNetworkInterruptions.get(target) ?? 0;
          expectedNetworkInterruptions.set(target, depth + 1);
          try {
            return await action();
          } finally {
            // Chromium сообщает об оборванных offline-переходом запросах
            // асинхронно, иногда уже после восстановления сети. Короткое окно
            // позволяет принять эти события, не скрывая ошибки следующих шагов.
            await target.waitForTimeout(500);
            if (depth === 0) expectedNetworkInterruptions.delete(target);
            else expectedNetworkInterruptions.set(target, depth);
          }
        },
      });

      context.off("page", monitorPage);
      for (const failure of pendingRetryableFailures.values()) {
        failures.add(failure);
      }
      if (failures.size > 0) {
        throw new Error(
          [
            "Browser diagnostics detected unexpected errors:",
            ...[...failures].sort().map((failure) => `- ${failure}`),
          ].join("\n"),
        );
      }
    },
    { auto: true },
  ],
  freshOwnerSession: [
    async ({ browserDiagnostics, page }, use) => {
      void browserDiagnostics;
      const authentication = await authenticateOwner(page, undefined, {
        mode: "warm",
      });
      expect(authentication.ownerSessionStatuses).toEqual([200]);
      await use(true);
    },
    { auto: true },
  ],
});

export { expect };

function safeURL(raw: string): string {
  if (!raw) return "unknown";
  try {
    const parsed = new URL(raw);
    return `${parsed.origin}${parsed.pathname}`.slice(0, 512);
  } catch {
    return "invalid-url";
  }
}

export function observeHTTPResponse(
  pending: Map<string, string>,
  status: number,
  method: string,
  url: string,
  idempotencyKey?: string,
): { failure?: string; recovered?: string } {
  const safeMethod = boundedToken(method);
  const location = safeURL(url);
  const failure = `response:${String(status)}:${safeMethod}:${location}`;
  const retryIdentity = mutationRetryIdentity(method, location, idempotencyKey);
  if (status >= 500) {
    if (retryIdentity) pending.set(retryIdentity, failure);
    return retryIdentity ? {} : { failure };
  }
  if (status < 200 || status >= 300 || !retryIdentity) return {};
  const recovered = pending.get(retryIdentity);
  if (!recovered) return {};
  pending.delete(retryIdentity);
  return { recovered };
}

function mutationRetryIdentity(
  method: string,
  location: string,
  idempotencyKey?: string,
) {
  if (!["POST", "PUT", "PATCH", "DELETE"].includes(method)) return undefined;
  const key = idempotencyKey ?? "";
  if (!/^[0-9a-f]{8}-[0-9a-f-]{27}$/i.test(key)) return undefined;
  return `${method}:${location}:${key}`;
}

function boundedToken(value: string): string {
  return /^[A-Za-z0-9_.:-]{1,80}$/.test(value) ? value : "other";
}

function boundedDiagnostic(value: string): string {
  return value
    .replace(/[\r\n\t]+/g, " ")
    .replace(/[^\p{L}\p{N}\p{P} ]+/gu, "?")
    .slice(0, 240);
}

function safeStackLocation(stack: string | undefined): string {
  const match = stack?.match(/https?:\/\/[^\s)]+/);
  if (!match) return "unknown";
  return safeURL(match[0]);
}
