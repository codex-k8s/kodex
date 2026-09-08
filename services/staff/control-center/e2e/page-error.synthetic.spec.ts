import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";
import {
  PageErrorDiagnostics,
  installPageErrorDiagnostics,
} from "./page-error-diagnostics";
import { persistSessionRenewalEvidence } from "./session-renewal-evidence";
test("pageerror: настоящие исключения безопасно сохраняются через list reporter", async ({
  page,
}, testInfo) => {
  const reporter = new PageErrorDiagnostics("https://kodex.test");
  installPageErrorDiagnostics(page, reporter, 0);
  await page.route("https://kodex.test/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: "<!doctype html><title>Fixture</title>",
    }),
  );
  await page.goto("https://kodex.test/first?token=private-fixture");
  reporter.beginStep();
  const error = page.waitForEvent("pageerror");
  await page.evaluate(() => {
    setTimeout(() => {
      throw new TypeError("private-message-fixture");
    }, 10);
  });
  await error;
  reporter.endStep();
  await page.goto(
    "https://kodex.test/first?token=private-fixture#same-document",
  );
  const late = page.waitForEvent("pageerror");
  await page.evaluate(() => {
    setTimeout(() => {
      throw new ReferenceError("private-late-fixture");
    }, 10);
  });
  await late;
  await page.goto("https://kodex.test/second");
  const unknown = page.waitForEvent("pageerror");
  await page.evaluate(() => {
    setTimeout(() => {
      throw new Error("private-after-navigation-fixture");
    }, 10);
  });
  await unknown;
  expect(reporter.snapshot().events.map((event) => event.errorClass)).toEqual([
    "TypeError",
    "ReferenceError",
    "Error",
  ]);
  expect(reporter.snapshot().events.map((event) => event.stepSequence)).toEqual(
    [1, 0, 0],
  );
  const documents = reporter
    .snapshot()
    .events.map((event) => event.observedDocument);
  expect(documents[0]).toBeGreaterThan(0);
  expect(documents[1]).toBe(documents[0]);
  expect(documents[2]).toBe(Number(documents[0]) + 1);
  expect(
    reporter.snapshot().events.map((event) => event.documentAttribution),
  ).toEqual(["OBSERVATION_ONLY", "OBSERVATION_ONLY", "OBSERVATION_ONLY"]);
  expect(reporter.failed()).toBe(true);
  const evidence = {
    status: reporter.failed() ? "FAIL" : "PASS",
    pageErrorDiagnostics: reporter.snapshot(),
  };
  await page.close();
  await persistSessionRenewalEvidence(testInfo, evidence);
  const persisted = await readFile(
    testInfo.outputPath("session-renewal-safe-evidence.json"),
    "utf8",
  );
  expect(JSON.parse(persisted)).toEqual(evidence);
  for (const forbidden of [
    "private",
    "https://",
    "token=",
    "setTimeout",
    "throw new",
  ])
    expect(persisted).not.toContain(forbidden);
  const attachment = testInfo.attachments.find(
    (value) => value.name === "session-renewal-safe-evidence",
  );
  expect(attachment?.path).toBeTruthy();
  if (!attachment?.path) throw new Error("Missing safe fixture attachment");
  expect(await readFile(attachment.path, "utf8")).toBe(persisted);
});
test("pageerror: переполнение реальных browser events не становится PASS", async ({
  page,
}) => {
  const reporter = new PageErrorDiagnostics("https://kodex.test");
  installPageErrorDiagnostics(page, reporter, 1);
  await page.goto("data:text/html,<!doctype html><title>Fixture</title>");
  await page.evaluate(() => {
    for (let i = 0; i < 35; i++)
      setTimeout(() => {
        throw new Error(`overflow fixture ${String(i)}`);
      }, i);
  });
  await expect.poll(() => reporter.snapshot().total).toBe(35);
  expect(reporter.snapshot()).toMatchObject({ total: 35, overflow: 3 });
  expect(reporter.snapshot().events).toHaveLength(32);
  expect(reporter.failed()).toBe(true);
  await page.close();
});
