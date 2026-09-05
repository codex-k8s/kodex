import { expect, type Page } from "@playwright/test";
import type { Artifact } from "../../src/shared/api/generated/openapi/types.gen";
import { syntheticCatalogRun } from "./catalog-run";
export async function checkHomeResults(
  page: Page,
  projectRef: string,
  capture: () => Promise<void>,
) {
  const cursors: string[] = [];
  let exactReads = 0;
  const artifact = (index: number): Artifact => ({
    ref: `artifact_home_${String(index)}`,
    version: 1,
    revision: 1,
    fileName: `Личный результат ${String(index)}`,
    mediaType: "text/plain",
    sizeBytes: 8,
    digest: "a".repeat(64),
    scanState: "CLEAN",
    lifecycleState: "ACTIVE",
    source: "AGENT_RESULT",
    agentBindings: [],
    previewAvailable: false,
    createdAt: "2026-09-05T00:00:00Z",
    nextActions: [],
  });
  await page.route("**/api/v1/runs?**", async (route) => {
    const params = new URL(route.request().url()).searchParams;
    if (params.get("pageSize") === "100") {
      await route.fulfill({ json: { items: [], total: 0 } });
      return;
    }
    expect(params.get("pageSize")).toBe("30");
    if (params.getAll("states").join(",") === "FAILED") {
      await route.fulfill({ json: { items: [], total: 0 } });
      return;
    }
    expect(params.getAll("states")).toEqual([
      "QUEUED",
      "RUNNING",
      "WAITING_HUMAN",
      "CANCELLING",
    ]);
    const cursor = params.get("pageToken");
    if (cursor) cursors.push(`run:${cursor}`);
    const items = Array.from({ length: cursor ? 1 : 30 }, (_, index) =>
      syntheticCatalogRun(index + (cursor ? 30 : 0), projectRef),
    );
    await route.fulfill({
      json: {
        items,
        total: 31,
        ...(cursor ? {} : { nextPageToken: "runs-next" }),
      },
    });
  });
  await page.route("**/api/v1/artifacts?**", async (route) => {
    const params = new URL(route.request().url()).searchParams;
    expect(params.get("pageSize")).toBe("30");
    expect(params.get("lifecycleState")).toBe("ACTIVE");
    const query = params.get("query");
    const cursor = params.get("pageToken");
    if (cursor) cursors.push(`file:${cursor}`);
    const items = query
      ? [artifact(0)]
      : Array.from({ length: cursor ? 11 : 30 }, (_, index) =>
          artifact(index + (cursor ? 30 : 0)),
        );
    await route.fulfill({
      json: {
        items,
        total: query ? 1 : 41,
        ...(query || cursor ? {} : { nextPageToken: "files-next" }),
      },
    });
  });
  await page.route("**/api/v1/artifacts/artifact_home_0", async (route) => {
    exactReads++;
    await route.fulfill({ json: artifact(0) });
  });
  await page.goto("https://kodex.test/");
  const runCard = page.locator(".home-running-section");
  const fileCard = page.locator('.home-result-catalog[data-kind="ARTIFACT"]');
  await expect(runCard.locator("header > span")).toHaveText("31");
  await expect(fileCard.locator("header > span")).toHaveText("41");
  for (const card of [runCard, fileCard]) {
    const rows = card.locator(".home-result-rows");
    expect(
      await rows.evaluate((element) => element.clientHeight),
    ).toBeLessThanOrEqual(552);
    await rows.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
      element.dispatchEvent(new Event("scroll"));
    });
  }
  await expect
    .poll(() => cursors)
    .toEqual(expect.arrayContaining(["run:runs-next", "file:files-next"]));
  await fileCard
    .getByRole("button", { name: "Развернуть список", exact: true })
    .click();
  const expanded = page.getByRole("dialog").last();
  await expanded.getByRole("searchbox").fill("Личный результат 0");
  await expect(expanded.locator(".home-result-row")).toHaveCount(1);
  await expanded
    .getByRole("button", { name: "Личный результат 0", exact: true })
    .click();
  const detail = page.getByRole("dialog", {
    name: "Личный результат 0",
    exact: true,
  });
  await expect(
    detail.getByRole("button", { name: "Скачать", exact: true }),
  ).toBeDisabled();
  expect(exactReads).toBe(1);
  await detail.getByRole("button", { name: "Закрыть", exact: true }).click();
  await capture();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await expanded.getByRole("button", { name: "Закрыть", exact: true }).click();
}
