import { expect, type Page } from "@playwright/test";
import type { Run } from "../../src/shared/api/generated/openapi/types.gen";
import { syntheticCatalogRun } from "./catalog-run";
export async function checkRunsCatalog(
  page: Page,
  projectRef: string,
  capture: () => Promise<void>,
  invalidate: () => void,
): Promise<void> {
  let newRun = false;
  function run(index: number): Run {
    return syntheticCatalogRun(index, projectRef);
  }
  const queries: { query: string; cursor: string | null; states: string[] }[] =
    [];
  await page.route("**/api/v1/runs?**", async (route) => {
    const params = new URL(route.request().url()).searchParams;
    expect(params.get("projectRef")).toBe(projectRef);
    if (params.get("pageSize") === "100") {
      expect(params.getAll("states")).toEqual([]);
      expect(params.get("pageToken")).toBeNull();
      await route.fulfill({
        json: {
          items: [
            ...Array.from({ length: 8 }, (_, index) => run(index)),
            ...(newRun ? [run(99)] : []),
          ],
          nextPageToken: "",
        },
      });
      return;
    }
    expect(params.get("pageSize")).toBe("40");
    const query = params.get("query") ?? "";
    const cursor = params.get("pageToken");
    const states = params.getAll("states");
    queries.push({ query, cursor, states });
    await route.fulfill({
      json: {
        items: states.includes("FAILED")
          ? [{ ...run(20), state: "FAILED" }]
          : query
            ? []
            : cursor
              ? [run(9)]
              : [
                  ...Array.from({ length: 8 }, (_, index) => run(index)),
                  ...(newRun ? [run(99)] : []),
                ],
        nextPageToken:
          states.includes("FAILED") || query || cursor ? "" : "runs_next",
      },
    });
  });
  await page.goto(`/projects/${projectRef}/runs`);
  const board = page.locator(".runs-board");
  const lane = board.locator(".runs-lane__body").first();
  await expect(lane.locator(".run-work-item")).toHaveCount(8);
  expect(
    await lane.evaluate(
      (element) => element.scrollHeight > element.clientHeight,
    ),
  ).toBe(true);
  const height = await lane.evaluate(
    (element) => element.getBoundingClientRect().height,
  );
  expect(height).toBeLessThanOrEqual(1256);
  await lane.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await expect
    .poll(() => queries.some((entry) => entry.cursor === "runs_next"))
    .toBe(true);
  await expect(lane.locator(".run-work-item")).toHaveCount(9);
  await lane.evaluate((element) => {
    element.scrollTop = 0;
  });
  await page.evaluate(() => window.scrollTo(0, 0));
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  expect(
    await lane.locator(".run-work-item h3").first().getAttribute("title"),
  ).toBe(run(0).title);
  await capture();
  await page.getByRole("button", { name: "Завершённые", exact: true }).click();
  await expect
    .poll(() =>
      queries.some(
        (entry) => entry.states.includes("FAILED") && entry.cursor === null,
      ),
    )
    .toBe(true);
  await expect(page.locator(".run-work-item")).toHaveCount(1);
  await page.getByRole("button", { name: "Активные", exact: true }).click();
  await expect
    .poll(() =>
      queries.some(
        (entry) => entry.states.includes("CANCELLING") && entry.cursor === null,
      ),
    )
    .toBe(true);
  await expect(lane.locator(".run-work-item")).toHaveCount(8);
  const filteredReads = queries.filter((entry) =>
    entry.states.includes("CANCELLING"),
  ).length;
  newRun = true;
  invalidate();
  await expect
    .poll(
      () =>
        queries.filter((entry) => entry.states.includes("CANCELLING")).length,
    )
    .toBeGreaterThan(filteredReads);
  await expect(lane.locator(".run-work-item")).toHaveCount(9);
  await page
    .getByRole("textbox", { name: "Поиск запусков", exact: true })
    .fill("No synthetic match");
  await expect
    .poll(() =>
      queries.some(
        (entry) =>
          entry.query === "No synthetic match" && entry.cursor === null,
      ),
    )
    .toBe(true);
  await expect(page.locator(".run-work-item")).toHaveCount(0);
}
