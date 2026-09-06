import { readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { test as base, type Page } from "@playwright/test";

type Stage = "BEFORE_EMAIL" | "AFTER_OIDC" | "BEFORE_SELECT" | "AFTER_SELECT";
const samplers = new WeakMap<Page, (stage: Stage) => Promise<void>>();
async function diagnosticBudget<T>(operation: Promise<T>): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      operation,
      new Promise<never>((_resolve, reject) => {
        timer = setTimeout(
          () => reject(new Error("Diagnostic operation budget exceeded")),
          5000,
        );
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}
export async function sampleBrowserDiagnostics(page: Page, stage: Stage) {
  await samplers.get(page)?.(stage);
}

async function memoryEvents() {
  const groups: Array<Record<string, number>> = [];
  try {
    const membership = await readFile("/proc/self/cgroup", "utf8");
    const group = membership
      .split("\n")
      .find((line) => line.startsWith("0::"))
      ?.slice(3);
    if (!group) return groups;
    let path = join("/sys/fs/cgroup", group);
    while (path.startsWith("/sys/fs/cgroup")) {
      const content = await readFile(join(path, "memory.events"), "utf8").catch(
        () => "",
      );
      if (content) {
        const counters: Record<string, number> = {};
        for (const line of content.trim().split("\n")) {
          const [key, value] = line.split(" ");
          if (
            key &&
            ["oom", "oom_kill", "oom_group_kill", "high", "max"].includes(key)
          )
            counters[key] = Number(value);
        }
        groups.push(counters);
      }
      if (path === "/sys/fs/cgroup") break;
      path = dirname(path);
    }
  } catch {
    return groups;
  }
  return groups;
}

export const test = base.extend<{ browserDiagnostics: undefined }>({
  browserDiagnostics: [
    async ({ page, browser }, use, testInfo) => {
      if (process.env.KODEX_SYNTHETIC_DIAGNOSTICS !== "1") {
        await use(undefined);
        return;
      }
      if (browser.browserType().name() !== "chromium")
        throw new Error("Synthetic diagnostics require Chromium");
      const cdp = await browser.newBrowserCDPSession();
      let metrics = await page.context().newCDPSession(page);
      await cdp.send("Target.setDiscoverTargets", { discover: true });
      await metrics.send("Performance.enable");
      const records: unknown[] = [
        { stage: "START", memory: await memoryEvents() },
      ];
      const diagnosticsPath = testInfo.outputPath("browser-diagnostics.json");
      let pendingWrite = Promise.resolve();
      const persist = () => {
        const snapshot = JSON.stringify(records, null, 2);
        pendingWrite = pendingWrite.then(() =>
          writeFile(diagnosticsPath, snapshot),
        );
        return pendingWrite;
      };
      await persist();
      const requests: string[] = [];
      const operations: Record<string, string> = {
        "/.well-known/openid-configuration": "OIDC_DISCOVERY",
        "/authorize": "OIDC_AUTHORIZE",
        "/token": "OIDC_TOKEN",
        "/api/v1/session": "SESSION",
        "/api/v1/integration-invocations/invocation_synthetic/email-effect-receipt":
          "EMAIL_RECEIPT",
        "/api/v1/email-effect-receipts/receipt_synthetic/reconciliation":
          "EMAIL_DECISION",
      };
      page.on("request", (request) => {
        const url = new URL(request.url());
        const label = ["kodex.test", "identity.invalid"].includes(url.hostname)
          ? operations[url.pathname]
          : undefined;
        if (label && requests.length < 100) requests.push(label);
      });
      cdp.on(
        "Target.targetCrashed",
        (event: { status: string; errorCode: number }) => {
          records.push({
            stage: "TARGET_CRASHED",
            status: /^[a-zA-Z_-]{1,64}$/.test(event.status)
              ? event.status
              : "OTHER",
            errorCode: event.errorCode,
            requests: [...requests],
          });
          void persist().catch(() => undefined);
        },
      );
      const traceState = { started: false };
      samplers.set(page, async (stage) => {
        records.push({
          stage: `${stage}_START`,
          requests: [...requests],
          memory: await memoryEvents(),
        });
        await persist();
        try {
          if (!traceState.started) {
            await diagnosticBudget(
              page.context().tracing.start({
                screenshots: true,
                snapshots: true,
                sources: false,
              }),
            );
            traceState.started = true;
          }
          if (stage === "AFTER_OIDC") {
            await diagnosticBudget(metrics.detach()).catch(() => undefined);
            metrics = await diagnosticBudget(
              page.context().newCDPSession(page),
            );
            await diagnosticBudget(metrics.send("Performance.enable"));
          }
          const data = await diagnosticBudget(
            metrics.send("Performance.getMetrics"),
          );
          records.push({
            stage,
            metrics: data.metrics.filter((metric) =>
              [
                "JSHeapUsedSize",
                "JSHeapTotalSize",
                "Nodes",
                "Documents",
                "LayoutCount",
                "RecalcStyleCount",
                "TaskDuration",
                "ScriptDuration",
              ].includes(metric.name),
            ),
            requests: [...requests],
            memory: await memoryEvents(),
          });
        } catch {
          records.push({ stage: `${stage}_METRICS_UNAVAILABLE` });
        }
        await persist();
      });
      try {
        try {
          await diagnosticBudget(
            page.context().tracing.start({
              screenshots: true,
              snapshots: true,
              sources: false,
            }),
          );
          traceState.started = true;
        } catch {
          records.push({ stage: "INITIAL_TRACE_UNAVAILABLE" });
          await persist();
        }
        await use(undefined);
      } finally {
        samplers.delete(page);
        records.push({ stage: "END", memory: await memoryEvents() });
        await persist();
        const cleanup = await diagnosticBudget(
          Promise.allSettled([
            ...(traceState.started
              ? [
                  page.context().tracing.stop({
                    path: testInfo.outputPath("email-diagnostic-trace.zip"),
                  }),
                ]
              : []),
            metrics.detach(),
            cdp.detach(),
          ]),
        ).catch(() => undefined);
        records.push({
          stage: "CLEANUP",
          outcomes: cleanup?.map((result) => result.status) ?? [
            "BUDGET_EXCEEDED",
          ],
        });
        await persist();
        await testInfo.attach("browser-diagnostics", {
          path: diagnosticsPath,
          contentType: "application/json",
        });
      }
    },
    { auto: true },
  ],
});
