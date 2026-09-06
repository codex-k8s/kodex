import { expect, type Page } from "@playwright/test";
import type {
  Agent,
  Artifact,
  ArtifactImpact,
  ArtifactBindingTarget,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkFileSelection(
  page: Page,
  projectRef: string,
  screenshot: (name?: string) => Promise<void>,
): Promise<void> {
  const agent: Agent = {
    ref: "agent_unbind",
    version: 2,
    projectRef,
    name: "Архивный сотрудник без доступа к файлам",
    purpose: "Проверка снятия прежней связи",
    roleDescription: "Аналитик",
    state: "ARCHIVED",
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
  let agentReadCalls = 0;
  await page.route(`**/api/v1/agents/${agent.ref}`, async (route) => {
    agentReadCalls++;
    await route.fulfill({
      status: 404,
      json: { status: 404, code: "NOT_FOUND" },
    });
  });
  const configured: ArtifactBindingTarget = {
    agentRef: "agent_configured",
    agentVersion: 9,
    name: "Настроен без runtime",
    state: "DISABLED",
    bound: false,
    canBind: true,
    canUnbind: false,
    bindReason: "AVAILABLE",
    unbindReason: "NOT_BOUND",
  };
  const bindingQueries: string[] = [];
  let bindingSnapshotChanged = false;
  let bindingCursorCalls = 0;
  await page.route(
    `**/api/v1/artifacts/${artifact.ref}/binding-targets*`,
    async (route) => {
      const query = new URL(route.request().url()).searchParams;
      const text = query.get("query") ?? "";
      bindingQueries.push(text);
      expect(query.get("pageSize")).toBe("30");
      if (query.get("pageToken")) {
        expect(query.get("pageToken")).toBe("binding-next");
        bindingCursorCalls++;
        bindingSnapshotChanged = true;
        await route.fulfill({
          json: {
            artifactRef: artifact.ref,
            artifactVersion: artifact.version,
            projectRef,
            items: [
              {
                ...configured,
                agentRef: "agent_stale",
                name: "Строка старого snapshot",
              },
            ],
            total: 3,
            nextPageToken: "",
            digest: "f".repeat(64),
            evaluatedAt: "2026-09-05T12:00:01Z",
          },
        });
        return;
      }
      const bound: ArtifactBindingTarget = {
        agentRef: agent.ref,
        agentVersion: agent.version,
        name: agent.name,
        state: "ARCHIVED",
        bound: true,
        canBind: false,
        canUnbind: true,
        bindReason: "AGENT_ARCHIVED",
        unbindReason: "AVAILABLE",
      };
      const targets = text
        ? [configured]
        : [...(artifact.agentBindings.length ? [bound] : []), configured];
      await route.fulfill({
        json: {
          artifactRef: artifact.ref,
          artifactVersion: artifact.version,
          projectRef,
          items: targets,
          total: bindingSnapshotChanged ? targets.length : 3,
          nextPageToken: bindingSnapshotChanged ? "" : "binding-next",
          digest: bindingSnapshotChanged
            ? "f".repeat(64)
            : String(artifact.version).repeat(64),
          evaluatedAt: "2026-09-05T12:00:00Z",
        },
      });
    },
  );
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
      expect(query.get("sourceKind")).toBeNull();
      expect(query.getAll("sourceKinds")).toEqual([
        "CONTROL_CENTER",
        "INTERACTION_ATTACHMENT",
      ]);
      if (query.get("pageToken")) {
        expect(query.get("pageToken")).toBe("files-next");
        await route.fulfill({
          json: {
            items: Array.from({ length: 5 }, (_, index) => ({
              ...artifact,
              ref: `artifact_extra_${String(index)}`,
              fileName: `Дополнительный файл ${String(index)}.txt`,
              nextActions: [],
            })),
            total: 7,
            nextPageToken: "",
          },
        });
        return;
      }
      await route.fulfill({
        json: {
          items: query.getAll("sourceKinds").includes("CONTROL_CENTER")
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
          total: 7,
          nextPageToken: "files-next",
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
  await expect(page.locator(".files-workspace__toolbar")).toContainText("из 7");
  await expect(page.locator(".files-workspace__count")).toBeVisible();
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
  await expect(
    page
      .locator(".file-details__bindings")
      .getByText("Видимых сотрудников: 3", { exact: true }),
  ).toBeVisible();
  await page
    .locator(".file-details__bindings")
    .getByRole("button", { name: "Загрузить ещё", exact: true })
    .click();
  await expect(
    page
      .locator(".file-details__bindings")
      .getByText("Видимых сотрудников: 2", { exact: true }),
  ).toBeVisible();
  expect(bindingCursorCalls).toBe(1);
  await expect(
    page.getByText("Строка старого snapshot", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page
      .locator(".file-details__bindings")
      .getByRole("checkbox", { name: configured.name }),
  ).toBeEnabled();
  await binding.uncheck();
  await expect(binding).toHaveCount(0);
  expect(bindingCalls).toBe(1);
  expect(agentReadCalls).toBe(0);
  await page
    .locator(".file-details__bindings")
    .getByRole("searchbox")
    .fill("Настроен");
  await expect.poll(() => bindingQueries.at(-1)).toBe("Настроен");
  await page
    .locator(".file-details__bindings")
    .getByRole("button", { name: "Развернуть список", exact: true })
    .click();
  const targetsDialog = page.getByRole("dialog", {
    name: "Доступ сотрудников",
    exact: true,
  });
  await expect(
    targetsDialog.getByRole("checkbox", { name: configured.name }),
  ).toBeEnabled();
  await screenshot("file-binding-targets");
  await targetsDialog
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
}
