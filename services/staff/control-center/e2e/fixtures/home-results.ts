import { expect, type Page } from "@playwright/test";
import type { Artifact } from "../../src/shared/api/generated/openapi/types.gen";
import { syntheticCatalogRun } from "./catalog-run";
import {
  assertSyntheticAdaptivePageSize,
  type SyntheticSnapshotInstaller,
} from "./runs-catalog";

export function homeResultArtifacts(projectRef: string): Artifact[] {
  return Array.from({ length: 41 }, (_, index) => ({
    ref: `artifact_home_${String(index)}`,
    currentRevisionRef: `arv_fixture_home_${String(index)}`,
    ...(index ? { projectRef } : {}),
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
  }));
}

export async function checkHomeResults(
  page: Page,
  projectRef: string,
  capture: () => Promise<void>,
  installSnapshot: SyntheticSnapshotInstaller,
) {
  const cursors: string[] = [];
  let exactReads = 0;
  const artifacts = homeResultArtifacts(projectRef);
  const runs = Array.from({ length: 31 }, (_, index) =>
    syntheticCatalogRun(index, projectRef),
  );
  const artifact = artifacts[0];
  const projectArtifacts = artifacts.filter(
    (item) => item.projectRef === projectRef,
  );
  const projectArtifact = projectArtifacts[0];
  if (!artifact) throw new Error("Missing owned synthetic artifact");
  if (!projectArtifact) throw new Error("Missing project synthetic artifact");
  // Home показывает загруженный префикс; полные страницы принадлежат каталогам.
  installSnapshot("RUN", (scope) =>
    scope === undefined || scope === projectRef
      ? structuredClone({
          catalog: {
            runs: runs.slice(0, 6),
            page: { nextPageToken: "runs-next" },
            gates: [],
            gatesPage: {},
          },
        })
      : undefined,
  );
  installSnapshot("ARTIFACT", (scope) =>
    scope === undefined || scope === projectRef
      ? structuredClone({
          catalog: {
            artifacts: (scope === undefined
              ? artifacts
              : projectArtifacts
            ).slice(0, 6),
            page: { nextPageToken: "files-next" },
          },
        })
      : undefined,
  );
  await page.route("**/api/v1/runs?**", async (route) => {
    const query = new URL(route.request().url()).searchParams;
    assertSyntheticAdaptivePageSize(query.get("pageSize"));
    expect(query.get("projectRef")).toBeNull();
    expect(query.getAll("states")).toEqual(["QUEUED"]);
    expect(query.get("query") ?? "").toBe("");
    const cursor = query.get("pageToken");
    if (cursor) {
      expect(cursor).toBe("runs-next");
      cursors.push(`run:${cursor}`);
    }
    await route.fulfill({
      json: {
        items: cursor ? runs.slice(30) : runs.slice(0, 30),
        total: 31,
        nextPageToken: cursor ? "" : "runs-next",
      },
    });
  });
  await page.route(
    `**/api/v1/projects/${projectRef}/artifacts?**`,
    async (route) => {
      const query = new URL(route.request().url()).searchParams;
      const size = Number(query.get("pageSize"));
      expect(Number.isSafeInteger(size)).toBe(true);
      expect(size).toBeGreaterThanOrEqual(6);
      expect(size).toBeLessThanOrEqual(100);
      expect(query.get("lifecycleState")).toBe("ACTIVE");
      const text = query.get("query") ?? "";
      if (text) expect(text).toBe(projectArtifact.fileName);
      expect(query.getAll("sourceKinds")).toEqual(
        text ? ["AGENT_RESULT", "INTEGRATION_RESULT"] : [],
      );
      const cursor = query.get("pageToken");
      if (cursor) {
        expect(cursor).toBe("files-next");
        cursors.push(`file:${cursor}`);
      }
      await route.fulfill({
        json: {
          items: text
            ? projectArtifacts.filter((item) => item.fileName === text)
            : cursor
              ? projectArtifacts.slice(6)
              : projectArtifacts.slice(0, 6),
          total: text ? 1 : projectArtifacts.length,
          nextPageToken: text || cursor ? "" : "files-next",
        },
      });
    },
  );
  await page.route(`**/api/v1/artifacts/${artifact.ref}`, async (route) => {
    exactReads++;
    await route.fulfill({ json: artifact });
  });
  await page.goto("/");
  const runCard = page.locator(".home-running-section");
  const fileCard = page.locator('.home-result-catalog[data-kind="ARTIFACT"]');
  await expect(runCard.locator("header > span")).toHaveText("6");
  await expect(fileCard.locator("header > span")).toHaveText("6");
  for (const card of [runCard, fileCard]) {
    const rows = card.locator(".home-result-rows");
    expect(
      await rows.evaluate((element) => element.clientHeight),
    ).toBeLessThanOrEqual(552);
    await rows.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
      element.dispatchEvent(new Event("scroll"));
    });
    await expect(card.locator(".home-result-row")).toHaveCount(6);
  }
  expect(cursors).toEqual([]);
  await expect(
    fileCard.getByRole("link", { name: "Все файлы", exact: true }),
  ).toHaveAttribute("href", "/files");
  await runCard.getByRole("link", { name: "Все запуски", exact: true }).click();
  await expect(page.locator(".run-table__row")).toHaveCount(30);
  await page.locator(".runs-board").evaluate((element) => {
    element.scrollTop = element.scrollHeight;
    element.dispatchEvent(new Event("scroll"));
  });
  await expect(page.locator(".run-table__row")).toHaveCount(31);
  await page.goto("/");
  await expect(fileCard.locator("header > span")).toHaveText("6");
  await fileCard
    .getByRole("button", { name: artifact.fileName, exact: true })
    .click();
  const detail = page.getByRole("dialog", {
    name: artifact.fileName,
    exact: true,
  });
  await expect(
    detail.getByRole("button", { name: "Скачать", exact: true }),
  ).toBeDisabled();
  expect(exactReads).toBe(1);
  await detail.getByRole("button", { name: "Закрыть", exact: true }).click();
  await capture();
  await page.goto(`/projects/${projectRef}/files`);
  await page
    .getByRole("combobox", { name: "Раздел", exact: true })
    .selectOption("RESULTS");
  await expect(page.locator("[data-artifact-ref]")).toHaveCount(6);
  await page.locator(".files-workspace__scroll").evaluate((element) => {
    element.scrollTop = element.scrollHeight;
    element.dispatchEvent(new Event("scroll"));
  });
  // EOF может уже удалить sentinel; прокручиваем имеющийся конец списка.
  await page.locator(".files-workspace__scroll").evaluate((element) => {
    element.lastElementChild?.scrollIntoView({ block: "end" });
  });
  await expect(page.locator("[data-artifact-ref]")).toHaveCount(
    projectArtifacts.length,
  );
  const files = page.locator(".files-workspace");
  await files.locator('input[type="search"]').fill(projectArtifact.fileName);
  await expect(page.locator("[data-artifact-ref]")).toHaveCount(1);
  expect(cursors).toEqual(
    expect.arrayContaining(["run:runs-next", "file:files-next"]),
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}
