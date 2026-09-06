import { expect, test } from "@playwright/test";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
} from "../src/shared/api/generated/openapi/types.gen";

for (const width of [390, 2900]) {
  test(`synthetic: Git source recovery и серверная история ${String(width)}px`, async ({
    page,
    context,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    page.on("console", (message) => {
      if (["warning", "error"].includes(message.type()))
        failures.push(message.text());
    });
    await context.addCookies([
      {
        name: "__Host-kodex-csrf",
        value: "s".repeat(43),
        domain: "kodex.test",
        path: "/",
        secure: true,
        sameSite: "Strict",
      },
    ]);
    const revision: ManagedConfigurationRevision = {
      sourceAvailable: true,
      ref: "revision_published",
      revision: 2,
      state: "PUBLISHED",
      contentFormat: "JSON",
      content: "{}",
      digest: "c".repeat(64),
      validationDiagnostics: [],
      createdAt: "2026-09-05T00:00:00Z",
    };
    let configuration: ManagedConfiguration = {
      sourceEditable: true,
      ref: "configuration",
      version: 8,
      kind: "ROLE_IMAGE",
      name: "Образ из Git",
      managedBy: "UI",
      source: "UI",
      sourceRevision: "",
      currentRevision: revision,
      updatedAt: revision.createdAt,
    };
    let accepted = false;
    let historyReads = 0;
    let writeBackHistoryReads = 0;
    const attempts: { key?: string; version?: string; body: unknown }[] = [];
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.origin !== "https://kodex.test") {
        failures.push("Unexpected origin");
        await route.abort();
        return;
      }
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
        "/api/v1/managed-configurations/configuration/git-write-backs"
      ) {
        expect(route.request().method()).toBe("GET");
        writeBackHistoryReads++;
        await route.fulfill({
          json: { items: [], total: 0, nextPageToken: "" },
        });
        return;
      }
      if (
        url.pathname ===
        "/api/v1/managed-configurations/configuration/revisions"
      ) {
        historyReads += 1;
        if (accepted && historyReads >= 4 && configuration.gitSource)
          configuration = {
            ...configuration,
            gitSource: {
              ...configuration.gitSource,
              version: 3,
              state: "READY",
              acceptedCommitSha: "a".repeat(40),
              acceptedContentSha256: "b".repeat(64),
              acceptedRevisionRef: revision.ref,
              syncedAt: revision.createdAt,
            },
            sourceRevision: "a".repeat(40),
          };
        await route.fulfill({
          json: { configuration, items: [revision], total: 1 },
        });
        return;
      }
      if (url.pathname === "/api/v1/integration-connections") {
        await route.fulfill({
          json: {
            items: [
              {
                ref: "connection",
                version: 12,
                name: "GitHub synthetic",
                definitionKey: "github",
              },
            ],
            total: 1,
          },
        });
        return;
      }
      if (url.pathname === "/api/v1/integration-connections/connection") {
        await route.fulfill({
          json: {
            ref: "connection",
            version: 12,
            name: "GitHub synthetic",
            definitionKey: "github",
          },
        });
        return;
      }
      if (
        url.pathname ===
        "/api/v1/role-image-configurations/configuration/git-source"
      ) {
        attempts.push({
          key: route.request().headers()["idempotency-key"],
          version: route.request().headers()["if-match"],
          body: route.request().postDataJSON() as unknown,
        });
        configuration = {
          ...configuration,
          version: 9,
          managedBy: "GIT",
          source: "source",
          gitSource: {
            ref: "source",
            version: 2,
            generation: 1,
            connectionRef: "connection",
            providerKey: "github",
            repositoryRef: "owner/repo",
            refName: "main",
            path: "image.yaml",
            state: "QUEUED",
          },
        };
        if (attempts.length <= 3) {
          await route.fulfill({
            status: 503,
            json: {
              type: "about:blank",
              status: 503,
              code: "UNAVAILABLE",
              title: "Synthetic unavailable",
            },
          });
          return;
        }
        accepted = true;
        await route.fulfill({ json: configuration, headers: { ETag: '"9"' } });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unexpected API ${url.pathname}`);
        await route.fulfill({ status: 404, json: {} });
        return;
      }
      const response = await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      });
      await route.fulfill({ response });
    });
    await page.goto("/e2e/fixtures/impact.html?kind=git");
    await page
      .getByRole("button", { name: "Подключить источник Git", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toContainText(
      "закрывает сохранённый неопубликованный черновик",
    );
    await dialog
      .getByRole("button", { name: "Соединение", exact: true })
      .click();
    await page.getByRole("option", { name: /GitHub synthetic/ }).click();
    await dialog.getByLabel("Репозиторий", { exact: true }).fill("owner/repo");
    await dialog.getByLabel("Ветка или ссылка", { exact: true }).fill("main");
    await dialog
      .getByLabel("Путь документа", { exact: true })
      .fill("image.yaml");
    await dialog
      .getByRole("button", { name: "Подключить источник Git", exact: true })
      .click();
    await expect.poll(() => attempts.length).toBe(3);
    await expect(
      page.getByText("Исход команды не подтверждён.", { exact: false }),
    ).toBeVisible();
    await page.reload();
    await page
      .getByRole("button", { name: "Повторить исходную команду", exact: true })
      .click();
    await expect(page.locator(".git-source-panel")).toContainText("READY");
    expect(attempts).toHaveLength(4);
    expect(new Set(attempts.map((attempt) => attempt.key)).size).toBe(1);
    expect(attempts.every((attempt) => attempt.version === '"8"')).toBe(true);
    expect(attempts[3]?.body).toEqual({
      connectionRef: "connection",
      expectedConnectionVersion: 12,
      repositoryRef: "owner/repo",
      refName: "main",
      path: "image.yaml",
      contentFormat: "YAML",
    });
    expect(historyReads).toBeGreaterThanOrEqual(4);
    await expect.poll(() => writeBackHistoryReads).toBeGreaterThan(0);
    await expect(page.locator(".git-source-panel")).toContainText(
      "b".repeat(64),
    );
    await expect(
      page.getByRole("button", { name: "Отсоединить от Git", exact: true }),
    ).toBeEnabled();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath("git-source-ready.png"),
      fullPage: true,
    });
    expect(failures).toEqual(
      Array<string>(3).fill(
        "Failed to load resource: the server responded with a status of 503 (Service Unavailable)",
      ),
    );
  });
}
