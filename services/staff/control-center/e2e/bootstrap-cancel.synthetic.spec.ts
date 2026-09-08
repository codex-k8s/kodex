import { expect, test, type Request } from "@playwright/test";
import {
  installSessionBootstrapObserver,
  sessionBootstrapIDHeader,
} from "./session-bootstrap-observer";
import { SessionBootstrapCorrelator } from "./session-bootstrap-observer";

test("synthetic: SpeechAvailabilityLease отменяет exact bootstrap при synchronize", async ({
  page,
}) => {
  const origin = "http://127.0.0.1:43122";
  const correlator = new SessionBootstrapCorrelator<Request>(
    `${origin}/api/v1/bootstrap`,
  );
  const failures: Request[] = [];
  let first: Request | undefined;
  await installSessionBootstrapObserver(page, origin, (event) =>
    correlator.observe(event),
  );
  page.on("request", (request) => {
    correlator.request(
      request,
      request.url(),
      request.method(),
      request.headers()[sessionBootstrapIDHeader],
    );
  });
  page.on("requestfailed", (request) => {
    failures.push(request);
    correlator.failed(request);
  });
  await page.route(`${origin}/api/v1/bootstrap`, async (route) => {
    if (!first) {
      first = route.request();
      // Первый native fetch остаётся незавершённым до реального abort класса.
      return;
    }
    await route.fulfill({
      json: {
        speechTranscription: {
          available: false,
          reason: "STT_PERMISSION_DENIED",
        },
      },
    });
  });
  await page.goto(`${origin}/e2e/fixtures/bootstrap-cancel.html`);
  await page.locator("#start").click();
  await expect.poll(() => Boolean(first)).toBe(true);
  expect(failures).toHaveLength(0);
  await page.locator("#synchronize").click();
  await expect.poll(() => failures.length).toBe(1);
  expect(failures[0]).toBe(first);
  const cancelled = failures[0];
  if (!cancelled) throw new Error("Missing cancelled request");
  await expect.poll(() => correlator.cancelled(cancelled)).toBe(true);
  await expect(page.locator("#available")).toHaveText("false");
  await page.locator("#stop").click();
  await page.close();
});

test("synthetic: bootstrap identity не меняет body/headers и не попадает в другие запросы", async ({
  page,
}) => {
  const observed: boolean[] = [];
  await installSessionBootstrapObserver(
    page,
    "https://kodex.test",
    () => undefined,
  );
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.fulfill({
        contentType: "text/html",
        body: "<!doctype html><title>Fixture</title>",
      });
      return;
    }
    observed.push(Boolean(request.headers()[sessionBootstrapIDHeader]));
    if (new URL(request.url()).origin === "https://kodex.test")
      expect(request.headers()["x-original"]).toBe("kept");
    else expect(request.headers()["x-original"]).toBeUndefined();
    if (request.method() === "POST")
      expect(request.postData()).toBe("fixture-body");
    await route.fulfill({ body: "ok" });
  });
  await page.goto("https://kodex.test");
  await page.evaluate(async () => {
    const headers = { "x-original": "kept" };
    await fetch("/api/v1/bootstrap", { headers });
    await fetch("/api/v1/bootstrap?fixture=1", { headers });
    await fetch("/api/v1/bootstrap", {
      headers,
      method: "POST",
      body: "fixture-body",
    });
    await fetch("/api/v1/session/ticket", { headers });
    await fetch("https://foreign.example/api/v1/bootstrap", {
      headers,
      mode: "no-cors",
    });
  });
  expect(observed).toEqual([true, false, false, false, false]);
  await page.close();
});
