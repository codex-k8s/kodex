import { expect, type Page } from "@playwright/test";
import type {
  RuntimeSecret,
  RuntimeSecretImpact,
  RuntimeSecretRebindInput,
  RuntimeSecretDraft,
  RuntimeSecretDraftImpactPlan,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkSecretEditor(
  page: Page,
  projectRef: string,
  screenshotPath?: string,
) {
  const failures: string[] = [];
  let created: RuntimeSecret | undefined;
  let creates = 0;
  let draft: RuntimeSecretDraft = {
    ref: "draft_synthetic",
    version: 1,
    generation: 1,
    projectRef,
    secretRef: "secret_synthetic",
    secretVersion: 1,
    name: "JSON_SYNTHETIC",
    description: "",
    valueType: "JSON",
    state: "DRAFT",
    publishedRevision: 0,
    createdAt: "2026-09-05T00:00:00Z",
    updatedAt: "2026-09-05T00:00:00Z",
    expiresAt: "2026-09-06T00:00:00Z",
  };
  let plan: RuntimeSecretDraftImpactPlan = {
    ref: "plan_synthetic",
    draftRef: draft.ref,
    draftVersion: 2,
    secretRef: draft.secretRef,
    secretVersion: 1,
    sourceRevision: 0,
    digest: "a".repeat(64),
    total: 1,
    expiresAt: draft.expiresAt,
    state: "PREPARED",
  };
  await page.route(
    `**/api/v1/projects/${projectRef}/runtime-secrets*`,
    async (route) => {
      if (route.request().method() === "GET") {
        // Список намеренно отстаёт от подтверждённой мутации.
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      failures.push("Immediate secret creation must not be called");
      await route.fulfill({ status: 405 });
    },
  );
  await page.route(
    `**/api/v1/projects/${projectRef}/runtime-secret-drafts`,
    async (route) => {
      creates += 1;
      const headers = route.request().headers();
      if (!headers["idempotency-key"] || !headers["x-csrf-token"])
        failures.push("Missing secret mutation protection");
      await route.fulfill({ status: 201, json: draft });
    },
  );
  await page.route("**/api/v1/runtime-secret-drafts/draft_synthetic", (route) =>
    route.fulfill({ json: draft }),
  );
  await page.route(
    "**/api/v1/runtime-secret-drafts/draft_synthetic/validate",
    async (route) => {
      expect(route.request().headers()["if-match"]).toBe('"1"');
      draft = { ...draft, version: 2, state: "VALID" };
      await route.fulfill({ json: draft });
    },
  );
  await page.route(
    "**/api/v1/runtime-secret-drafts/draft_synthetic/impact-plans",
    async (route) => {
      expect(route.request().headers()["if-match"]).toBe('"2"');
      await route.fulfill({ status: 201, json: plan });
    },
  );
  await page.route(
    "**/api/v1/runtime-secret-draft-impact-plans/plan_synthetic*",
    (route) =>
      route.fulfill({
        json: {
          plan,
          total: 1,
          nextPageToken: "",
          items: [
            {
              ref: "item_synthetic",
              consumer: {
                environmentRef: "environment_synthetic",
                environmentVersion: 19,
                environmentVersionRef: "source_synthetic",
                projectRef,
                secretRevisions: [],
              },
              outcome: plan.state === "APPLIED" ? "APPLIED" : "PENDING",
              ...(plan.state === "APPLIED"
                ? { resultEnvironmentVersionRef: "new_environment_synthetic" }
                : {}),
            },
          ],
        },
      }),
  );
  await page.route(
    "**/api/v1/runtime-secret-drafts/draft_synthetic/publish",
    async (route) => {
      expect(route.request().headers()["if-match"]).toBe('"2"');
      expect(route.request().postDataJSON()).toEqual({
        expectedSecretVersion: 1,
        impactPlanRef: "plan_synthetic",
        selectedItemRefs: ["item_synthetic"],
      });
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
      draft = {
        ...draft,
        version: 3,
        state: "PUBLISHED",
        publishedRevision: 1,
      };
      plan = { ...plan, state: "APPLIED" };
      await route.fulfill({ json: { draft, secret: created } });
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
    .getByRole("button", { name: "Сохранить черновик", exact: true })
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
    .getByRole("button", { name: "Сохранить черновик", exact: true })
    .click();
  await expect(
    dialog.getByText("draft_synthetic", { exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/draftRef=draft_synthetic/);
  await expect(dialog.locator(".cm-content")).toHaveCount(0);
  await dialog
    .getByRole("button", { name: "Проверить черновик", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Подготовить план влияния", exact: true })
    .click();
  await expect(dialog.getByRole("checkbox")).toBeChecked();
  const draftBounds = await dialog.boundingBox();
  expect(draftBounds).not.toBeNull();
  if (!draftBounds) throw new Error("Secret draft is not visible");
  expect(draftBounds.x).toBeGreaterThanOrEqual(0);
  expect(draftBounds.x + draftBounds.width).toBeLessThanOrEqual(
    page.viewportSize()?.width ?? 0,
  );
  if (screenshotPath)
    await page.screenshot({ path: screenshotPath, fullPage: true });
  await dialog
    .getByRole("button", {
      name: "Опубликовать и применить выбранное",
      exact: true,
    })
    .click();
  await expect(dialog).toContainText("new_environment_synthetic");
  await expect(page).toHaveURL(/planRef=plan_synthetic/);
  await page.reload();
  await expect(dialog).toContainText("new_environment_synthetic");
  await expect(dialog).toContainText("PUBLISHED");
  await dialog
    .getByRole("button", { name: "Закрыть", exact: true })
    .first()
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
