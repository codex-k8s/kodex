import { expect, type Page } from "@playwright/test";
import type {
  RoleImageRecipe,
  RoleImageBuild,
  RoleImageRecipeRevision,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkRoleImageCatalog(page: Page, projectRef: string) {
  const recipes: RoleImageRecipe[] = Array.from({ length: 8 }, (_, index) => ({
    ref: `image_synthetic_${String(index)}`,
    projectRef,
    version: 1,
    roleDefinitionRef: "role_synthetic",
    name: `Образ ${String(index + 1)} с длинным названием для выполнения проектных задач`,
    state: "ACTIVE",
    environment: { environmentKey: "standard", dockerfile: "FROM scratch" },
    generation: 1,
    promotedImageReady: false,
    nextActions: ["OPEN"],
    createdAt: "2026-09-05T00:00:00Z",
    updatedAt: "2026-09-05T00:00:00Z",
  }));
  await page.route(
    `**/api/v1/projects/${projectRef}/role-image-recipes*`,
    (route) => route.fulfill({ json: { items: recipes, nextPageToken: "" } }),
  );
  await page.route("**/api/v1/role-environments*", (route) =>
    route.fulfill({ json: { items: [] } }),
  );
  await page.route(`**/api/v1/projects/${projectRef}/agents*`, (route) =>
    route.fulfill({ json: { items: [], nextPageToken: "" } }),
  );
  await page.goto(`/projects/${projectRef}/role-images`);
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
    ref: `build_synthetic_${String(index)}`,
    version: 1,
    recipeRef: recipe.ref,
    recipeGeneration: 1,
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
  await page.route(endpoint, (route) =>
    route.fulfill({ json: { recipe, builds } }),
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
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
}
