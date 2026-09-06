import { readFileSync } from "node:fs";
import { expect, type Page } from "@playwright/test";
import { load, JSON_SCHEMA } from "js-yaml";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkIntegrationPackage(page: Page): Promise<void> {
  const source = readFileSync(
    new URL(
      "../../../../../contracts/integrations/v1/definitions/github.yaml",
      import.meta.url,
    ),
    "utf8",
  );
  const initial = load(source, { schema: JSON_SCHEMA }) as Record<
    string,
    unknown
  >;
  let configuration: ManagedConfiguration = {
    ref: "package_synthetic",
    kind: "INTEGRATION_DEFINITION",
    name: "GitHub",
    version: 1,
    managedBy: "UI",
    source: "ui",
    sourceRevision: "1",
    updatedAt: "2026-09-04T11:00:00Z",
  };
  let revision: ManagedConfigurationRevision = {
    ref: "package_revision",
    revision: 1,
    content: source,
    contentFormat: "YAML",
    state: "DRAFT",
    digest: "a".repeat(64),
    validationDiagnostics: [],
    createdAt: configuration.updatedAt,
  };
  let saved: Record<string, unknown> | undefined;
  const previous: ManagedConfigurationRevision[] = [];
  await page.route(
    "**/api/v1/managed-configurations/package_synthetic/revisions*",
    async (route) => {
      await route.fulfill({
        json: {
          configuration,
          items: [revision, ...previous],
          total: 1 + previous.length,
        },
      });
    },
  );
  await page.route(
    "**/api/v1/integration-definition-configurations/package_synthetic/revisions/package_revision/saves",
    async (route) => {
      const body = route.request().postDataJSON() as {
        content: string;
        contentFormat: "JSON" | "YAML";
      };
      expect(route.request().headers()["if-match"]).toBe('"1"');
      saved = load(body.content, { schema: JSON_SCHEMA }) as Record<
        string,
        unknown
      >;
      previous.push({ ...revision, state: "DISCARDED" });
      revision = {
        ...revision,
        ref: "package_revision_2",
        revision: 2,
        parentRevisionRef: "package_revision",
        content: body.content,
        contentFormat: body.contentFormat,
      };
      configuration = { ...configuration, version: 2 };
      await route.fulfill({
        headers: { ETag: '"2"' },
        json: { configuration, revision },
      });
    },
  );
  await page.route(
    "**/api/v1/integration-definition-configurations/package_synthetic/revisions/package_revision_2/discard",
    async (route) => {
      expect(route.request().headers()["if-match"]).toBe('"2"');
      revision = { ...revision, state: "DISCARDED" };
      configuration = { ...configuration, version: 3 };
      await route.fulfill({
        headers: { ETag: '"3"' },
        json: { configuration, revision },
      });
    },
  );
  await page.goto("/configurations/INTEGRATION_DEFINITION/package_synthetic");
  const form = page.locator(".configuration-fields");
  await expect(form.getByLabel("Версия API", { exact: true })).toHaveValue(
    "integrations.kodex.io/v1",
  );
  await expect(
    form.getByRole("combobox", { name: "Владелец адаптера", exact: true }),
  ).toHaveValue("integration-gateway");
  await form
    .getByRole("textbox", { name: "Описание", exact: true })
    .fill("Synthetic edited description");
  await page
    .getByRole("combobox", { name: "Формат", exact: true })
    .selectOption("JSON");
  await expect(
    form.getByRole("textbox", { name: "Описание", exact: true }),
  ).toHaveValue("Synthetic edited description");
  await page.getByRole("button", { name: "Изменения", exact: true }).click();
  const diff = page.getByRole("dialog", { name: "Изменения", exact: true });
  await expect(diff.locator(".cm-content")).toContainText(
    "Synthetic edited description",
  );
  await diff.getByRole("button", { name: "Закрыть", exact: true }).click();
  await page
    .getByRole("button", { name: "Сохранить черновик", exact: true })
    .click();
  await expect.poll(() => saved).toBeDefined();
  await expect(
    page.getByRole("button", { name: "Сохранить черновик", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "История", exact: true }).click();
  const history = page.getByRole("dialog", { name: "История", exact: true });
  await expect(history.locator("[data-state='DISCARDED']")).toHaveCount(1);
  await history.getByRole("button", { name: "Закрыть", exact: true }).click();
  expect(saved).toEqual({
    ...initial,
    spec: {
      ...(initial.spec as Record<string, unknown>),
      description: "Synthetic edited description",
    },
  });
  await expect(
    page.getByRole("status", { name: "Проверка схемы" }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Отбросить черновик", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "Отбросить черновик", exact: true })
    .getByRole("button", { name: "Отбросить черновик", exact: true })
    .click();
  await expect(
    page
      .locator(".configuration-editor__toolbar")
      .getByRole("button", { name: "Отбросить черновик", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.locator(".configuration-editor > header [data-state='DISCARDED']"),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}
