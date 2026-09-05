import { expect, type Page } from "@playwright/test";
import type {
  Agent,
  Project,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkOrganizationCatalog(
  page: Page,
  projects: Project[],
  invalidate: () => void,
  capture: () => Promise<void>,
): Promise<void> {
  const first = projects[0];
  const second = projects[1];
  if (!first || !second) throw new Error("Missing synthetic catalog projects");
  let version = 1;
  const requests: Array<{
    project: string | null;
    query: string | null;
    cursor: string | null;
  }> = [];
  const agent = (index: number, projectRef: string): Agent => ({
    ref: `agent_catalog_${String(index)}`,
    version,
    projectRef,
    name: `Сотрудник ${String(index)} ${"ДлинноеНазвание".repeat(10)}`,
    purpose: "Подробное назначение сотрудника ".repeat(12),
    roleDescription: "",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_synthetic",
    runtimeName: "Длинное имя модели ".repeat(10),
    runtimeReady: true,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    nextActions: [],
    updatedAt: "2026-09-05T00:00:00Z",
  });
  await page.route("**/api/v1/agents?**", async (route) => {
    const url = new URL(route.request().url());
    expect(url.searchParams.get("pageSize")).toBe("30");
    const project = url.searchParams.get("projectRef");
    const query = url.searchParams.get("query");
    const cursor = url.searchParams.get("pageToken");
    requests.push({ project, query, cursor });
    if (project) {
      expect(project).toBe(first.ref);
      await route.fulfill({
        json: { items: [agent(0, first.ref)], nextActions: [] },
      });
      return;
    }
    if (cursor) {
      expect(cursor).toBe("catalog_next");
      await route.fulfill({
        json: { items: [agent(8, first.ref)], nextActions: [] },
      });
      return;
    }
    await route.fulfill({
      json: {
        items: [
          ...Array.from({ length: 7 }, (_, i) => agent(i, first.ref)),
          agent(7, second.ref),
        ],
        nextPageToken: "catalog_next",
        nextActions: [],
      },
    });
  });
  await page.goto("/agents");
  const catalog = page.locator(".organization-catalog").first();
  const rows = catalog.locator(".organization-catalog__entry");
  await expect(rows).toHaveCount(8);
  await expect(catalog.locator(".organization-catalog__group")).toHaveCount(2);
  const group = catalog
    .locator(".organization-catalog__group")
    .filter({ has: page.getByRole("link", { name: first.name, exact: true }) });
  const list = group.locator(".organization-catalog__items");
  expect(
    await list.evaluate((element) => element.clientHeight),
  ).toBeLessThanOrEqual(672);
  expect(
    await rows.evaluateAll((elements) =>
      elements.every((element) => {
        const row = element.getBoundingClientRect();
        const copy = element.firstElementChild?.getBoundingClientRect();
        return !!copy && copy.top >= row.top && copy.bottom <= row.bottom;
      }),
    ),
  ).toBe(true);
  await list.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await expect(rows).toHaveCount(9);
  version = 2;
  invalidate();
  await expect(rows).toHaveCount(8);
  await expect(rows.first()).toContainText("v2");
  expect(requests.at(-1)?.cursor).toBeNull();
  await group
    .getByRole("button", { name: "Развернуть список проекта", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: first.name, exact: true });
  await expect(dialog.locator(".organization-catalog__entry")).toHaveCount(1);
  await dialog
    .getByRole("searchbox", { name: "Поиск", exact: true })
    .fill("Сотрудник");
  await expect
    .poll(() => requests.at(-1))
    .toEqual({ project: first.ref, query: "Сотрудник", cursor: null });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await capture();
  await dialog
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();
}
