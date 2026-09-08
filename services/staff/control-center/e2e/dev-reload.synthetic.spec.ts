import { writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { createServer } from "node:http";
import {
  PageErrorDiagnostics,
  installPageErrorDiagnostics,
} from "./page-error-diagnostics";
import { expect, test } from "@playwright/test";
import { remoteReloadClientSource } from "../vite.config";
import { installReadNetworkObserver } from "./ui-read-network";
for (const observed of [false, true]) {
  test(`dev reload: outage caught with observer=${String(observed)}`, async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) =>
      errors.push(`${error.name}:${error.message}`),
    );
    if (observed) await installReadNetworkObserver(page, "https://kodex.test");
    let attempts = 0;
    await page.route("https://kodex.test/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/__kodex_dev_revision") {
        attempts++;
        await route.abort("connectionfailed");
      } else if (path === "/__kodex_dev_reload.js")
        await route.fulfill({
          contentType: "application/javascript",
          body: remoteReloadClientSource(),
        });
      else
        await route.fulfill({
          contentType: "text/html",
          body: '<!doctype html><script type="module" src="/__kodex_dev_reload.js"></script>',
        });
    });
    await page.goto("https://kodex.test/");
    await expect.poll(() => attempts).toBeGreaterThanOrEqual(2);
    await page.close();
    expect(errors).toEqual([]);
  });
}
for (const observed of [false, true]) {
  test(`dev reload: pagehide aborts and pageshow resumes once observer=${String(observed)}`, async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.name));
    if (observed) await installReadNetworkObserver(page, "https://kodex.test");
    let attempts = 0;
    await page.route("https://kodex.test/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/__kodex_dev_revision") {
        attempts++;
        if (attempts === 1) return;
        await route.fulfill({
          contentType: "text/plain",
          body: "00000000-0000-0000-0000-000000000000:1",
        });
      } else if (path === "/__kodex_dev_reload.js")
        await route.fulfill({
          contentType: "application/javascript",
          body: remoteReloadClientSource(),
        });
      else
        await route.fulfill({
          contentType: "text/html",
          body: '<!doctype html><script type="module" src="/__kodex_dev_reload.js"></script>',
        });
    });
    await page.goto("https://kodex.test/", { waitUntil: "domcontentloaded" });
    await expect.poll(() => attempts).toBe(1);
    await page.evaluate(() =>
      window.dispatchEvent(
        new PageTransitionEvent("pagehide", { persisted: true }),
      ),
    );
    await page.waitForTimeout(1200);
    expect(attempts).toBe(1);
    await page.evaluate(() => {
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: true }),
      );
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: true }),
      );
    });
    await expect.poll(() => attempts).toBe(2);
    await page.goto("https://kodex.test/next", {
      waitUntil: "domcontentloaded",
    });
    await expect.poll(() => attempts).toBeGreaterThanOrEqual(3);
    await page.close();
    expect(errors).toEqual([]);
  });
}
test("dev reload: native timeout recovers and revision causes one reload", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.name));
  let attempts = 0,
    documents = 0;
  await page.route("https://kodex.test/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/__kodex_dev_revision") {
      attempts++;
      if (attempts === 1) return;
      await route.fulfill({
        contentType: "text/plain",
        body: `00000000-0000-0000-0000-000000000000:${String(attempts === 2 ? 1 : 2)}`,
      });
    } else if (path === "/__kodex_dev_reload.js")
      await route.fulfill({
        contentType: "application/javascript",
        body: remoteReloadClientSource(),
      });
    else {
      documents++;
      await route.fulfill({
        contentType: "text/html",
        body: '<!doctype html><script type="module" src="/__kodex_dev_reload.js"></script>',
      });
    }
  });
  await page.goto("https://kodex.test/", { waitUntil: "domcontentloaded" });
  await expect
    .poll(() => attempts, { timeout: 7000 })
    .toBeGreaterThanOrEqual(3);
  await expect.poll(() => documents).toBe(2);
  await page.waitForTimeout(1500);
  expect(documents).toBe(2);
  await page.close();
  expect(errors).toEqual([]);
});

for (const observed of [false, true]) {
  test(`dev reload: real navigation cancels pending native request observer=${String(observed)}`, async ({
    page,
    context,
  }, testInfo) => {
    let requests = 0;
    const server = createServer((request, response) => {
      if (request.url === "/__kodex_dev_revision") {
        requests++;
        response.writeHead(200, { "Content-Type": "text/plain" });
        response.flushHeaders();
        const timer = setTimeout(() => {
          response.end("00000000-0000-0000-0000-000000000000:1");
        }, 0);
        response.on("close", () => clearTimeout(timer));
      } else if (request.url === "/__kodex_dev_reload.js") {
        response.writeHead(200, { "Content-Type": "application/javascript" });
        response.end(remoteReloadClientSource());
      } else {
        const timer = setTimeout(
          () => {
            response.writeHead(200, { "Content-Type": "text/html" });
            response.end(
              '<!doctype html><script type="module" src="/__kodex_dev_reload.js"></script>',
            );
          },
          request.url === "/doc0" ? 0 : 1800,
        );
        response.on("close", () => clearTimeout(timer));
      }
    });
    await new Promise<void>((resolve) =>
      server.listen(0, "127.0.0.1", resolve),
    );
    const address = server.address();
    if (!address || typeof address === "string")
      throw new Error("Missing fixture port");
    const origin = `http://127.0.0.1:${String(address.port)}`;
    await context.route("**/*", (route) => route.continue());
    const errors = new PageErrorDiagnostics(origin);
    const expectedMessage = `Fetch API cannot load ${origin}/__kodex_dev_revision due to access control checks.`;
    const expectedDigest = createHash("sha256")
      .update(expectedMessage.substring(expectedMessage.indexOf(":") + 2))
      .digest("hex");
    installPageErrorDiagnostics(page, errors, 0);
    if (observed) await installReadNetworkObserver(page, origin);
    try {
      for (let i = 0; i < 3; i++) {
        const before = requests;
        await page.goto(`${origin}/doc${String(i)}`, {
          waitUntil: "domcontentloaded",
        });
        await expect
          .poll(() => requests, { intervals: [10] })
          .toBeGreaterThan(before);
      }
      await page.close();
      const safePath = testInfo.outputPath("navigation-safe.json");
      await writeFile(
        safePath,
        JSON.stringify({
          sourceSHA256: createHash("sha256")
            .update(remoteReloadClientSource())
            .digest("hex"),
          ...errors.snapshot(),
          allMatchAccessControl: errors
            .snapshot()
            .events.every((e) => e.messageSHA256 === expectedDigest),
        }),
        { mode: 0o600, flag: "wx" },
      );
      await testInfo.attach("safe-navigation-pageerrors", {
        path: safePath,
        contentType: "application/json",
      });
      expect(errors.snapshot().total).toBe(0);
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });
}

test("dev reload: dismissed beforeunload resumes once after trusted input", async ({
  page,
}) => {
  let attempts = 0;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.name));
  await page.route("https://kodex.test/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/__kodex_dev_revision") {
      attempts++;
      await route.fulfill({
        contentType: "text/plain",
        body: "00000000-0000-0000-0000-000000000000:1",
      });
    } else if (path === "/__kodex_dev_reload.js")
      await route.fulfill({
        contentType: "application/javascript",
        body: remoteReloadClientSource(),
      });
    else
      await route.fulfill({
        contentType: "text/html",
        body: '<!doctype html><button>Fixture</button><script type="module" src="/__kodex_dev_reload.js"></script>',
      });
  });
  await page.goto("https://kodex.test/");
  await expect.poll(() => attempts).toBeGreaterThan(0);
  await page.getByRole("button").click();
  await page.evaluate(() =>
    window.addEventListener("beforeunload", (event) => {
      event.preventDefault();
    }),
  );
  let dialogs = 0;
  page.on("dialog", (dialog) => {
    dialogs++;
    return dialog.dismiss();
  });
  await page.close({ runBeforeUnload: true });
  await expect.poll(() => dialogs).toBe(1);
  expect(dialogs).toBe(1);
  expect(page.url()).toBe("https://kodex.test/");
  const before = attempts;
  await page.waitForTimeout(1200);
  expect(attempts).toBe(before);
  await page.dispatchEvent("button", "pointerdown");
  await page.waitForTimeout(1200);
  expect(attempts).toBe(before);
  await page.getByRole("button").click();
  await expect.poll(() => attempts).toBe(before + 1);
  await page.close();
  expect(errors).toEqual([]);
});
test("dev reload: real cross-origin refusal remains observable", async ({
  page,
  browserName,
}) => {
  const foreign = createServer((_request, response) => {
    response.writeHead(200, { "Content-Type": "text/plain" });
    response.end("00000000-0000-0000-0000-000000000000:2");
  });
  await new Promise<void>((resolve) => foreign.listen(0, "127.0.0.1", resolve));
  const foreignAddress = foreign.address();
  if (!foreignAddress || typeof foreignAddress === "string")
    throw new Error("Missing fixture port");
  const server = createServer((request, response) => {
    if (request.url === "/__kodex_dev_revision") {
      response.writeHead(302, {
        Location: `http://127.0.0.1:${String(foreignAddress.port)}/`,
      });
      response.end();
    } else if (request.url === "/__kodex_dev_reload.js") {
      response.writeHead(200, { "Content-Type": "application/javascript" });
      response.end(remoteReloadClientSource());
    } else {
      response.writeHead(200, { "Content-Type": "text/html" });
      response.end(
        '<!doctype html><script type="module" src="/__kodex_dev_reload.js"></script>',
      );
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Missing fixture port");
  const origin = `http://127.0.0.1:${String(address.port)}`;
  const errors = new PageErrorDiagnostics(origin);
  installPageErrorDiagnostics(page, errors, 0);
  let failed = 0,
    consoleErrors = 0;
  page.on("requestfailed", () => failed++);
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors++;
  });
  try {
    await page.goto(origin);
    await expect
      .poll(() => failed + consoleErrors + errors.snapshot().total)
      .toBeGreaterThan(0);
    if (browserName === "webkit") {
      await expect.poll(() => errors.snapshot().total).toBeGreaterThan(0);
      expect(errors.failed()).toBe(true);
    }
    await page.close();
  } finally {
    server.closeAllConnections();
    foreign.closeAllConnections();
    await Promise.all([
      new Promise<void>((resolve) => server.close(() => resolve())),
      new Promise<void>((resolve) => foreign.close(() => resolve())),
    ]);
  }
});
