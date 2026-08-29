import { loadE2EEnvironment } from "./environment";
import { expect, test } from "./fixtures";
import { gotoWithRetry, routeRef } from "./helpers";

const environment = loadE2EEnvironment();
const projectName = `${environment.resourcePrefix} — отдел продаж`;

test("mobile shell, помощник и граф доступны без горизонтального переполнения", async ({
  page,
}) => {
  await gotoWithRetry(page, "/projects");

  const project = page.getByRole("link", { name: new RegExp(projectName) });
  await expect(project).toBeVisible();
  await project.click();
  await expect(page).toHaveURL(/\/projects\/[^/]+$/);
  const projectRef = routeRef(page, "projects");

  const menu = page.getByRole("button", { name: "Меню" });
  await expect(menu).toBeVisible();
  await menu.click();
  await expect(menu).toHaveAttribute("aria-expanded", "true");
  await expect(
    page.getByRole("navigation", { name: "Навигация Проекта" }),
  ).toBeVisible();

  await page.getByRole("button", { name: /^Помощник/ }).first().click();
  await expect(page.getByRole("dialog", { name: "Kodex" })).toBeVisible();
  await expect(
    page.getByLabel("Опишите, что нужно настроить или запустить"),
  ).toBeVisible();

  await gotoWithRetry(page, `/projects/${projectRef}/runs`);
  const run = page.locator(".run-work-item").first();
  await expect(run).toBeVisible();
  await run.click();
  const graph = page.getByRole("tree", { name: "Связи графа" });
  await expect(graph).toBeVisible();
  await expect(page.locator(".graph-viewport")).toBeHidden();
  await expect(graph.getByRole("treeitem").first()).toBeVisible();

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
