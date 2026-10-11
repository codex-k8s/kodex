import { expect, type Page } from "@playwright/test";
import type { Run } from "../../src/shared/api/generated/openapi/types.gen";
import { syntheticCatalogRun } from "./catalog-run";
import type { PlatformSnapshotEnvelope } from "../../src/shared/api/generated/asyncapi/PlatformSnapshotEnvelope";

export type SyntheticSnapshotReader = (
  projectRef?: string,
) => PlatformSnapshotEnvelope["snapshot"] | undefined;
export type SyntheticSnapshotInstaller = (
  kind: PlatformSnapshotEnvelope["kind"],
  read: SyntheticSnapshotReader,
) => void;

export function scopedSyntheticSnapshot(
  projectRef: string,
  read: () => PlatformSnapshotEnvelope["snapshot"],
): SyntheticSnapshotReader {
  return (scope) =>
    scope === projectRef ? structuredClone(read()) : undefined;
}

export function assertSyntheticAdaptivePageSize(value: string | null): void {
  expect(value).toMatch(/^[1-9]\d*$/);
  const size = Number(value);
  expect(size).toBeGreaterThanOrEqual(8);
  expect(size).toBeLessThanOrEqual(100);
}
export async function checkRunsCatalog(
  page: Page,
  projectRef: string,
  capture: () => Promise<void>,
  invalidate: (runs: Run[]) => void,
  installSnapshot?: SyntheticSnapshotInstaller,
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
    if (params.get("pageSize") === "6") {
      expect(params.getAll("states")).toEqual([]);
      expect(params.get("pageToken")).toBeNull();
      await route.fulfill({
        json: {
          items: [
            ...(newRun ? [run(99)] : []),
            ...Array.from({ length: 6 }, (_, index) => run(index)),
          ].slice(0, 6),
          nextPageToken: "",
        },
      });
      return;
    }
    assertSyntheticAdaptivePageSize(params.get("pageSize"));
    const query = params.get("query") ?? "";
    const cursor = params.get("pageToken");
    const states = params.getAll("states");
    queries.push({ query, cursor, states });
    await route.fulfill({
      json: {
        items: query
          ? []
          : states.includes("FAILED")
            ? [{ ...run(20), state: "FAILED" }]
            : query || !states.includes("QUEUED")
              ? []
              : cursor
                ? [run(9)]
                : [
                    ...Array.from({ length: 8 }, (_, index) => run(index)),
                    ...(newRun ? [run(99)] : []),
                  ],
        nextPageToken:
          !states.includes("QUEUED") || query || cursor ? "" : "runs_next",
      },
    });
  });
  installSnapshot?.(
    "RUN",
    scopedSyntheticSnapshot(projectRef, () => ({
      catalog: {
        runs: [
          ...Array.from({ length: 8 }, (_, index) => run(index)),
          { ...run(20), state: "FAILED" },
          ...(newRun ? [run(99)] : []),
        ],
        page: { nextPageToken: "runs_next" },
        gates: [],
        gatesPage: {},
      },
    })),
  );
  await page.goto(`/projects/${projectRef}/runs`);
  const board = page.locator(".runs-board");
  const lane = board;
  const rows = board.locator(".run-table__row");
  const queuedRows = board
    .locator("tbody")
    .filter({
      has: page.locator(".runs-board__group", { hasText: "В очереди" }),
    })
    .locator(".run-table__row");
  // Текущий каталог — единая таблица со строками групп и одним scrollport.
  // Sentinel короткой страницы может догрузить хвост ещё до ручного scroll.
  await expect(rows.nth(7)).toBeVisible();
  expect(
    await lane.evaluate(
      (element) => getComputedStyle(element).overflowY === "auto",
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
  await expect(rows).toHaveCount(10);
  await expect(queuedRows.locator(".runs-board__identity a")).toHaveText([
    ...Array.from({ length: 8 }, (_, index) => run(index).title),
    run(9).title,
  ]);
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
    await rows.locator(".runs-board__identity a").first().getAttribute("title"),
  ).toBe(run(0).title);
  await capture();
  // После полной догрузки тот же producer подтверждает полный состав без
  // continuation cursor. Переключение фильтра не требует HTTP reread.
  invalidate([
    ...Array.from({ length: 8 }, (_, index) => run(index)),
    run(9),
    { ...run(20), state: "FAILED" },
  ]);
  await expect(rows).toHaveCount(10);
  const readsBeforeFilter = queries.length;
  await page.getByRole("button", { name: "Завершённые", exact: true }).click();
  await expect(rows).toHaveCount(1);
  await expect(rows.locator(".runs-board__identity a")).toHaveText([
    run(20).title,
  ]);
  expect(queries).toHaveLength(readsBeforeFilter);
  await page.getByRole("button", { name: "Активные", exact: true }).click();
  await expect(rows).toHaveCount(9);
  expect(queries).toHaveLength(readsBeforeFilter);
  newRun = true;
  invalidate([
    ...Array.from({ length: 8 }, (_, index) => run(index)),
    run(9),
    { ...run(20), state: "FAILED" },
    run(99),
  ]);
  await expect(rows).toHaveCount(10);
  await expect(rows.locator(".runs-board__identity a")).toHaveText([
    ...Array.from({ length: 8 }, (_, index) => run(index).title),
    run(9).title,
    run(99).title,
  ]);
  expect(queries).toHaveLength(readsBeforeFilter);
  await page
    .getByRole("searchbox", {
      name: "Найти по названию, заданию, цели или инициатору",
      exact: true,
    })
    .fill("No synthetic match");
  await expect
    .poll(() =>
      queries.some(
        (entry) =>
          entry.query === "No synthetic match" && entry.cursor === null,
      ),
    )
    .toBe(true);
  await expect(rows).toHaveCount(0);
}
