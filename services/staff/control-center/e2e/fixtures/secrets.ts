import { expect, type Page } from "@playwright/test";
import type {
  RuntimeSecret,
  RuntimeSecretImpact,
  RuntimeSecretRebindInput,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkSecretEditor(page: Page, projectRef: string) {
  const failures: string[] = [];
  let created: RuntimeSecret | undefined;
  let creates = 0;
  await page.route(
    `**/api/v1/projects/${projectRef}/runtime-secrets*`,
    async (route) => {
      if (route.request().method() === "GET") {
        // Список намеренно отстаёт от подтверждённой мутации.
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (route.request().method() !== "POST") {
        failures.push("Unexpected secret catalog method");
        await route.fulfill({ status: 405 });
        return;
      }
      creates += 1;
      const headers = route.request().headers();
      if (!headers["idempotency-key"] || !headers["x-csrf-token"])
        failures.push("Missing secret mutation protection");
      created = {
        ref: "secret_synthetic",
        projectRef,
        version: 1,
        name: "JSON_SYNTHETIC",
        description: "",
        valueType: "JSON",
        currentRevision: 1,
        state: "ACTIVE",
        nextActions: ["ROTATE", "REVOKE"],
        createdAt: "2026-09-05T00:00:00Z",
        updatedAt: "2026-09-05T00:00:00Z",
      };
      await route.fulfill({ status: 201, json: created });
    },
  );
  await page.route(
    "**/api/v1/runtime-secrets/secret_synthetic",
    async (route) => {
      if (route.request().method() !== "GET" || !created) {
        failures.push("Unexpected secret readback");
        await route.fulfill({ status: 404 });
        return;
      }
      await route.fulfill({ json: created });
    },
  );
  await page.goto(`/projects/${projectRef}/secrets`);
  await page
    .getByRole("button", { name: "Создать секрет", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Название", { exact: true }).fill("JSON_SYNTHETIC");
  await dialog.getByLabel("Тип значения", { exact: true }).selectOption("JSON");
  await dialog
    .getByLabel("Секретное значение", { exact: true })
    .fill('{"synthetic":}');
  await dialog
    .getByRole("button", { name: "Создать секрет", exact: true })
    .click();
  expect(creates).toBe(0);
  await expect(dialog.locator(".field-error").last()).toContainText(/1/);
  await dialog
    .getByRole("button", { name: "Показать введённое значение", exact: true })
    .click();
  const editor = dialog.locator(".cm-content");
  await editor.fill('{"synthetic":true}');
  await expect(dialog.locator(".secret-form__value button")).toHaveCount(1);
  await dialog.getByRole("button", { name: "Форматировать JSON" }).click();
  await expect(editor).toContainText('"synthetic": true');
  await dialog
    .getByRole("button", { name: "Создать секрет", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator(".runtime-secrets__name")).toHaveText(
    "JSON_SYNTHETIC",
  );
  expect(creates).toBe(1);
  expect(failures).toEqual([]);
  await expect(page.locator(".cm-content")).toHaveCount(0);
  const row = page.locator(".runtime-secrets__table tbody tr");
  const rowBounds = await row.boundingBox();
  const actionBounds = await row
    .locator(".runtime-secrets__actions")
    .boundingBox();
  expect(rowBounds).not.toBeNull();
  expect(actionBounds).not.toBeNull();
  if (!rowBounds || !actionBounds) throw new Error("Secret row is not visible");
  expect(actionBounds.y + actionBounds.height).toBeLessThanOrEqual(
    rowBounds.y + rowBounds.height,
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  const impact: RuntimeSecretImpact = {
    secretRef: "secret_synthetic",
    secretVersion: 1,
    targetRevision: 1,
    total: 1,
    nextPageToken: "",
    consumers: [
      {
        environmentRef: "environment_synthetic",
        environmentVersion: 19,
        environmentVersionRef: "source_synthetic",
        projectRef,
        secretRevisions: [1],
      },
    ],
  };
  let rebinds = 0;
  await page.route(
    "**/api/v1/runtime-secrets/secret_synthetic/revisions/1/impact*",
    (route) => route.fulfill({ json: impact }),
  );
  await page.route(
    "**/api/v1/runtime-secrets/secret_synthetic/revisions/1/consumer-bindings",
    async (route) => {
      rebinds += 1;
      expect(route.request().method()).toBe("POST");
      expect(route.request().headers()["if-match"]).toBe('"1"');
      const body = route.request().postDataJSON() as RuntimeSecretRebindInput;
      expect(body).toEqual({
        selections: [
          {
            environmentRef: "environment_synthetic",
            expectedEnvironmentVersion: 19,
            sourceVersionRef: "source_synthetic",
            consumers: [],
          },
        ],
      });
      await route.fulfill({
        json: {
          environments: [
            {
              environmentRef: "environment_synthetic",
              environmentVersion: 20,
              projectRef,
              versionRef: "published_synthetic",
              digest: "a".repeat(64),
            },
          ],
          bindings: [],
        },
      });
    },
  );
  await page.locator(".runtime-secrets__name").click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Влияние ревизии", exact: true })
    .click();
  const rebind = page.getByRole("dialog", {
    name: "Перепривязка секрета",
    exact: true,
  });
  await rebind.getByRole("checkbox").check();
  await rebind
    .getByRole("button", { name: "Перепривязать выбранные: 1", exact: true })
    .click();
  await expect(
    rebind.getByRole("heading", { name: "Перепривязка выполнена" }),
  ).toBeVisible();
  await expect(rebind.locator(".impact-receipt")).toContainText(
    "published_synthetic",
  );
  expect(rebinds).toBe(1);
  await rebind
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();
}
