import { expect, test, type Page } from "@playwright/test";
import type { IntegrationConnection } from "../src/shared/api/generated/openapi/types.gen";
import { policyConnection } from "./fixtures/credential-policy-data";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

async function setup(
  page: Page,
  lostResponse = false,
  state: IntegrationConnection["state"] = "DISABLED",
) {
  let current: IntegrationConnection = {
    ...structuredClone(policyConnection),
    state,
  };
  let writes = 0;
  const failures: string[] = [];
  page.on("pageerror", (error) => failures.push(error.message));
  await installSyntheticCsrf(page.context(), "p");
  await page.route("**/*", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const connectionPath = `/api/v1/integration-connections/${policyConnection.ref}`;
    if (path === connectionPath && request.method() === "GET") {
      await route.fulfill({ status: 200, json: current });
      return;
    }
    if (
      path === `${connectionPath}/credential-policy` &&
      request.method() === "PUT"
    ) {
      writes++;
      expect(request.headers()["if-match"]).toBe(
        `"${String(current.version)}"`,
      );
      expect(request.headers()["x-csrf-token"]).toBeTruthy();
      expect(request.headers()["idempotency-key"]).toBeTruthy();
      const body: unknown = request.postDataJSON();
      expect(body).toEqual({
        policy: writes === 1 ? "INITIATOR_REQUIRED" : "PROJECT_SHARED",
      });
      current = {
        ...current,
        version: current.version + 1,
        credentialPolicy:
          writes === 1 ? "INITIATOR_REQUIRED" : "PROJECT_SHARED",
      };
      if (lostResponse) await route.abort("failed");
      else await route.fulfill({ status: 200, json: current });
      return;
    }
    if (
      await handleSyntheticRuntimeRequest(
        route,
        new URL(request.url()),
        failures,
      )
    )
      return;
    if (path.startsWith("/api/")) {
      failures.push(`Unexpected synthetic API: ${request.method()} ${path}`);
      await route.abort("failed");
    } else
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${path}${new URL(request.url()).search}`,
        }),
      });
  });
  await page.goto("/e2e/fixtures/credential-policy.html");
  await page.getByRole("button", { name: "Выбрать режим ключа" }).click();
  await expect(
    page.getByRole("combobox", { name: "Чей ключ использовать" }),
  ).toHaveValue("PROJECT_SHARED");
  return { writes: () => writes, failures };
}

test("режим общего/личного ключа меняется явной командой без переноса секретов", async ({
  page,
}) => {
  const observed = await setup(page);
  const selection = page.getByRole("combobox", {
    name: "Чей ключ использовать",
  });
  await selection.selectOption("INITIATOR_REQUIRED");
  await page.getByRole("button", { name: "Сохранить режим" }).click();
  await expect(
    page.getByText("Режим сохранён.", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Открыть «Мои подключения»" }),
  ).toHaveAttribute("href", "/integrations?section=personal");
  await page.screenshot({
    path: test.info().outputPath("credential-policy.png"),
  });
  await selection.selectOption("PROJECT_SHARED");
  await page.getByRole("button", { name: "Сохранить режим" }).click();
  await expect(selection).toHaveValue("PROJECT_SHARED");
  await expect(
    page.getByRole("button", { name: "Сохранить режим" }),
  ).toBeDisabled();
  expect(observed.writes()).toBe(2);
  expect(await page.locator('input[type="password"]').count()).toBe(0);
  expect(observed.failures).toEqual([]);
});

test("потерянный ответ требует readback и не повторяет запись", async ({
  page,
}) => {
  const observed = await setup(page, true);
  await page.getByRole("combobox").selectOption("INITIATOR_REQUIRED");
  await page.getByRole("button", { name: "Сохранить режим" }).click();
  await expect(
    page.getByText("Результат записи не подтверждён.", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Сохранить режим" }),
  ).toBeDisabled();
  expect(observed.writes()).toBe(1);
  await page.getByRole("button", { name: "Обновить состояние" }).click();
  await expect(page.getByRole("combobox")).toHaveValue("INITIATOR_REQUIRED");
  await expect(
    page.getByText("Результат записи не подтверждён.", { exact: false }),
  ).toHaveCount(0);
  expect(observed.writes()).toBe(1);
  expect(observed.failures).toEqual([]);
});

test("fresh enabled состояние блокирует смену до любых записей", async ({
  page,
}) => {
  const observed = await setup(page, false, "CONNECTED");
  await expect(page.getByRole("combobox")).toBeDisabled();
  await expect(
    page.getByText("Для смены режима отключите подключение", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Сохранить режим" }),
  ).toBeDisabled();
  expect(observed.writes()).toBe(0);
  expect(observed.failures).toEqual([]);
});
