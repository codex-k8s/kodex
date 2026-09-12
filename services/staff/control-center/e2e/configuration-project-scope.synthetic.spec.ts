import { expect, test, type Page, type Route } from "@playwright/test";
import type {
  ManagedConfigurationResult,
  Project,
} from "../src/shared/api/generated/openapi/types.gen";

const oldProject = project("project_old", "Старый synthetic Проект");
const newProject = project("project_new", "Новый synthetic Проект");
const csrf = "s".repeat(43);

function project(ref: string, name: string): Project {
  return {
    ref,
    version: 1,
    name,
    purpose: "Synthetic fixture",
    language: "ru",
    lifecycle: "ACTIVE",
    agentCount: 0,
    integrationState: "NONE",
    workflowCount: 0,
    activeRunCount: 0,
    pendingGateCount: 0,
    updatedAt: "2026-09-09T00:00:00Z",
    nextActions: [],
  };
}

async function installBase(
  page: Page,
  handler: (route: Route, url: URL) => Promise<boolean>,
): Promise<string[]> {
  const failures: string[] = [];
  page.on("pageerror", (error) => failures.push(error.message));
  page.on("console", (message) => {
    if (["warning", "error"].includes(message.type()))
      failures.push(message.text());
  });
  await page.context().addCookies([
    {
      name: "__Host-kodex-csrf",
      value: csrf,
      domain: "kodex.test",
      path: "/",
      secure: true,
      sameSite: "Strict",
    },
  ]);
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== "https://kodex.test") {
      failures.push(`Unexpected origin: ${url.origin}`);
      await route.abort();
      return;
    }
    if (url.pathname === "/config/runtime-config.json") {
      await route.fulfill({
        json: {
          revision: "0".repeat(64),
          environment: "synthetic",
          apiBaseUrl: "/",
          realtimeUrl: "/api/v1",
          requestTimeoutMs: 10_000,
          oidc: {
            authority: "https://identity.invalid",
            clientId: "synthetic",
            redirectUri: "/auth/callback",
            postLogoutRedirectUri: "/",
            scope: "openid",
          },
        },
      });
      return;
    }
    if (await handler(route, url)) return;
    if (url.pathname.startsWith("/api/")) {
      failures.push(
        `Unexpected API: ${route.request().method()} ${url.pathname}`,
      );
      await route.fulfill({ status: 404, json: {} });
      return;
    }
    const assetPath =
      route.request().resourceType() === "document" &&
      url.pathname.startsWith("/configurations/")
        ? "/e2e/fixtures/configuration-project-scope.html"
        : url.pathname;
    const response = await route.fetch({
      url: `http://127.0.0.1:43122${assetPath}${url.search}`,
    });
    await route.fulfill({ response });
  });
  return failures;
}

async function chooseProject(page: Page, selected: Project): Promise<void> {
  await page.getByRole("button", { name: "Проект", exact: true }).click();
  await page.getByRole("option", { name: new RegExp(selected.name) }).click();
}

test("synthetic: смена Проекта отменяет pending loadProject и не принимает stale результат", async ({
  page,
}) => {
  let releaseOld!: () => void;
  const oldGate = new Promise<void>((resolve) => {
    releaseOld = resolve;
  });
  let oldRequestStarted = false;
  await page.addInitScript(() => {
    const observedWindow = window as Window & {
      __projectRequestSignals?: Array<{
        path: string;
        source: "input" | "init";
        aborted: boolean;
      }>;
    };
    observedWindow.__projectRequestSignals = [];
    const nativeFetch = window.fetch;
    window.fetch = (input, init) => {
      const request = input instanceof Request ? input : undefined;
      const inputURL =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      const url = new URL(request?.url ?? inputURL, window.location.href);
      for (const [source, signal] of [
        ["input", request?.signal],
        ["init", init?.signal],
      ] as const) {
        if (!signal) continue;
        const observation = {
          path: url.pathname,
          source,
          aborted: signal.aborted,
        };
        observedWindow.__projectRequestSignals?.push(observation);
        signal.addEventListener(
          "abort",
          () => {
            observation.aborted = true;
          },
          { once: true },
        );
      }
      return nativeFetch(input, init);
    };
  });
  const failures = await installBase(page, async (route, url) => {
    if (url.pathname === "/api/v1/projects") {
      await route.fulfill({
        json: { items: [oldProject, newProject], total: 2 },
      });
      return true;
    }
    if (url.pathname === `/api/v1/projects/${oldProject.ref}`) {
      oldRequestStarted = true;
      await oldGate;
      await route.fulfill({ json: oldProject }).catch(() => undefined);
      return true;
    }
    if (url.pathname === `/api/v1/projects/${newProject.ref}`) {
      await route.fulfill({ json: newProject });
      return true;
    }
    return false;
  });

  await page.goto(
    `/configurations/PROMPT_TEMPLATE/new?projectRef=${oldProject.ref}`,
  );
  await expect.poll(() => oldRequestStarted).toBe(true);
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as Window & {
              __projectRequestSignals?: Array<{
                path: string;
                source: "input" | "init";
                aborted: boolean;
              }>;
            }
          ).__projectRequestSignals ?? [],
      ),
    )
    .toContainEqual({
      path: `/api/v1/projects/${oldProject.ref}`,
      source: "init",
      aborted: false,
    });
  await chooseProject(page, newProject);
  await expect(page).toHaveURL(
    new RegExp(`projectRef=${encodeURIComponent(newProject.ref)}$`),
  );
  releaseOld();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as Window & {
              __projectRequestSignals?: Array<{
                path: string;
                source: "input" | "init";
                aborted: boolean;
              }>;
            }
          ).__projectRequestSignals ?? [],
      ),
    )
    .toContainEqual({
      path: `/api/v1/projects/${oldProject.ref}`,
      source: "init",
      aborted: true,
    });
  await expect(
    page.getByRole("button", { name: "Проект", exact: true }),
  ).toContainText(newProject.name);
  await expect(page.getByText(oldProject.name, { exact: true })).toHaveCount(0);
  expect(failures).toEqual([]);
});

test("synthetic: ROLE_IMAGE выбирает Проект и создаёт draft в exact scope", async ({
  page,
}) => {
  const accessTargets: unknown[] = [];
  const createBodies: unknown[] = [];
  const result: ManagedConfigurationResult = {
    configuration: {
      sourceEditable: true,
      ref: "role_image_configuration",
      version: 1,
      kind: "ROLE_IMAGE",
      projectRef: newProject.ref,
      name: "Synthetic образ",
      managedBy: "UI",
      archived: false,
      nextActions: [],
      source: "UI",
      sourceRevision: "",
      updatedAt: "2026-09-09T00:00:00Z",
    },
    revision: {
      sourceAvailable: true,
      ref: "role_image_revision",
      revision: 1,
      state: "DRAFT",
      contentFormat: "JSON",
      content: '{"baseImage":"debian:stable"}',
      digest: "a".repeat(64),
      validationDiagnostics: [],
      createdAt: "2026-09-09T00:00:00Z",
    },
  };
  const failures = await installBase(page, async (route, url) => {
    if (url.pathname === "/api/v1/projects") {
      await route.fulfill({ json: { items: [newProject], total: 1 } });
      return true;
    }
    if (url.pathname === `/api/v1/projects/${newProject.ref}`) {
      await route.fulfill({ json: newProject });
      return true;
    }
    if (
      url.pathname === "/api/v1/administration/access/effective-access/query"
    ) {
      const body = route.request().postDataJSON() as {
        target: { kind: string; projectRef?: string };
        permissionKeys: string[];
      };
      accessTargets.push(body.target);
      await route.fulfill({
        json: {
          items: body.permissionKeys.map((permissionKey) => ({
            permissionKey,
            decision: "ALLOWED",
            target: body.target,
          })),
          total: body.permissionKeys.length,
        },
      });
      return true;
    }
    if (url.pathname === "/api/v1/role-image-configurations/drafts") {
      expect(route.request().method()).toBe("POST");
      expect(route.request().headers()["x-csrf-token"]).toBe(csrf);
      createBodies.push(route.request().postDataJSON());
      await route.fulfill({
        status: 201,
        json: result,
        headers: { ETag: '"1"' },
      });
      return true;
    }
    if (
      url.pathname ===
      `/api/v1/managed-configurations/${result.configuration.ref}/revisions`
    ) {
      await route.fulfill({
        json: {
          configuration: result.configuration,
          items: [result.revision],
          total: 1,
        },
      });
      return true;
    }
    return false;
  });

  await page.goto("/configurations/ROLE_IMAGE/new");
  const save = page.getByRole("button", {
    name: "Сохранить черновик",
    exact: true,
  });
  await expect(save).toBeDisabled();
  await expect(
    page.getByText(
      "Выберите проект. Шаблон промпта и конфигурация образа роли создаются только в выбранном проекте.",
    ),
  ).toBeVisible();
  await chooseProject(page, newProject);
  await expect(page).toHaveURL(
    new RegExp(`projectRef=${encodeURIComponent(newProject.ref)}$`),
  );
  await page.getByLabel("Название", { exact: true }).fill("Synthetic образ");
  await page.getByRole("button", { name: "Источник", exact: true }).click();
  await page
    .getByLabel("Содержимое", { exact: true })
    .fill('{"baseImage":"debian:stable"}');
  await expect(save).toBeEnabled();
  await save.click();
  await expect.poll(() => createBodies.length).toBe(1);
  expect(createBodies).toEqual([
    {
      projectRef: newProject.ref,
      name: "Synthetic образ",
      contentFormat: "JSON",
      content: '{"baseImage":"debian:stable"}',
    },
  ]);
  expect(accessTargets).toContainEqual({
    kind: "PROJECT",
    projectRef: newProject.ref,
  });
  await expect(page).toHaveURL(
    new RegExp(
      `/configurations/ROLE_IMAGE/${result.configuration.ref}\\?projectRef=${newProject.ref}$`,
    ),
  );
  expect(failures).toEqual([]);
});
