import { expect, test } from "@playwright/test";

for (const width of [1440, 390]) {
  test(`публикация проверенной интеграции передаёт помощнику только ссылки, ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    await page.route("https://kodex.test/**", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unexpected API request ${url.pathname}`);
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto(
      "/e2e/fixtures/ui-proof.html?fixture=assistant-openapi-import",
    );
    await page.evaluate(() => {
      window.dispatchEvent(
        new CustomEvent("kodex:assistant:open", {
          detail: {
            configurationRef: "mcfg_synthetic",
            revisionRef: "mrev_validated",
          },
        }),
      );
    });
    const assistant = page.locator("#assistant-workspace");
    await expect(assistant).toBeVisible();
    const draft = assistant.locator(".assistant-composer textarea");
    await expect(draft).toHaveValue(/mcfg_synthetic/);
    await expect(draft).toHaveValue(/mrev_validated/);
    await expect(draft).not.toHaveValue(/openapi: 3\.1\.0|credential|apiKey/);
    await page.evaluate(() => {
      window.dispatchEvent(
        new CustomEvent("kodex:assistant:open", {
          detail: { configurationRef: "../unsafe", revisionRef: "mrev_other" },
        }),
      );
    });
    await expect(draft).toHaveValue(/mcfg_synthetic/);
    expect(failures).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}

for (const width of [1440, 390]) {
  test(`помощник открывает защищённый импорт OpenAPI внутри диалога, ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({
      width,
      height: width === 390 ? 844 : 900,
    });
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    await page.route("https://kodex.test/**", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unexpected API request ${url.pathname}`);
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto(
      "/e2e/fixtures/ui-proof.html?fixture=assistant-openapi-import",
    );
    await page.locator(".assistant-fab").click();
    const assistant = page.locator("#assistant-workspace");
    await expect(assistant).toBeVisible();
    await assistant
      .getByRole("link", { name: "защищённую форму импорта" })
      .click();
    const importDialog = page.getByRole("dialog", {
      name: "Импорт интеграции из OpenAPI",
    });
    await expect(importDialog).toBeVisible();
    await expect(assistant).toBeVisible();
    await expect(page.locator(".assistant-overlay")).toHaveAttribute(
      "inert",
      "",
    );
    const source = importDialog.getByRole("textbox", {
      name: "Контракт OpenAPI JSON или YAML",
    });
    await source.fill(
      "openapi: 3.1.0\ninfo:\n  title: Synthetic\n  version: 1.0.0",
    );
    await expect(assistant.locator(".assistant-chat-log")).not.toContainText(
      "Synthetic",
    );
    await importDialog.getByRole("button", { name: "Закрыть" }).click();
    await expect(importDialog).toHaveCount(0);
    await expect(assistant).toBeVisible();
    await expect(page.locator(".assistant-overlay")).not.toHaveAttribute(
      "inert",
      "",
    );
    expect(failures).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}

test("после защищённого импорта помощник показывает черновик без OpenAPI в чате", async ({
  page,
}) => {
  await page.context().addCookies([
    {
      name: "__Host-kodex-csrf",
      value: "synthetic-csrf-token-value-for-browser-fixture-0000000000",
      url: "https://kodex.test/",
      secure: true,
    },
  ]);
  const calls: string[] = [];
  await page.route("https://kodex.test/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === "/config/runtime-config.json") {
      await route.fulfill({
        json: {
          revision: "0".repeat(64),
          environment: "synthetic",
          apiBaseUrl: "/",
          realtimeUrl: "/api/v1",
          requestTimeoutMs: 10000,
          oidc: {
            authority: "https://identity.invalid",
            clientId: "synthetic",
            redirectUri: "/auth/callback",
            postLogoutRedirectUri: "/",
            scope: "openid",
          },
        },
      });
      return;
    }
    if (
      url.pathname ===
      "/api/v1/integration-definition-configurations/openapi-inspections"
    ) {
      calls.push("inspect");
      expect(request.method()).toBe("POST");
      await route.fulfill({
        json: {
          digest: "a".repeat(64),
          title: "Synthetic",
          version: "1.0.0",
          operations: [
            {
              operationId: "checkHealth",
              method: "GET",
              path: "/health",
              summary: "Health",
              serverOrigin: "https://example.invalid",
              candidate: true,
              healthCandidate: true,
              reason: "",
            },
          ],
        },
      });
      return;
    }
    if (
      url.pathname === "/api/v1/integration-definition-configurations/drafts"
    ) {
      calls.push("create");
      expect(request.method()).toBe("POST");
      const body = request.postDataJSON() as {
        name: string;
        contentFormat: string;
        content: string;
      };
      expect(body.name).toBe("Synthetic");
      expect(body.contentFormat).toBe("OPENAPI_IMPORT");
      expect(body.content).toContain("checkHealth");
      await route.fulfill({
        status: 201,
        json: {
          configuration: {
            ref: "cfg_synthetic_import",
            version: 1,
            kind: "INTEGRATION_DEFINITION",
            name: "Synthetic",
            managedBy: "UI",
            source: "",
            sourceRevision: "",
            archived: false,
            nextActions: [],
            updatedAt: "2026-09-25T00:00:00Z",
          },
          revision: {
            ref: "rev_synthetic_import",
            revision: 1,
            state: "DRAFT",
            contentFormat: "JSON",
            content: "{}",
            digest: "b".repeat(64),
            validationDiagnostics: [],
            createdAt: "2026-09-25T00:00:00Z",
          },
        },
      });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      calls.push("unexpected");
      await route.abort();
      return;
    }
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  await page.goto(
    "/e2e/fixtures/ui-proof.html?fixture=assistant-openapi-create",
  );
  expect(
    await page.evaluate(() => document.cookie.includes("__Host-kodex-csrf=")),
  ).toBe(true);
  await page.locator(".assistant-fab").click();
  const assistant = page.locator("#assistant-workspace");
  await assistant
    .getByRole("link", { name: "защищённую форму импорта" })
    .click();
  const importDialog = page.getByRole("dialog", {
    name: "Импорт интеграции из OpenAPI",
  });
  await importDialog
    .getByRole("textbox", { name: "Контракт OpenAPI JSON или YAML" })
    .fill("openapi: 3.1.0\ninfo:\n  title: Synthetic\n  version: 1.0.0");
  await importDialog
    .getByRole("button", { name: "Проверить контракт" })
    .click();
  await expect.poll(() => calls).toContain("inspect");
  await importDialog.getByRole("checkbox", { name: /GET \/health/ }).check();
  await importDialog
    .getByRole("combobox", { name: "Проверка соединения" })
    .selectOption("checkHealth");
  await importDialog.getByRole("button", { name: "Создать черновик" }).click();
  await expect(importDialog).toHaveCount(0);
  await expect(assistant.getByRole("status")).toContainText(
    "Черновик интеграции создан",
  );
  await expect(
    assistant.getByRole("button", { name: "Проверить черновик" }),
  ).toBeVisible();
  await expect(assistant.locator(".assistant-chat-log")).not.toContainText(
    "Synthetic",
  );
  expect(calls).toEqual(["inspect", "create"]);
});
