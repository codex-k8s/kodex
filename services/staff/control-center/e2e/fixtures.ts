import { expect, test as base, type Page } from "@playwright/test";

export interface BrowserDiagnostics {
  readonly monitorPage: (page: Page) => void;
  readonly withExpectedNetworkInterruption: <T>(
    page: Page,
    action: () => Promise<T>,
  ) => Promise<T>;
}

export const test = base.extend<{
  browserDiagnostics: BrowserDiagnostics;
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
          failures.add(`pageerror:${boundedToken(error.name)}`);
        });
        target.on("console", (message) => {
          if (
            message.type() === "error" &&
            !networkInterruptionExpected(target)
          ) {
            const location = message.location().url || target.url();
            failures.add(`console:error:${safeURL(location)}`);
          }
        });
        target.on("requestfailed", (request) => {
          if (
            !networkInterruptionExpected(target) &&
            request.failure()?.errorText !== "net::ERR_ABORTED"
          ) {
            failures.add(
              `requestfailed:${boundedToken(request.method())}:${safeURL(request.url())}`,
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
