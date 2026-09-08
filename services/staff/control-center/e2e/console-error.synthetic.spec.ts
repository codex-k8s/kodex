import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";
import {
  ConsoleErrorDiagnostics,
  installConsoleErrorDiagnostics,
} from "./console-error-diagnostics";
import { persistSessionRenewalEvidence } from "./session-renewal-evidence";
test("console: настоящие ошибки и документы, безопасный list attachment", async ({
  page,
}, info) => {
  const d = new ConsoleErrorDiagnostics("https://kodex.test");
  installConsoleErrorDiagnostics(page, d, 0);
  await page.route("https://kodex.test/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: '<!doctype html><title>Fixture</title><script>function emitFixture(){console.error("TypeError: private-fixture");}</script>',
    }),
  );
  await page.goto("https://kodex.test/first?secret=private");
  await page.evaluate(() => {
    console.log("private-log");
    console.warn("private-warning");
  });
  expect(d.snapshot().total).toBe(0);
  d.beginStep();
  await page.evaluate("emitFixture()");
  await expect.poll(() => d.snapshot().total).toBe(1);
  d.endStep();
  await page.goto("https://kodex.test/first?secret=private#same");
  await page.evaluate("emitFixture()");
  await expect.poll(() => d.snapshot().total).toBe(2);
  await page.goto("https://kodex.test/second");
  d.setStage("CLEANUP");
  await page.evaluate("emitFixture()");
  await expect.poll(() => d.snapshot().total).toBe(3);
  const events = d.snapshot().events;
  expect(events.map((e) => e.errorClass)).toEqual([
    "TypeError",
    "TypeError",
    "TypeError",
  ]);
  expect(events.map((e) => e.stage)).toEqual([
    "UI_STEP",
    "BETWEEN_STEPS",
    "CLEANUP",
  ]);
  expect(events[0]?.observedDocument).toBeGreaterThan(0);
  expect(events[1]?.observedDocument).toBe(events[0]?.observedDocument);
  expect(events[2]?.observedDocument).toBe(
    Number(events[0]?.observedDocument) + 1,
  );
  expect(
    events.every(
      (e) => e.category === "APPLICATION_OTHER" && e.coordinatesKnown,
    ),
  ).toBe(true);
  expect(d.failed()).toBe(true);
  await page.close();
  const evidence = { status: "FAIL", consoleErrorDiagnostics: d.snapshot() };
  await persistSessionRenewalEvidence(info, evidence);
  const saved = await readFile(
    info.outputPath("session-renewal-safe-evidence.json"),
    "utf8",
  );
  expect(JSON.parse(saved)).toEqual(evidence);
  expect(saved).not.toMatch(/private|https:|secret=|emitFixture/);
  const attachment = info.attachments.find(
    (x) => x.name === "session-renewal-safe-evidence",
  );
  expect(attachment?.path).toBeTruthy();
  if (!attachment?.path) throw new Error("Missing fixture attachment");
  expect(await readFile(attachment.path, "utf8")).toBe(saved);
});
test("console: два таба, cap и cleanup остаются FAIL", async ({ context }) => {
  const d = new ConsoleErrorDiagnostics("https://kodex.test");
  const pages = await Promise.all([context.newPage(), context.newPage()]);
  for (const [index, page] of pages.entries()) {
    installConsoleErrorDiagnostics(page, d, index, () =>
      index === 0 ? "INITIAL_READY" : "CLEANUP",
    );
    await page.goto("data:text/html,<!doctype html><title>Fixture</title>");
    await page.evaluate(() => {
      for (let i = 0; i < 18; i++)
        console.error(`private fixture ${String(i)}`);
    });
  }
  await expect.poll(() => d.snapshot().total).toBe(36);
  expect(d.snapshot()).toMatchObject({ total: 36, overflow: 4 });
  expect(d.snapshot().events).toHaveLength(32);
  expect(
    d
      .snapshot()
      .events.some((e) => e.page === 0 && e.stage === "INITIAL_READY"),
  ).toBe(true);
  expect(
    d.snapshot().events.some((e) => e.page === 1 && e.stage === "CLEANUP"),
  ).toBe(true);
  expect(d.failed()).toBe(true);
  await Promise.all(pages.map((p) => p.close()));
});
