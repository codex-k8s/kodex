import { expect, test } from "@playwright/test";

import { loadE2EEnvironment } from "./environment";
import { routeRef } from "./helpers";

const environment = loadE2EEnvironment();
const projectName = `${environment.resourcePrefix} — отдел продаж`;

test("mobile shell, помощник и граф доступны без горизонтального переполнения", async ({
  page,
}) => {
  await page.goto("/projects");

  const project = page.getByRole("link", { name: new RegExp(projectName) });
  await expect(project).toBeVisible();
  await project.click();
  const projectRef = routeRef(page, "projects");

  const menu = page.getByRole("button", { name: "Меню" });
  await expect(menu).toBeVisible();
  await menu.click();
  await expect(menu).toHaveAttribute("aria-expanded", "true");
  await expect(
    page.getByRole("navigation", { name: "Навигация Проекта" }),
  ).toBeVisible();

  await page
    .getByRole("link", { name: "Помощник", exact: true })
    .last()
    .click();
  await expect(
    page.getByRole("heading", { name: "Помощник MatterCodex" }),
  ).toBeVisible();
  await expect(
    page.getByLabel("Опишите, что нужно настроить или запустить"),
  ).toBeVisible();

  await page.goto(`/projects/${projectRef}/runs`);
  const run = page.locator(".entity-row").first();
  await expect(run).toBeVisible();
  await run.click();
  await expect(page.locator(".graph-mobile-list")).toBeVisible();
  await expect(page.locator(".graph-viewport")).toBeHidden();
  await expect(page.locator(".graph-mobile-node").first()).toBeVisible();

  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);

  await page.keyboard.press("Tab");
  const focused = await page.evaluate(
    () => document.activeElement?.tagName ?? "",
  );
  expect(["A", "BUTTON", "INPUT", "SELECT", "TEXTAREA"]).toContain(focused);
});
