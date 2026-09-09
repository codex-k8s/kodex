import { expect, test } from "@playwright/test";
import { createHash } from "node:crypto";
import { createServer } from "node:http";
import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { installReadNetworkObserver } from "./ui-read-network";
import {
  PageErrorDiagnostics,
  installPageErrorDiagnostics,
} from "./page-error-diagnostics";

async function fixture(mode: "ready" | "http" | "contract" | "cors" = "ready") {
  const root = resolve("dist-document-lifecycle");
  let apiReads = 0;
  const foreign = createServer((_request, response) => {
    response.end("{}");
  });
  await new Promise<void>((resolve) => foreign.listen(0, "127.0.0.1", resolve));
  const foreignAddress = foreign.address();
  if (!foreignAddress || typeof foreignAddress === "string")
    throw new Error("Missing fixture port");
  const server = createServer((request, response) => {
    const url = new URL(request.url ?? "/", "http://fixture.invalid");
    if (url.pathname.startsWith("/api/v1/")) {
      apiReads++;
      const projects = Array.from({ length: 6 }, (_, index) => ({
        ref: `fixture-${String(index)}`,
        name: "Fixture",
        purpose: "Fixture",
        lifecycle: "ACTIVE",
        version: 1,
        integrationState: "READY",
        agentCount: 0,
        workflowCount: 0,
        activeRunCount: 0,
        pendingGateCount: 0,
        nextActions: [],
      }));
      if (url.pathname === "/api/v1/overview" && mode === "cors") {
        response.writeHead(302, {
          Location: `http://127.0.0.1:${String(foreignAddress.port)}/`,
        });
        response.end();
        return;
      }
      setTimeout(
        () => {
          response.writeHead(
            url.pathname === "/api/v1/overview" && mode === "http" ? 403 : 200,
            { "Content-Type": "application/json" },
          );
          response.end(
            JSON.stringify(
              url.pathname === "/api/v1/overview"
                ? mode === "http"
                  ? { code: "ACCESS_DENIED", status: 403 }
                  : mode === "contract"
                    ? {}
                    : { activeRuns: [], pendingGates: [], recentArtifacts: [] }
                : {
                    items: url.pathname === "/api/v1/projects" ? projects : [],
                    nextActions: [],
                    nextPageToken: "",
                    assistant: {},
                  },
            ),
          );
        },
        url.pathname === "/api/v1/projects" ? 180 : 10,
      );
      return;
    }
    if (
      url.pathname.startsWith("/assets/") &&
      /^\/assets\/[a-zA-Z0-9._-]+$/.test(url.pathname)
    ) {
      void readFile(resolve(root, `.${url.pathname}`)).then(
        (data) => {
          response.writeHead(200, {
            "Content-Type": url.pathname.endsWith(".css")
              ? "text/css"
              : "application/javascript",
          });
          response.end(data);
        },
        () => {
          response.writeHead(404);
          response.end();
        },
      );
      return;
    }
    setTimeout(
      () => {
        void readFile(
          resolve(root, "e2e/fixtures/document-lifecycle.html"),
        ).then(
          (data) => {
            response.writeHead(200, { "Content-Type": "text/html" });
            response.end(data);
          },
          () => {
            response.writeHead(404);
            response.end();
          },
        );
      },
      url.pathname === "/" ? 1800 : 0,
    );
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Missing fixture port");
  return {
    origin: `http://127.0.0.1:${String(address.port)}`,
    reads: () => apiReads,
    close: async () => {
      server.closeAllConnections();
      foreign.closeAllConnections();
      await Promise.all([
        new Promise<void>((resolve) => server.close(() => resolve())),
        new Promise<void>((resolve) => foreign.close(() => resolve())),
      ]);
    },
  };
}

for (const observer of [false, true]) {
  test(`document lifecycle: delayed Projects → close → locale, observer=${String(observer)}`, async ({
    page,
  }, testInfo) => {
    const server = await fixture();
    const errors = new PageErrorDiagnostics(server.origin);
    installPageErrorDiagnostics(page, errors, 0);
    if (observer) await installReadNetworkObserver(page, server.origin);
    try {
      await page.setViewportSize({ width: 390, height: 900 });
      await page.goto(`${server.origin}/projects`);
      await expect(page.locator(".project-list__item")).toHaveCount(6);
      await page.locator(".projects-toolbar .icon-button").click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.locator(".project-list__item")).toHaveCount(6);
      await dialog
        .getByRole("button", { name: "Закрыть", exact: true })
        .click();
      await expect(dialog).toHaveCount(0);
      await page.locator("#arm").click();
      await expect(page.locator("#result")).toHaveText("armed");
      await page.goto(`${server.origin}/`, { waitUntil: "domcontentloaded" });
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.locator("#locale").click();
      await expect(page.locator("html")).toHaveAttribute("lang", "en");
      await expect(page.locator(".project-list__item")).toHaveCount(6);
      const expectedMessages = new Set(
        [
          "bootstrap",
          "projects?pageSize=100",
          "overview",
          "runs?pageSize=100",
        ].map((route) => {
          const message = `Fetch API cannot load ${server.origin}/api/v1/${route} due to access control checks.`;
          return createHash("sha256")
            .update(message.slice(message.indexOf(":") + 2))
            .digest("hex");
        }),
      );
      const safePath = testInfo.outputPath("page-errors-safe.json");
      await writeFile(
        safePath,
        JSON.stringify({
          ...errors.snapshot(),
          knownWebKitAccessControl: errors
            .snapshot()
            .events.filter((event) => expectedMessages.has(event.messageSHA256))
            .length,
        }),
        { mode: 0o600, flag: "wx" },
      );
      await testInfo.attach("page-errors-safe", {
        path: safePath,
        contentType: "application/json",
      });
      expect(errors.snapshot().total).toBe(0);
      expect(server.reads()).toBe(2);
      await page.close();
    } finally {
      await server.close();
    }
  });
}

test("document lifecycle: отмена ухода не возобновляет старый resync; trusted input открывает новый", async ({
  page,
}) => {
  const server = await fixture();
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.name));
  try {
    await page.goto(`${server.origin}/projects`);
    await expect(page.locator(".project-list__item")).toHaveCount(6);
    await page.locator("#arm").click();
    await page.evaluate(() =>
      window.addEventListener("beforeunload", (event) =>
        event.preventDefault(),
      ),
    );
    let dismissed = 0;
    page.on("dialog", async (dialog) => {
      dismissed++;
      await dialog.dismiss();
    });
    await page.close({ runBeforeUnload: true });
    await expect.poll(() => dismissed).toBe(1);
    await expect(page.locator("#result")).toHaveText("cancelled");
    expect(server.reads()).toBe(1);
    await page.dispatchEvent("#resume", "pointerdown");
    await page.waitForTimeout(200);
    expect(server.reads()).toBe(1);
    await page.locator("#resume").click();
    await expect(page.locator("#result")).toHaveText("ready");
    // Initial list + девять resync reads + отдельный список ProjectsPage.
    expect(server.reads()).toBe(11);
    expect(errors).toEqual([]);
    await page.close();
  } finally {
    await server.close();
  }
});

for (const mode of ["http", "contract", "cors"] as const) {
  test(`document lifecycle: настоящая ${mode} ошибка остаётся видимой`, async ({
    page,
  }) => {
    const server = await fixture(mode);
    try {
      await page.goto(`${server.origin}/projects`);
      await expect(page.locator(".project-list__item")).toHaveCount(6);
      await page.locator("#resume").click();
      await expect(page.locator("#result")).toHaveText("error");
      await page.close();
    } finally {
      await server.close();
    }
  });
}
