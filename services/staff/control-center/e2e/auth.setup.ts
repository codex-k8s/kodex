import { rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";

import { expect, test, type Browser, type Page } from "@playwright/test";

import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { withoutKodexAPICookies, writeStorageState } from "./storage-state";

const environment = loadE2EAuthEnvironment();
const delayedSessionResponseMs = 8_500;

test.describe("OIDC-сессия владельца", () => {
  test("владелец входит через настроенный OIDC", async ({ page, context }) => {
    // Trace, video и screenshot отключены в auth config: credentials не попадают
    // в reporter или browser artifacts.
    const sessionResponse = waitForOwnerSession(page);
    await authenticateOwner(
      page,
      {
        username: environment.ownerUsername,
        password: environment.ownerPassword,
      },
      { mode: "local" },
    );
    expect((await sessionResponse).status()).toBe(204);

    await expect(page).toHaveURL(
      new RegExp(`^${escapeRegExp(environment.baseURL)}/`),
    );
    await page.locator(".current-user-menu__trigger").click();
    await expect(
      page.getByRole("button", { name: "Выйти", exact: true }),
    ).toBeVisible();

    const bootstrap = withoutKodexAPICookies(await context.storageState());
    await writeStorageState(environment.outputStorageState, bootstrap);
  });

  test("dev watcher не перезагружает страницу при записи OIDC storage state", async ({
    browser,
  }) => {
    const context = await warmOwnerContext(browser);
    const page = await context.newPage();
    let viteHMRFramesReceived = 0;
    page.on("websocket", (socket) => {
      if (new URL(socket.url()).searchParams.has("token")) {
        socket.on("framereceived", () => {
          viteHMRFramesReceived += 1;
        });
      }
    });
    const probePath = fileURLToPath(
      new URL("../.auth/watcher-probe.json", import.meta.url),
    );

    try {
      await authenticateOwner(page, undefined, { mode: "warm" });
      await expect(page.locator(".app-shell")).toBeVisible();
      const documentMarker = await page.evaluate(() => {
        const marker = crypto.randomUUID();
        document.documentElement.dataset.oidcWatcherProbe = marker;
        return marker;
      });
      const bootstrap = withoutKodexAPICookies(await context.storageState());

      await writeStorageState(probePath, bootstrap);
      // Покрывает несколько циклов polling watcher и возможный debounce Vite.
      await page.waitForTimeout(12_000);

      await expect(page.locator(".app-shell")).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.dataset.oidcWatcherProbe,
        ),
      ).toBe(documentMarker);
      expect(viteHMRFramesReceived).toBe(0);
    } finally {
      await context.close();
      await rm(probePath, { force: true });
    }
  });

  test("warm OIDC дожидается медленного callback в общем auth budget", async ({
    browser,
  }) => {
    const context = await warmOwnerContext(browser);
    const page = await context.newPage();
    let delayedRequests = 0;
    await page.route(ownerSessionURL(), async (route) => {
      if (route.request().method() !== "POST") {
        await route.continue();
        return;
      }
      delayedRequests += 1;
      await new Promise<void>((resolve) =>
        globalThis.setTimeout(resolve, delayedSessionResponseMs),
      );
      await route.continue();
    });

    try {
      const sessionResponse = waitForOwnerSession(page);
      await authenticateOwner(page, undefined, { mode: "warm" });

      expect((await sessionResponse).status()).toBe(204);
      expect(delayedRequests).toBe(1);
      await expect(page.locator(".app-shell")).toBeVisible();
      await expect(page).not.toHaveURL(/\/auth\/callback/);
    } finally {
      await context.close();
    }
  });

  test("warm OIDC закрыто отклоняет ошибку создания owner session", async ({
    browser,
  }) => {
    const context = await warmOwnerContext(browser);
    const page = await context.newPage();
    await page.route(ownerSessionURL(), async (route) => {
      if (route.request().method() !== "POST") {
        await route.continue();
        return;
      }
      await route.fulfill({
        body: JSON.stringify({ title: "Synthetic owner session failure" }),
        contentType: "application/problem+json",
        status: 503,
      });
    });

    try {
      await expect(
        authenticateOwner(page, undefined, { mode: "warm" }),
      ).rejects.toThrow(
        new RegExp(
          `frontend OIDC transition received an HTTP error \\(503:POST:${escapeRegExp(ownerSessionURL())}\\)`,
        ),
      );
    } finally {
      await context.close();
    }
  });
});

async function warmOwnerContext(browser: Browser) {
  return await browser.newContext({
    baseURL: environment.baseURL,
    locale: "ru-RU",
    storageState: environment.outputStorageState,
  });
}

function waitForOwnerSession(page: Page) {
  return page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).origin === environment.baseURL &&
      new URL(response.url()).pathname === "/api/v1/session",
  );
}

function ownerSessionURL(): string {
  return new URL("/api/v1/session", environment.baseURL).href;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
