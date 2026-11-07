import { expect, test } from "@playwright/test";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

// HTTP fixtures проверяют реальные generated launch client, problem decoder и notice/settings
// components. Корпоративный пользователь и installed owner/gateway behavior не проверяются.
test("launch preflight показывает все подключения без retry и сохраняет задачу при настройке", async ({
  page,
  context,
}) => {
  const failures: string[] = [];
  let launches = 0;
  await installSyntheticCsrf(context, "p");
  await context.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (url.pathname === "/api/v1/me/integration-connections") {
      await route.fulfill({
        json: {
          items: [
            {
              connectionRef: "int_fixture01",
              connectionVersion: 7,
              name: "GitLab для проверки",
              definitionKey: "gitlab",
              policy: "INITIATOR_REQUIRED",
              enabled: true,
              available: true,
            },
          ],
          nextPageToken: "",
        },
      });
      return;
    }
    if (url.pathname === "/api/v1/runs" && request.method() === "POST") {
      launches++;
      expect(request.headers()["x-csrf-token"]).toBe("p".repeat(43));
      expect(request.headers()["idempotency-key"]).toBeTruthy();
      await route.fulfill({
        status: 409,
        contentType: "application/problem+json",
        json: {
          type: "urn:kodex:problem:integration_credentials_required",
          title: "Настройте личные подключения",
          status: 409,
          code: "INTEGRATION_CREDENTIALS_REQUIRED",
          correlationId: "10000000-0000-4000-8000-000000000001",
          retryable: false,
          integrationCredentialIssues: [
            {
              connectionRef: "int_fixture01",
              connectionName: "GitLab для проверки",
              capabilityKey: "gitlab.project.metadata.read",
              problem: "CREDENTIAL_REVOKED",
            },
            {
              connectionRef: "int_fixture02",
              connectionName: "Jira для проверки",
              capabilityKey: "jira.issue.read",
              problem: "CREDENTIAL_MISSING",
            },
          ],
        },
      });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      failures.push("unexpected API");
      await route.abort();
      return;
    }
    const pathname =
      url.pathname === "/integrations"
        ? "/e2e/fixtures/personal-connections.html"
        : url.pathname;
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${pathname}${url.search}`,
      }),
    });
  });
  page.on("pageerror", () => failures.push("pageerror"));
  await page.goto("/e2e/fixtures/personal-connections.html");
  const task = "Synthetic task must remain while credentials are configured";
  await page.getByLabel("Задача для synthetic запуска").fill(task);
  await page
    .getByRole("button", {
      name: "Проверить и запустить synthetic процесс",
      exact: true,
    })
    .click();
  await expect(
    page.getByRole("heading", {
      name: "Для запуска нужны ваши подключения",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Ключ отозван; добавьте новый", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByText("Jira для проверки", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/Процесс не создан/)).toBeVisible();
  const link = page.getByRole("link", {
    name: "Открыть «Мои подключения» в новой вкладке",
    exact: true,
  });
  await expect(link).toHaveAttribute("href", "/integrations?section=personal");
  const popupEvent = page.waitForEvent("popup");
  await link.click();
  const popup = await popupEvent;
  await popup.waitForLoadState();
  await expect(
    popup.getByRole("heading", { name: "Мои подключения", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Задача для synthetic запуска")).toHaveValue(
    task,
  );
  expect(launches).toBe(1);
  expect(failures).toEqual([]);
  await popup.close();
});
