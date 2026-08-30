import { expect, test as base, type Page } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EEnvironment } from "./environment";

const environment = loadE2EEnvironment();

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
    async ({ context, page }, use) => {
      const failures = new Set<string>();
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
          if (response.status() >= 500) {
            failures.add(
              `response:${String(response.status())}:${boundedToken(response.request().method())}:${safeURL(response.url())}`,
            );
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
      const sessionResponse = page.waitForResponse(
        (response) =>
          response.request().method() === "POST" &&
          new URL(response.url()).origin === environment.baseURL &&
          new URL(response.url()).pathname === "/api/v1/session",
        { timeout: 100_000 },
      );
      void browserDiagnostics;
      await authenticateOwner(page, undefined, { mode: "warm" });
      expect((await sessionResponse).status()).toBe(204);
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
