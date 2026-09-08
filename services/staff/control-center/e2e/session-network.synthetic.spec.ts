import { expect, test, type Request } from "@playwright/test";
import { SessionRequestDiagnostics } from "./session-request-diagnostics";

test("synthetic: failed browser request сохраняет exact безопасную identity и остаётся failure", async ({
  page,
}) => {
  const observer = new SessionRequestDiagnostics<Request>("https://kodex.test");
  let failed = 0;
  page.on("request", (request) =>
    observer.start(request, {
      url: request.url(),
      method: request.method(),
      resourceType: request.resourceType(),
      stage: "INITIAL_READY",
      tab: 0,
    }),
  );
  page.on("requestfailed", (request) => {
    failed++;
    observer.failed(
      request,
      "READBACK",
      request.failure()?.errorText ?? "UNKNOWN",
    );
  });
  await page.route("https://kodex.test/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/api/v1/bootstrap")
      await route.abort("connectionreset");
    else
      await route.fulfill({
        contentType: "text/html",
        body: "<!doctype html><title>Fixture</title>",
      });
  });
  await page.goto("https://kodex.test");
  await page.evaluate(() =>
    fetch("/api/v1/bootstrap?private=fixture-marker", {
      headers: { "X-Fixture": "private-marker" },
    }).catch(() => undefined),
  );
  await expect.poll(() => failed).toBe(1);
  const result = observer.snapshot();
  expect(result.failures).toHaveLength(1);
  expect(result.failures[0]).toMatchObject({
    identityKnown: true,
    route: "BOOTSTRAP",
    method: "GET",
    resourceType: "fetch",
    startedStage: "INITIAL_READY",
    failedStage: "READBACK",
    tab: 0,
  });
  expect(result.failures[0]?.requestSequence).toBeGreaterThan(1);
  expect(JSON.stringify(result)).not.toMatch(
    /fixture-marker|private-marker|https:\/\//,
  );
  await page.unrouteAll({ behavior: "wait" });
});
