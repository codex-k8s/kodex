import { expect, test, type Page } from "@playwright/test";

import type {
  ProjectPage,
  RevisionImpactPlan,
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentPage,
  RuntimeEnvironmentSet,
} from "../src/shared/api/generated/openapi/types.gen";
import { authenticateOwner } from "./auth-flow";
import { loadE2EAuthEnvironment } from "./environment";
import { gotoWithRetry } from "./helpers";

const environment = loadE2EAuthEnvironment();

test("MVP-UI-47 выбирает consumers по умолчанию и отменяет publication", async ({
  page,
}) => {
  const serverFailures: string[] = [];
  const browserFailures: string[] = [];
  let publicationRequests = 0;
  let draftRef = "";
  let discarded = false;
  page.on("response", (response) => {
    if (response.status() >= 500)
      serverFailures.push(
        `${String(response.status())} ${new URL(response.url()).pathname}`,
      );
  });
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname.endsWith("/publication")
    )
      publicationRequests += 1;
  });

  await authenticateOwner(
    page,
    {
      username: environment.ownerUsername,
      password: environment.ownerPassword,
    },
    { mode: "local" },
  );
  page.on("pageerror", (error) => browserFailures.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") browserFailures.push(message.text());
  });
  const target = await environmentWithConsumer(page);
  const before = await runtimeEnvironment(page, target.environmentRef);

  try {
    await gotoWithRetry(
      page,
      `/projects/${encodeURIComponent(target.projectRef)}/environments/${encodeURIComponent(target.environmentRef)}`,
    );
    const description = page.getByLabel("Описание", { exact: true });
    await expect(description).toBeEnabled();
    await description.fill(
      `${await description.inputValue()} Проверка impact plan.`,
    );

    const createResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname.endsWith(
          "/runtime-environment-drafts",
        ),
    );
    await page
      .getByRole("button", { name: "Сохранить черновик", exact: true })
      .click();
    const createdResponse = await createResponse;
    expect(createdResponse.status()).toBe(201);
    const created = (await createdResponse.json()) as RuntimeEnvironmentDraft;
    expect(created.state).toBe("DRAFT");
    expect(created.environmentRef).toBe(target.environmentRef);
    draftRef = created.ref;

    const validationResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname.endsWith("/validation"),
    );
    await page.getByRole("button", { name: "Проверить", exact: true }).click();
    const validatedResponse = await validationResponse;
    expect(validatedResponse.status()).toBe(200);
    const validated =
      (await validatedResponse.json()) as RuntimeEnvironmentDraft;
    expect(validated.state).toBe("VALID");
    expect(validated.ref).toBe(draftRef);

    const impactResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname.endsWith("/impact-plans"),
    );
    await page
      .getByRole("button", { name: "Опубликовать", exact: true })
      .click();
    const preparedResponse = await impactResponse;
    expect(preparedResponse.status()).toBe(201);
    const plan = (await preparedResponse.json()) as RevisionImpactPlan;
    expect(plan).toMatchObject({
      kind: "RUNTIME_ENVIRONMENT",
      state: "PREPARED",
      draftRef,
    });

    const dialog = page.getByRole("dialog", { name: "План публикации" });
    await expect(dialog).toBeVisible();
    const checkboxes = dialog.locator('input[type="checkbox"]');
    await expect(checkboxes.first()).toBeVisible();
    const count = await checkboxes.count();
    expect(count).toBeGreaterThan(0);
    for (let index = 0; index < count; index += 1)
      await expect(checkboxes.nth(index)).toBeChecked();
    await expect(
      dialog.getByRole("button", {
        name: `Опубликовать и обновить выбранных: ${String(count)}`,
        exact: true,
      }),
    ).toBeVisible();

    await checkboxes.first().uncheck();
    await expect(
      dialog.getByRole("button", {
        name: `Опубликовать и обновить выбранных: ${String(count - 1)}`,
        exact: true,
      }),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(publicationRequests).toBe(0);

    await discardDraft(page);
    discarded = true;
    const after = await runtimeEnvironment(page, target.environmentRef);
    expect(after).toMatchObject({
      ref: before.ref,
      version: before.version,
      description: before.description,
      currentVersion: { ref: before.currentVersion.ref },
    });
    expect(serverFailures).toEqual([]);
    expect(browserFailures).toEqual([]);
  } finally {
    if (draftRef && !discarded) {
      await gotoWithRetry(
        page,
        `/projects/${encodeURIComponent(target.projectRef)}/environments/${encodeURIComponent(target.environmentRef)}?draftRef=${encodeURIComponent(draftRef)}`,
      );
      await discardDraft(page);
    }
  }
});

async function environmentWithConsumer(
  page: Page,
): Promise<{ projectRef: string; environmentRef: string }> {
  const projects = await api<ProjectPage>(
    page,
    "/api/v1/projects?pageSize=100",
  );
  for (const project of projects.items) {
    const environments = await api<RuntimeEnvironmentPage>(
      page,
      `/api/v1/projects/${encodeURIComponent(project.ref)}/runtime-environments?pageSize=100`,
    );
    for (const candidate of environments.items) {
      if (!candidate.nextActions.includes("UPDATE")) continue;
      const consumers = await api<{ items: unknown[] }>(
        page,
        `/api/v1/runtime-environments/${encodeURIComponent(candidate.ref)}/agents?pageSize=100`,
      );
      if (consumers.items.length > 0)
        return { projectRef: project.ref, environmentRef: candidate.ref };
    }
  }
  throw new Error("updateable runtime environment with a consumer is absent");
}

async function runtimeEnvironment(
  page: Page,
  environmentRef: string,
): Promise<RuntimeEnvironmentSet> {
  const result = await api<RuntimeEnvironmentSet>(
    page,
    `/api/v1/runtime-environments/${encodeURIComponent(environmentRef)}`,
  );
  expect(result.currentVersion.policy.kubernetesAccess.kind).toMatch(
    /^(NONE|READ_OWN_EXECUTION)$/,
  );
  for (const item of result.currentVersion.policy.network.egress)
    expect(item.destination).not.toMatch(/^RUNTIME_NETWORK_DESTINATION_/);
  return result;
}

async function api<T>(page: Page, path: string): Promise<T> {
  return await page.evaluate(async (url) => {
    const response = await fetch(url);
    if (!response.ok)
      throw new Error(`API read failed with ${String(response.status)}`);
    return (await response.json()) as T;
  }, path);
}

async function discardDraft(page: Page): Promise<void> {
  await page
    .getByRole("button", { name: "Удалить черновик", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Удалить черновик" });
  await expect(dialog).toBeVisible();
  const response = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === "DELETE" &&
      new URL(candidate.url()).pathname.includes(
        "/runtime-environment-drafts/",
      ),
  );
  await dialog
    .getByRole("button", { name: "Удалить черновик", exact: true })
    .click();
  expect((await response).status()).toBe(200);
  await expect(dialog).toHaveCount(0);
}
