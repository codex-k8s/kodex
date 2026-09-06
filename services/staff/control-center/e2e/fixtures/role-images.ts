import { expect, type Page } from "@playwright/test";
import type {
  RoleImageRecipe,
  RoleImageBuild,
  RoleImageRecipeRevision,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkRoleImageCatalog(page: Page, projectRef: string) {
  await page.route(
    "**/api/v1/administration/access/effective-access/query",
    async (route) => {
      const input = route.request().postDataJSON() as {
        target: { kind: string; projectRef?: string };
        permissionKeys: string[];
        subjectRef?: string;
      };
      if (!input.permissionKeys.includes("image.source.view"))
        return await route.fallback();
      expect(input.target).toEqual({ kind: "PROJECT", projectRef });
      expect(input.subjectRef).toBeUndefined();
      await route.fulfill({
        json: {
          subject: {
            ref: "user_synthetic",
            kind: "USER",
            displayName: "Тестовый владелец",
          },
          evaluatedAt: new Date().toISOString(),
          items: input.permissionKeys.map((permissionKey) => ({
            permissionKey,
            target: input.target,
            decision: "DENIED",
            explanation: [],
          })),
        },
      });
    },
  );
  const recipes: RoleImageRecipe[] = Array.from({ length: 8 }, (_, index) => ({
    sourceAvailable: true,
    ref: `image_synthetic_${String(index)}`,
    projectRef,
    version: 1,
    roleDefinitionRef: "role_synthetic",
    name: `Образ ${String(index + 1)} с длинным названием для выполнения проектных задач`,
    state: "ACTIVE",
    environment: { environmentKey: "standard", dockerfile: "FROM scratch" },
    generation: 1,
    managedLineage: {
      managedBy: "GIT",
      origin: "MANAGED",
      configurationRef: `configuration_synthetic_${String(index)}`,
      revisionRef: `config_revision_${String(index)}`,
      revision: 3,
      sourceRef: "git://synthetic/role-image.yaml",
      sourceRevision: "a".repeat(40),
    },
    promotedImageReady: false,
    nextActions: ["OPEN"],
    createdAt: "2026-09-05T00:00:00Z",
    updatedAt: "2026-09-05T00:00:00Z",
  }));
  await page.route(
    `**/api/v1/projects/${projectRef}/role-image-recipes*`,
    (route) => {
      const params = new URL(route.request().url()).searchParams;
      const query = params.get("query");
      const items =
        params.get("state") === "ARCHIVED"
          ? []
          : query
            ? [recipes[0]]
            : recipes;
      return route.fulfill({
        json: {
          items,
          total: query ? 1 : params.get("state") === "ARCHIVED" ? 0 : 43,
          nextPageToken: "",
        },
      });
    },
  );
  await page.route("**/api/v1/role-environments*", (route) =>
    route.fulfill({ json: { items: [] } }),
  );
  await page.route(`**/api/v1/projects/${projectRef}/agents*`, (route) =>
    route.fulfill({ json: { items: [], nextPageToken: "" } }),
  );
  await page.goto(`/projects/${projectRef}/role-images`);
  await expect(page.locator(".image-card")).toHaveCount(8);
  await expect(
    page.getByRole("link", { name: "Новый образ", exact: true }),
  ).toHaveCount(0);
  await expect(page.locator(".catalog-count")).toHaveText("Всего: 43");
  await expect(
    page
      .locator(".image-card")
      .first()
      .getByText("Источник: GIT", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("searchbox", { name: "Найти образ по имени", exact: true })
    .fill("Образ 1");
  await expect(page.locator(".image-card")).toHaveCount(1);
  await expect(page.locator(".catalog-count")).toHaveText("Всего: 1");
  await page
    .getByRole("searchbox", { name: "Найти образ по имени", exact: true })
    .fill("");
  await page.getByRole("combobox").selectOption("ARCHIVED");
  await expect(page.locator(".catalog-count")).toHaveText("Всего: 0");
  await page.getByRole("combobox").selectOption("ALL");
  await expect(page.locator(".image-card")).toHaveCount(8);
  const cards = await page.locator(".image-card").evaluateAll((elements) =>
    elements.map((element) => {
      const card = element.getBoundingClientRect();
      const footer = element.querySelector("footer")?.getBoundingClientRect();
      return {
        within:
          !!footer &&
          footer.bottom <= card.bottom &&
          footer.right <= card.right,
        overflow: element.scrollWidth > element.clientWidth,
      };
    }),
  );
  expect(cards.every((card) => card.within && !card.overflow)).toBe(true);
  await page
    .getByRole("button", { name: "Развернуть список проекта", exact: true })
    .click();
  await expect(page.getByRole("dialog").locator(".image-card")).toHaveCount(8);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  const recipe = recipes[0];
  if (!recipe) throw new Error("Missing synthetic recipe");
  return recipe;
}

export async function checkRoleImageHistory(
  page: Page,
  recipe: RoleImageRecipe,
) {
  const builds: RoleImageBuild[] = Array.from({ length: 8 }, (_, index) => ({
    sourceAvailable: true,
    ref: `build_synthetic_${String(index)}`,
    version: 1,
    recipeRef: recipe.ref,
    recipeGeneration: 1,
    configurationRevisionRef: recipe.managedLineage?.revisionRef,
    dockerfile: "FROM scratch\nLABEL fixture=synthetic",
    attempt: index + 1,
    stage: "COMPLETED",
    progressPercent: 100,
    createdAt: recipe.createdAt,
    updatedAt: recipe.updatedAt,
  }));
  const revisions: RoleImageRecipeRevision[] = Array.from(
    { length: 8 },
    (_, index) => ({
      ref: `revision_synthetic_${String(index)}`,
      recipeRef: recipe.ref,
      revision: index + 1,
      recipeVersion: 1,
      recipeGeneration: 1,
      specSha256: "a".repeat(64),
      provenanceSha256: "b".repeat(64),
      sourceSha256: "c".repeat(64),
      immutableBuildSha256: "d".repeat(64),
      imageArtifactRef: "artifact_synthetic",
      manifestDigest: `sha256:${"e".repeat(64)}`,
      promotionReceiptSha256: "f".repeat(64),
      createdAt: recipe.createdAt,
    }),
  );
  const endpoint = `**/api/v1/projects/${recipe.projectRef}/role-image-recipes/${recipe.ref}`;
  let sourceAvailable = true;
  await page.route(endpoint, (route) =>
    route.fulfill({
      json: sourceAvailable
        ? { recipe, builds }
        : {
            recipe: {
              ...recipe,
              sourceAvailable: false,
              environment: {
                environmentKey: recipe.environment.environmentKey,
              },
              nextActions: ["OPEN", "REQUEST_BUILD"],
            },
            builds: builds.map((build) => ({
              ...build,
              sourceAvailable: false,
              dockerfile: undefined,
            })),
          },
    }),
  );
  await page.route(`${endpoint}/revisions*`, (route) =>
    route.fulfill({ json: { items: revisions, nextPageToken: "" } }),
  );
  await page.goto(`/projects/${recipe.projectRef}/role-images/${recipe.ref}`);
  await expect(page.locator(".build-row")).toHaveCount(8);
  await expect(page.locator(".revision-row")).toHaveCount(8);
  const histories = page.locator(".build-history");
  await histories
    .first()
    .getByRole("button", { name: "Развернуть список проекта", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.locator(".build-source summary").first().click();
  await expect(dialog.locator(".cm-content")).toHaveAttribute(
    "contenteditable",
    "false",
  );
  await expect(dialog.locator(".voice-input-button")).toHaveCount(0);
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
  await histories
    .last()
    .getByRole("button", { name: "Развернуть список проекта", exact: true })
    .click();
  await expect(dialog.locator(".revision-row")).toHaveCount(8);
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
  await page.evaluate(() => window.scrollTo(0, 0));
  sourceAvailable = false;
  await page.reload();
  await expect(page.locator(".source-unavailable")).toBeVisible();
  await expect(
    page.locator(".dockerfile-editor, .build-source, .code-diff"),
  ).toHaveCount(0);
  await expect(page.locator(".build-row")).toHaveCount(8);
  await expect(
    page.getByRole("button", { name: "Запросить сборку", exact: true }),
  ).toBeEnabled();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
}
