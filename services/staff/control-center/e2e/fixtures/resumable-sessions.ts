import { expect, type Page } from "@playwright/test";
import type { Agent } from "../../src/shared/api/generated/openapi/types.gen";
import { syntheticCatalogRun } from "./catalog-run";

export async function checkResumableSessions(
  page: Page,
  projectRef: string,
  capture: () => Promise<void>,
) {
  let stale = false;
  let targetReads = 0;
  const agent: Agent = {
    ref: "agent_session_catalog",
    version: 1,
    projectRef,
    name: "Сотрудник продолжения",
    purpose: "Проверка Session",
    roleDescription: "Аналитик",
    state: "READY",
    enabled: true,
    system: false,
    runtimeReady: true,
    runtimeRef: "runtime_synthetic",
    runtimeName: "Synthetic",
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: "2026-09-05T00:00:00Z",
    nextActions: ["OPEN", "LAUNCH"],
  };
  const run = (index: number) => ({
    ...syntheticCatalogRun(index, projectRef),
    title: `Продолжение ${String(index)}`,
    state: "SUCCEEDED" as const,
    nextActions: ["OPEN", "ADD_TURN"] as const,
    target: {
      type: "AGENT" as const,
      ref: agent.ref,
      displayName: agent.name,
      version: 1,
    },
  });
  await page.route("**/api/v1/runs?**", async (route) => {
    const params = new URL(route.request().url()).searchParams;
    if (params.get("resumableSessionsOnly") !== "true") return route.fallback();
    expect(params.getAll("states")).toEqual([]);
    expect(params.get("pageSize")).toBe("30");
    if (params.get("targetRef")) {
      expect(params.get("projectRef")).toBe(projectRef);
      expect(params.get("targetType")).toBe("AGENT");
      expect(params.get("targetRef")).toBe(agent.ref);
      targetReads++;
      await route.fulfill({ json: { items: [run(70)], total: 1 } });
      return;
    }
    if (params.get("pageToken")) {
      expect(params.get("pageToken")).toBe("session-snapshot");
      stale = true;
      await route.fulfill({
        status: 412,
        json: {
          status: 412,
          code: "VERSION_OR_STATE_CONFLICT",
          retryable: true,
        },
      });
      return;
    }
    await route.fulfill({
      json: {
        items: stale
          ? [run(50)]
          : Array.from({ length: 30 }, (_, index) => run(index)),
        total: stale ? 1 : 43,
        ...(stale ? {} : { nextPageToken: "session-snapshot" }),
      },
    });
  });
  await page.goto("/");
  const card = page.locator('.home-result-catalog[data-kind="SESSION"]');
  await expect(card.locator("header > span")).toHaveText("43");
  expect(
    await card
      .locator(".home-result-rows")
      .evaluate((element) => element.clientHeight),
  ).toBeLessThanOrEqual(552);
  await card.locator(".home-result-rows").evaluate((element) => {
    element.scrollTop = element.scrollHeight;
    element.dispatchEvent(new Event("scroll"));
  });
  await expect(card.locator("header > span")).toHaveText("1");
  await expect(card.locator(".home-result-row")).toHaveCount(1);
  await expect(
    card.getByRole("link", { name: "Продолжение 50", exact: true }),
  ).toBeVisible();
  await card
    .getByRole("button", { name: "Развернуть список", exact: true })
    .click();
  await expect(
    page
      .getByRole("dialog", { name: "Продолжить сессию", exact: true })
      .locator(".home-result-row"),
  ).toHaveCount(1);
  await page
    .getByRole("dialog")
    .last()
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
  await page.route(`**/api/v1/agents/${agent.ref}`, (route) =>
    route.fulfill({ json: agent }),
  );
  await page.goto(
    `/projects/${projectRef}/runs/new?targetType=AGENT&targetRef=${agent.ref}`,
  );
  const continuation = page.locator(
    'input[name="new-run-session-policy"][value="CONTINUE"]',
  );
  await expect(continuation).toBeEnabled();
  await continuation.check();
  await expect(page.locator(".new-run-session-picker__overlay")).toBeVisible();
  await expect(
    page
      .locator(".new-run-session-picker__overlay")
      .getByText("Продолжение 70", { exact: true }),
  ).toBeVisible();
  expect(targetReads).toBeGreaterThan(0);
  await capture();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page
    .locator(".new-run-session-picker__overlay")
    .getByText("Продолжение 70", { exact: true })
    .click();
  await expect(page.locator("#new-run-session-picker-trigger")).toContainText(
    "Продолжение 70",
  );
}
