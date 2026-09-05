import { expect, type Page } from "@playwright/test";
import type {
  Agent,
  Artifact,
  ArtifactImpact,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkFileSelection(
  page: Page,
  projectRef: string,
  screenshot: () => Promise<void>,
): Promise<void> {
  const agent: Agent = {
    ref: "agent_unbind",
    version: 2,
    projectRef,
    name: "Агент без доступа к файлам",
    purpose: "Проверка снятия прежней связи",
    roleDescription: "Аналитик",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_unbind",
    runtimeName: "Synthetic",
    runtimeReady: false,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: "2026-09-05T00:00:00Z",
    nextActions: [],
  };
  const artifact: Artifact = {
    ref: "artifact_selection",
    projectRef,
    version: 4,
    revision: 2,
    fileName: "Выбор точной ревизии.txt",
    mediaType: "text/plain",
    sizeBytes: 16,
    digest: "a".repeat(64),
    scanState: "CLEAN",
    source: "CONTROL_CENTER",
    lifecycleState: "ACTIVE",
    agentBindings: [agent.ref],
    previewAvailable: false,
    createdAt: "2026-09-05T00:00:00Z",
    nextActions: ["DELETE", "BIND"],
  };
  let impactCalls = 0;
  let permitted = false;
  await page.route(
    `**/api/v1/projects/${projectRef}/agents*`,
    async (route) => {
      await route.fulfill({ json: { items: [agent] } });
    },
  );
  let bindingCalls = 0;
  await page.route(`**/api/v1/agents/${agent.ref}`, async (route) => {
    await route.fulfill({ json: agent });
  });
  await page.route(
    `**/api/v1/artifacts/${artifact.ref}/bindings`,
    async (route) => {
      expect(route.request().method()).toBe("POST");
      expect(route.request().headers()["if-match"]).toBe('"4"');
      expect(route.request().headers()["idempotency-key"]).toBeTruthy();
      expect(route.request().postDataJSON()).toEqual({
        agentRef: agent.ref,
        enabled: false,
      });
      bindingCalls += 1;
      artifact.version = 5;
      artifact.agentBindings = [];
      await route.fulfill({ json: artifact });
    },
  );
  await page.route(
    `**/api/v1/projects/${projectRef}/artifacts*`,
    async (route) => {
      const query = new URL(route.request().url()).searchParams;
      expect(query.get("lifecycleState")).toBe("ACTIVE");
      await route.fulfill({
        json: {
          items:
            query.get("sourceKind") === "CONTROL_CENTER"
              ? [
                  artifact,
                  {
                    ...artifact,
                    ref: "artifact_no_delete",
                    fileName: "Без удаления.txt",
                    nextActions: [],
                  },
                ]
              : [],
        },
      });
    },
  );
  await page.route(
    `**/api/v1/artifacts/${artifact.ref}/impact*`,
    async (route) => {
      expect(route.request().method()).toBe("GET");
      expect(new URL(route.request().url()).searchParams.get("action")).toBe(
        "DELETE",
      );
      impactCalls += 1;
      await route.fulfill({
        json: {
          artifactRef: artifact.ref,
          artifactVersion: artifact.version,
          action: "DELETE",
          impactDigest: "b".repeat(64),
          bindingCount: 0,
          attachmentCount: 0,
          activeRuntimeCount: 0,
          activeRuns: [],
          activeRunsTruncated: false,
          blockers: permitted ? [] : ["ACTIVE_RUNTIME"],
          permitted,
        } satisfies ArtifactImpact,
      });
    },
  );
  await page.goto(`/projects/${projectRef}/files`);
  const selected = page.locator(
    `[data-artifact-ref="${artifact.ref}"] input[type="checkbox"]`,
  );
  const unavailable = page.locator(
    '[data-artifact-ref="artifact_no_delete"] input[type="checkbox"]',
  );
  await expect(selected).toBeEnabled();
  await expect(unavailable).toBeDisabled();
  await selected.check();
  const name = page.locator(
    `[data-artifact-ref="${artifact.ref}"] .file-list-row__identity strong`,
  );
  if ((page.viewportSize()?.width ?? 1440) < 761) {
    const box = await name.boundingBox();
    expect(box?.width).toBeGreaterThan(100);
    await expect(page.locator(".files-list__head")).toBeHidden();
  }
  await screenshot();
  const toggleBox = await page
    .getByRole("button", { name: "Сетка", exact: true })
    .boundingBox();
  expect(toggleBox).not.toBeNull();
  expect((toggleBox?.x ?? 0) + (toggleBox?.width ?? 0)).toBeLessThanOrEqual(
    page.viewportSize()?.width ?? 0,
  );
  await page.getByRole("button", { name: "Сетка", exact: true }).click();
  await expect(selected).toBeChecked();
  await expect(selected).toBeEnabled();
  await expect(unavailable).toBeDisabled();
  expect(impactCalls).toBe(0);
  await page
    .locator(".selection-toolbar")
    .getByRole("button", { name: "Переместить в корзину", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("button", { name: "Переместить в корзину", exact: true }),
  ).toBeDisabled();
  expect(impactCalls).toBe(1);
  await dialog.getByRole("button", { name: "Отмена", exact: true }).click();
  permitted = true;
  await page
    .locator(".selection-toolbar")
    .getByRole("button", { name: "Переместить в корзину", exact: true })
    .click();
  await expect(
    dialog.getByRole("button", { name: "Переместить в корзину", exact: true }),
  ).toBeEnabled();
  expect(impactCalls).toBe(2);
  await dialog.getByRole("button", { name: "Отмена", exact: true }).click();
  await page.locator(`[data-artifact-ref="${artifact.ref}"]`).click();
  const binding = page
    .locator(".file-details__bindings")
    .getByRole("checkbox", { name: agent.name });
  await expect(binding).toBeChecked();
  await expect(binding).toBeEnabled();
  await binding.uncheck();
  await expect(binding).not.toBeChecked();
  await expect(binding).toBeDisabled();
  expect(bindingCalls).toBe(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
}
