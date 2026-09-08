import {
  loadFixtureManifest,
  validateFixture,
  FixtureUnavailable,
  type FixturePin,
} from "./ui-fixture-manifest";
import { installReadNetworkObserver } from "./ui-read-network";
import {
  environmentInspector,
  configurationHistory,
  sourceKeyboard,
  globalSearch,
  vfsRead,
  kanbanPages,
} from "./ui-populated-browser";
import { test, expect, type Request } from "@playwright/test";
import { loadE2ESessionRenewalEnvironment } from "./environment";
import {
  geometry,
  safeAlertMetrics,
  assistantDialog,
  cleanupAssistant,
  safeFocusMetrics,
  checkCondition,
  checkGeometry,
  observeCondition,
  focused,
  visit,
  observeActionResponse,
} from "./ui-acceptance-browser";
import { installProtocolObserver } from "./session-renewal-proof";
import {
  createJournal,
  integrationPageShape,
  conditionFailure,
  selectedVariants,
  type Condition,
  permittedRequest,
  projectRefs,
  versionsFromEnvironment,
  widths,
  type Variant,
  type Reason,
} from "./ui-acceptance-proof";

import {
  projectForm,
  projectCollection,
  configurationCreate,
  assistantDraft,
  assistantHistory,
  ReadonlyFixtureMissing,
} from "./ui-readonly-forms";
class MissingFixture extends Error {}

const environment = loadE2ESessionRenewalEnvironment();
const globals = [
  ["home", "/", ["MVP-UI-04", "MVP-UI-06"]],
  ["projects", "/projects", ["MVP-UI-05", "MVP-UI-10"]],
  ["agents", "/agents", ["MVP-UI-28"]],
  ["workflows", "/workflows", ["MVP-UI-32"]],
  ["automations", "/automations", ["MVP-UI-43"]],
  ["environments", "/environments", ["MVP-UI-44"]],
  ["secrets", "/secrets", ["MVP-UI-50"]],
  ["members", "/members", ["MVP-UI-27"]],
  ["files", "/files", ["MVP-UI-25", "MVP-UI-26", "MVP-UI-37"]],
  ["runs", "/runs", ["MVP-UI-24"]],
  ["integrations", "/integrations", ["MVP-UI-38", "MVP-UI-39", "MVP-UI-42"]],
  ["decisions", "/decisions", ["MVP-UI-38"]],
  [
    "providers",
    "/administration/providers",
    ["MVP-UI-19", "MVP-UI-51", "MVP-UI-52"],
  ],
  [
    "prompt-templates",
    "/configurations/PROMPT_TEMPLATE",
    ["MVP-UI-15", "MVP-UI-30"],
  ],
  ["role-images", "/configurations/ROLE_IMAGE", ["CFG-01", "CFG-03"]],
  [
    "integration-definitions",
    "/configurations/INTEGRATION_DEFINITION",
    ["CFG-02", "CFG-03"],
  ],
  ["stt", "/configurations/SYSTEM_STT", ["MVP-UI-56"]],
] as const;
const projectSections = [
  "agents",
  "workflows",
  "automations",
  "environments",
  "secrets",
  "members",
  "files",
  "runs",
] as const;

// Чтение существующих данных; opt-in создаёт только два собственных проекта.
// Provider, Run, grants, publish, delete, STT и чужие записи не вызываются.
test("широкая UI-приёмка сохраняет независимые variants и applicability64", async ({
  context,
  browserName,
}, testInfo) => {
  const versions = versionsFromEnvironment(process.env);
  const rawJournal = process.env.KODEX_E2E_UI_EVIDENCE_DIR ?? "";
  const mode = process.env.KODEX_E2E_UI_CREATE_PROJECTS ?? "0";
  if (
    !["0", "1"].includes(mode) ||
    !/^[a-z][a-z0-9-]{3,60}$/.test(environment.resourcePrefix)
  )
    throw new Error("Invalid UI fixture profile");
  const selection = selectedVariants(process.env.KODEX_E2E_UI_VARIANTS, mode);
  const fixtures = loadFixtureManifest(
    process.env.KODEX_E2E_UI_FIXTURE_MANIFEST,
  );
  if (fixtures.manifest && mode !== "0")
    throw new Error("Pinned fixture profile is readonly");
  const journal = await createJournal(
    rawJournal,
    versions,
    browserName,
    fixtures.sha256,
  );
  const variants: Variant[] = [];
  const deadline = Date.now() + environment.runTimeoutMs - 30_000;
  let locale: "ru" | "en" = "ru";
  let width = 1440;
  let creatingProject = false;
  let commonBlocked = false;
  let projects: string[] = [];
  let connectionShape: Record<string, boolean | number> = {};
  const shapeReads = new Set<Promise<void>>();
  const counters = {
    httpErrors: 0,
    pageErrors: 0,
    consoleErrors: 0,
    abortedRequests: 0,
    blockedWrites: 0,
    networkErrors: 0,
  };
  await context.addInitScript(installProtocolObserver);
  const page = await context.newPage();
  const network = await installReadNetworkObserver(page, environment.baseURL);
  const pin = async (slot: string): Promise<FixturePin> => {
    const selected = fixtures.manifest?.fixtures.find(
      (item) => item.slot === slot,
    );
    if (!selected) throw new FixtureUnavailable("MISSING");
    await validateFixture(context.request, selected);
    return selected;
  };
  page.on("pageerror", () => counters.pageErrors++);
  page.on("console", (message) => {
    if (message.type() === "error") counters.consoleErrors++;
  });
  const pendingReads = new Set<Request>();
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.origin === environment.baseURL && url.pathname.startsWith("/api/"))
      pendingReads.add(request);
  });
  page.on("requestfinished", (request) => pendingReads.delete(request));
  page.on("requestfailed", (request) => {
    pendingReads.delete(request);
  });
  page.on("response", (response) => {
    const url = new URL(response.url());
    if (
      url.origin === environment.baseURL &&
      url.pathname === "/api/v1/integration-connections" &&
      response.request().method() === "GET"
    ) {
      const reading = response.json().then(
        (value: unknown) => {
          connectionShape = integrationPageShape(value);
        },
        () => {
          connectionShape = { connectionShapeObserved: false };
        },
      );
      shapeReads.add(reading);
      void reading.finally(() => shapeReads.delete(reading));
    }
    if (
      url.origin === environment.baseURL &&
      url.pathname.startsWith("/api/") &&
      response.status() >= 400
    )
      counters.httpErrors++;
    if (url.origin === environment.baseURL && response.status() === 401)
      commonBlocked = true;
  });
  await context.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    let permissionQuery: unknown;
    if (
      request.method() === "POST" &&
      url.pathname === "/api/v1/administration/access/effective-access/query"
    ) {
      try {
        permissionQuery = request.postDataJSON();
      } catch {
        /* Неверный JSON остаётся заблокирован. */
      }
    }
    if (
      url.origin === environment.baseURL &&
      !permittedRequest(
        request.method(),
        url.pathname,
        creatingProject,
        permissionQuery,
      )
    ) {
      counters.blockedWrites++;
      await route.abort("blockedbyclient");
      return;
    }
    if (request.method() === "POST" && url.pathname === "/api/v1/projects")
      creatingProject = false;
    await route.continue();
  });
  const record = async (
    id: string,
    ids: readonly string[],
    status: Variant["status"],
    reason: Reason,
    metrics: Variant["metrics"] = {},
    condition?: Condition,
  ) => {
    const value: Variant = {
      id,
      requirements: ids,
      status,
      reason,
      locale,
      width,
      metrics,
      ...(condition === undefined ? {} : { condition }),
      timestampUTC: new Date().toISOString(),
    };
    variants.push(value);
    await journal.variant(value);
  };
  const step = async (
    id: string,
    ids: readonly string[],
    action: () => Promise<Variant["metrics"]>,
    available = true,
  ): Promise<boolean> => {
    if (
      selection &&
      !selection.has(id) &&
      id !== "initial-session-shell" &&
      !id.startsWith("locale-")
    )
      return false;
    if (commonBlocked || !available || Date.now() >= deadline) {
      await record(
        id,
        ids,
        "NOT RUN",
        commonBlocked
          ? "DEPENDENCY_UNAVAILABLE"
          : !available
            ? "FIXTURE_UNAVAILABLE"
            : "BUDGET_EXHAUSTED",
      );
      return false;
    }
    connectionShape = {};
    const before = { ...counters };
    const networkBefore = network.snapshot();
    try {
      const metrics = await action();
      await observeCondition(
        "API_READINESS",
        () => expect.poll(() => pendingReads.size, { timeout: 15_000 }).toBe(0),
        () => Promise.resolve(pendingReads.size),
        0,
      );
      await Promise.all(shapeReads);
      const networkAfter = network.snapshot();
      counters.networkErrors = networkAfter.unexplainedFailures;
      counters.abortedRequests = networkAfter.confirmedCancellations;
      checkCondition(
        "NETWORK_ERRORS",
        Math.max(
          0,
          networkAfter.unexplainedFailures - networkBefore.unexplainedFailures,
        ),
        0,
      );
      checkCondition("NETWORK_ERRORS", networkAfter.overflow, 0);
      const settled = await geometry(page);
      checkGeometry(settled);
      checkCondition("HTTP_ERRORS", counters.httpErrors - before.httpErrors, 0);
      checkCondition("PAGE_ERRORS", counters.pageErrors - before.pageErrors, 0);
      checkCondition(
        "NETWORK_ERRORS",
        Math.max(0, counters.networkErrors - before.networkErrors),
        0,
      );
      checkCondition(
        "CONSOLE_ERRORS",
        counters.consoleErrors - before.consoleErrors,
        0,
      );
      checkCondition(
        "BLOCKED_WRITES",
        counters.blockedWrites - before.blockedWrites,
        0,
      );
      await record(id, ids, "PASS", "OBSERVED", {
        ...metrics,
        ...connectionShape,
        settledOverflow: settled.overflow,
        consoleErrors: counters.consoleErrors - before.consoleErrors,
        abortedRequests: counters.abortedRequests - before.abortedRequests,
        rawFailedRequests:
          networkAfter.rawFailedRequests - networkBefore.rawFailedRequests,
        confirmedCancellations:
          networkAfter.confirmedCancellations -
          networkBefore.confirmedCancellations,
        unexplainedFailures: networkAfter.unexplainedFailures,
        networkOverflow: networkAfter.overflow,
      });
      return true;
    } catch (error) {
      if (error instanceof FixtureUnavailable && error.code === "SESSION") {
        commonBlocked = true;
        await record(id, ids, "NOT RUN", "DEPENDENCY_UNAVAILABLE", {
          sessionUnavailable: true,
        });
        return false;
      }
      if (
        (error instanceof MissingFixture ||
          error instanceof ReadonlyFixtureMissing) &&
        counters.httpErrors === before.httpErrors &&
        counters.pageErrors === before.pageErrors &&
        counters.consoleErrors === before.consoleErrors &&
        counters.networkErrors === before.networkErrors &&
        counters.blockedWrites === before.blockedWrites
      ) {
        await record(
          id,
          ids,
          "NOT RUN",
          "FIXTURE_UNAVAILABLE",
          error instanceof ReadonlyFixtureMissing
            ? {
                fixtureProjectsEmpty: error.stage === "PROJECTS_EMPTY",
                fixtureProjectDialogEmpty:
                  error.stage === "PROJECT_DIALOG_EMPTY",
                fixtureHistoryEmpty: error.stage === "ASSISTANT_HISTORY_EMPTY",
                fixtureComposerUnavailable:
                  error.stage === "ASSISTANT_COMPOSER_UNAVAILABLE",
              }
            : {},
        );
        return false;
      }
      if (
        error instanceof FixtureUnavailable &&
        network.snapshot().unexplainedFailures === 0 &&
        counters.httpErrors === before.httpErrors &&
        counters.consoleErrors === before.consoleErrors &&
        counters.blockedWrites === before.blockedWrites
      ) {
        await record(id, ids, "NOT RUN", "FIXTURE_UNAVAILABLE", {
          fixtureMissing: error.code === "MISSING",
          fixtureForbidden: error.code === "FORBIDDEN",
          fixtureNotFound: error.code === "NOT_FOUND",
          fixtureVersionDrift: error.code === "VERSION_DRIFT",
          fixtureScopeMismatch: error.code === "SCOPE_MISMATCH",
          fixtureEmpty: error.code === "EMPTY_PAGE",
          fixturePageBudget: error.code === "PAGE_BUDGET",
        });
        return false;
      }
      await Promise.all(shapeReads);
      const failure = conditionFailure(error);
      await record(
        id,
        ids,
        "FAIL",
        "UI_ASSERTION_FAILED",
        {
          ...failure.metrics,
          ...connectionShape,
          ...(await safeAlertMetrics(page).catch(() => ({}))),
          ...(await safeFocusMetrics(page).catch(() => ({}))),
          rawFailedRequests:
            network.snapshot().rawFailedRequests -
            networkBefore.rawFailedRequests,
          unexplainedFailures: network.snapshot().unexplainedFailures,
          networkOverflow: network.snapshot().overflow,
          httpErrors: counters.httpErrors - before.httpErrors,
          pageErrors: counters.pageErrors - before.pageErrors,
          blockedWrites: counters.blockedWrites - before.blockedWrites,
          networkErrors: counters.networkErrors - before.networkErrors,
          consoleErrors: counters.consoleErrors - before.consoleErrors,
        },
        failure.condition,
      );
      return false;
    }
  };
  try {
    await page.setViewportSize({ width, height: 900 });
    const initial = await step(
      "initial-session-shell",
      ["MVP-UI-07", "MVP-UI-11"],
      async () => {
        const response = await context.request.get("/api/v1/session", {
          timeout: 15_000,
          failOnStatusCode: false,
        });
        expect(response.status()).toBe(200);
        await response.dispose();
        await visit(page, "/");
        await expect
          .poll(
            () =>
              page.evaluate(
                () =>
                  (
                    window as unknown as {
                      __kodexSessionProofProtocols?: string[];
                    }
                  ).__kodexSessionProofProtocols?.includes("v2") ?? false,
              ),
            { timeout: 15_000 },
          )
          .toBe(true);
        return { ticketV2: true };
      },
    );
    commonBlocked = !initial;
    network.setStage("READBACK");
    if (initial && fixtures.manifest) {
      projects = fixtures.manifest.fixtures
        .filter((item) => item.kind === "PROJECT")
        .map((item) => item.ref);
    }
    if (initial && !fixtures.manifest) {
      await step("fixture-project-discovery", ["MVP-UI-27"], async () => {
        const response = await context.request.get(
          "/api/v1/projects?pageSize=2",
          { timeout: 15_000, failOnStatusCode: false },
        );
        expect(response.status()).toBe(200);
        projects = projectRefs(await response.json());
        await response.dispose();
        return { projectCount: projects.length };
      });
    }
    if (mode === "1" && initial) {
      const own: string[] = [];
      for (const slot of [0, 1] as const) {
        const attempt = { sent: false };
        const created = await step(
          `fixture-create-project-${String(slot)}`,
          ["MVP-UI-06", "MVP-UI-10"],
          async () => {
            await visit(page, "/projects");
            await page
              .getByRole("button", { name: "Новый Проект", exact: true })
              .click();
            const form = page.locator("#project-form");
            await form
              .getByLabel("Название", { exact: true })
              .fill(`${environment.resourcePrefix}-project-${String(slot)}`);
            await form
              .getByLabel("Назначение", { exact: true })
              .fill(
                "Синтетический проект для проверки навигации без запуска провайдера",
              );
            await journal.projectIntent(slot);
            creatingProject = true;
            attempt.sent = true;
            const response = await observeActionResponse(
              page,
              (value) =>
                new URL(value.url()).pathname === "/api/v1/projects" &&
                value.request().method() === "POST",
              () =>
                page
                  .locator('button[form="project-form"][type="submit"]')
                  .click(),
            );
            expect(response.status()).toBe(201);
            const refs = projectRefs({ items: [await response.json()] });
            const ref = refs[0];
            if (!ref) throw new Error("Missing created project reference");
            await journal.projectReceipt(slot, ref);
            await expect(page).toHaveURL(new RegExp(`/projects/${ref}$`));
            own.push(ref);
            return { created: true };
          },
        );
        creatingProject = false;
        if (!created) {
          if (attempt.sent)
            await record(
              `fixture-create-project-${String(slot)}-unknown`,
              ["MVP-UI-06"],
              "FAIL",
              "UNKNOWN_OUTCOME",
            );
          break;
        }
      }
      if (own.length) projects = own;
    }
    for (const nextLocale of ["ru", "en"] as const) {
      const changedLocale = await step(
        `locale-${nextLocale}`,
        ["MVP-UI-01"],
        async () => {
          await visit(page, "/");
          await page.locator(".current-user-menu__trigger").click();
          await page
            .getByRole("button", {
              name: nextLocale.toUpperCase(),
              exact: true,
            })
            .click();
          await expect(page.locator("html")).toHaveAttribute(
            "lang",
            nextLocale,
          );
          locale = nextLocale;
          return { localeChanged: true };
        },
      );
      if (!changedLocale) continue;
      for (const nextWidth of [1440, 390]) {
        width = nextWidth;
        await page.setViewportSize({
          width,
          height: width === 390 ? 844 : 900,
        });
        for (const [id, path, ids] of globals)
          await step(
            `route-${id}-${locale}-${String(width)}`,
            ["MVP-UI-01", "MVP-UI-03", ...ids],
            () => visit(page, path),
          );
      }
      for (const formWidth of [1440, 390]) {
        width = formWidth;
        await page.setViewportSize({
          width,
          height: width === 390 ? 844 : 900,
        });
        // Новые действия opt-in через точные IDs; старый широкий BUI не расширяется скрыто.
        if (selection) {
          await step(
            `project-form-cancel-${locale}-${String(width)}`,
            ["MVP-UI-01", "MVP-UI-10", "MVP-UI-22"],
            () => projectForm(page, locale, environment.resourcePrefix),
          );
          await step(
            `project-collection-expand-${locale}-${String(width)}`,
            ["MVP-UI-05", "MVP-UI-10", "MVP-UI-12"],
            () => projectCollection(page, locale),
          );
          for (const kind of [
            "PROMPT_TEMPLATE",
            "ROLE_IMAGE",
            "INTEGRATION_DEFINITION",
          ] as const) {
            await step(
              `configuration-create-editor-${kind.toLowerCase().replaceAll("_", "-")}-${locale}-${String(width)}`,
              kind === "ROLE_IMAGE"
                ? ["CFG-01", "CFG-03"]
                : kind === "INTEGRATION_DEFINITION"
                  ? ["CFG-02", "CFG-03"]
                  : ["MVP-UI-15", "MVP-UI-30"],
              () => configurationCreate(page, kind, locale),
            );
          }
          await step(
            `assistant-history-draft-${locale}-${String(width)}`,
            ["MVP-UI-09", "MVP-UI-14"],
            () => assistantDraft(page, locale, environment.resourcePrefix),
          );
        }
      }
      if (selection)
        for (const historyWidth of [1440, 768, 390]) {
          width = historyWidth;
          await page.setViewportSize({
            width,
            height: width === 390 ? 844 : 1024,
          });
          await step(
            `assistant-history-search-${locale}-${String(width)}`,
            ["MVP-UI-09", "MVP-UI-14"],
            async () =>
              assistantHistory(
                page,
                locale,
                environment.resourcePrefix,
                await pin("assistant-primary"),
              ),
          );
        }
      for (const nextWidth of widths) {
        width = nextWidth;
        await page.setViewportSize({
          width,
          height: width === 390 ? 844 : width === 768 ? 1024 : 1080,
        });
        for (const [id, path] of [
          ["home", "/"],
          ["projects", "/projects"],
          ["runs", "/runs"],
        ] as const)
          await step(
            `geometry-${id}-${locale}-${String(width)}`,
            ["MVP-UI-02", "MVP-UI-03", "MVP-UI-12", "MVP-UI-24"],
            () => visit(page, path),
          );
      }
    }
    width = 1440;
    await page.setViewportSize({ width, height: 900 });
    for (const [index, ref] of projects.entries()) {
      for (const section of projectSections)
        await step(`project-${String(index)}-${section}`, ["MVP-UI-27"], () =>
          visit(page, `/projects/${ref}/${section}`),
        );
      await step(
        `project-${String(index)}-files-trash`,
        ["MVP-UI-25", "MVP-UI-26"],
        () => visit(page, `/projects/${ref}/files/trash`),
      );
      await step(
        `project-${String(index)}-environment-inspector`,
        ["MVP-UI-44", "MVP-UI-48"],
        async () => {
          if (selection) {
            const exact = fixtures.manifest?.fixtures.find(
              (item) => item.kind === "ENVIRONMENT" && item.projectRef === ref,
            );
            if (!exact) throw new FixtureUnavailable("MISSING");
            await validateFixture(context.request, exact);
            return environmentInspector(page, exact);
          }
          await visit(page, `/projects/${ref}/environments`);
          await expect(page.locator(".environment-inspector")).toHaveCount(0);
          const rows = page.locator(".environment-table tbody tr");
          if ((await rows.count()) === 0) {
            await record(
              `project-${String(index)}-environment-populated`,
              ["MVP-UI-44", "MVP-UI-48"],
              "NOT RUN",
              "FIXTURE_UNAVAILABLE",
            );
            return { defaultSelection: false, selectionAndEscape: false };
          }
          await rows.first().click();
          await expect(page.locator(".environment-inspector")).toBeVisible();
          await page.keyboard.press("Escape");
          await expect(page.locator(".environment-inspector")).toHaveCount(0);
          return { defaultSelection: false, selectionAndEscape: true };
        },
      );
    }
    for (const [kind, ids] of [
      ["PROMPT_TEMPLATE", ["MVP-UI-15", "MVP-UI-30"]],
      ["ROLE_IMAGE", ["CFG-01", "CFG-03"]],
      ["INTEGRATION_DEFINITION", ["CFG-02", "CFG-03"]],
    ] as const) {
      await step(
        `configuration-history-${kind.toLowerCase().replaceAll("_", "-")}`,
        ids,
        async () => {
          if (selection) {
            const exact = fixtures.manifest?.fixtures.find(
              (item) =>
                item.kind === "CONFIGURATION" &&
                item.configurationKind === kind,
            );
            if (!exact) throw new FixtureUnavailable("MISSING");
            await validateFixture(context.request, exact);
            return configurationHistory(page, exact, locale);
          }
          await visit(page, `/configurations/${kind}`);
          const entry = page.locator(".configuration-catalog__row").first();
          if (!(await entry.count())) throw new MissingFixture();
          await entry.click();
          const history = page.getByRole("button", {
            name: locale === "ru" ? "История" : "History",
            exact: true,
          });
          await expect(history).toBeVisible();
          await history.click();
          const dialog = page.getByRole("dialog", {
            name: locale === "ru" ? "История" : "History",
            exact: true,
          });
          await expect(dialog).toBeVisible();
          const metrics = await geometry(page);
          expect(metrics.overflow).toBeLessThanOrEqual(1);
          await page.keyboard.press("Escape");
          await expect(dialog).toHaveCount(0);
          return { historyOpenedAndClosed: true, overflow: metrics.overflow };
        },
      );
    }
    if (selection) {
      await step(
        "fixture-environment-inspector",
        ["MVP-UI-44", "MVP-UI-48"],
        async () =>
          environmentInspector(page, await pin("environment-primary")),
      );
      await step(
        "fixture-configuration-history",
        ["CFG-01", "CFG-02", "CFG-03"],
        async () =>
          configurationHistory(
            page,
            await pin("configuration-primary"),
            locale,
          ),
      );
      await step(
        "fixture-configuration-source-keys",
        ["MVP-UI-15", "MVP-UI-20", "CFG-01", "CFG-02"],
        async () =>
          sourceKeyboard(page, await pin("configuration-primary"), locale),
      );
      await step(
        "fixture-role-image-project-source",
        ["CFG-01", "CFG-03"],
        async () => {
          const exact = await pin("configuration-project-role-image");
          if (!exact.projectRef || exact.configurationKind !== "ROLE_IMAGE")
            throw new FixtureUnavailable("MISSING");
          return sourceKeyboard(page, exact, locale, true);
        },
      );
      await step(
        "fixture-assistant-history-pagination",
        ["MVP-UI-09"],
        async () =>
          assistantHistory(
            page,
            locale,
            environment.resourcePrefix,
            await pin("assistant-primary"),
            true,
          ),
      );
      await step("fixture-global-search-debounce", ["MVP-UI-53"], async () =>
        globalSearch(page, await pin("search-project"), true),
      );
      for (const kind of ["project", "agent", "workflow", "run"])
        await step(`fixture-global-search-${kind}`, ["MVP-UI-54"], async () => {
          const exact = await pin(`search-${kind}`);
          if (exact.kind !== kind.toUpperCase())
            throw new FixtureUnavailable("SCOPE_MISMATCH");
          return globalSearch(page, exact);
        });
      for (const state of ["active", "trash"])
        await step(
          `fixture-vfs-${state}`,
          ["MVP-UI-25", "MVP-UI-26"],
          async () => {
            const exact = await pin(`vfs-${state}`);
            if (
              exact.lifecycleState !==
              (state === "active" ? "ACTIVE" : "DELETED")
            )
              throw new FixtureUnavailable("SCOPE_MISMATCH");
            return vfsRead(page, exact);
          },
        );
      await step(
        "fixture-vfs-pagination",
        ["MVP-UI-25", "MVP-UI-26"],
        async () => vfsRead(page, await pin("vfs-active"), true),
      );
      await step("fixture-kanban-pages", ["MVP-UI-24"], async () =>
        kanbanPages(page, await pin("project-primary")),
      );
    }
    await step("project-eight-sections", ["MVP-UI-27"], async () => {
      const ref = projects[0];
      if (!ref) throw new MissingFixture();
      const exact = fixtures.manifest?.fixtures.find(
        (item) => item.kind === "PROJECT" && item.ref === ref,
      );
      if (exact) await validateFixture(context.request, exact);
      await visit(page, `/projects/${ref}/agents`);
      for (const section of projectSections)
        await expect(
          page.locator(`.project-nav a[href="/projects/${ref}/${section}"]`),
        ).toHaveCount(1);
      return { sectionCount: projectSections.length };
    });
    await step(
      "project-picker-keyboard-escape",
      ["MVP-UI-13", "MVP-UI-22"],
      async () => {
        await visit(page, "/projects");
        const trigger = page.locator(
          ".topbar-project-picker .async-picker__trigger",
        );
        await observeCondition("SELECTOR_OPEN", () => trigger.click());
        const input = page.locator(
          ".async-picker__popover input[role=combobox]",
        );
        await focused("SELECTOR_FOCUS", input);
        await input.press("ArrowDown");
        await input.press("Escape");
        const popover = page.locator(".async-picker__popover");
        await observeCondition(
          "SELECTOR_ESCAPE",
          () => expect(popover).toHaveCount(0),
          () => popover.count(),
          0,
        );
        await focused("SELECTOR_RETURN_FOCUS", trigger);
        return { focusReturned: true };
      },
    );
    await step(
      "project-picker-async-search",
      ["MVP-UI-13", "MVP-UI-22"],
      async () => {
        await visit(page, "/projects");
        await page
          .locator(".topbar-project-picker .async-picker__trigger")
          .click();
        const input = page.locator(
          ".async-picker__popover input[role=combobox]",
        );
        const query = `${environment.resourcePrefix}-absent-search-fixture`;
        const response = await observeActionResponse(
          page,
          (value) => {
            const url = new URL(value.url());
            return (
              value.request().method() === "GET" &&
              url.pathname === "/api/v1/projects" &&
              url.searchParams.get("query") === query
            );
          },
          () => input.fill(query),
        );
        expect(response.status()).toBe(200);
        await expect(
          page.locator('.async-picker__popover [role="option"]'),
        ).toHaveCount(0);
        await input.press("Escape");
        return { serverSearch: true, emptyResult: true };
      },
    );
    await step("kanban-independent-scroll", ["MVP-UI-24"], async () => {
      await visit(page, "/runs");
      const lanes = page.locator(".runs-lane__body");
      if (!(await lanes.count())) throw new MissingFixture();
      await expect(lanes).toHaveCount(4);
      const bounds = await lanes.evaluateAll((items) =>
        items.map((item) => ({
          top: item.scrollTop,
          scroll: item.scrollHeight,
          client: item.clientHeight,
        })),
      );
      const index = bounds.findIndex(
        (value) => value.scroll > value.client + 20,
      );
      if (index < 0) throw new MissingFixture();
      await lanes.nth(index).hover();
      await page.mouse.wheel(0, 600);
      await expect
        .poll(async () => lanes.nth(index).evaluate((item) => item.scrollTop))
        .toBeGreaterThan(0);
      const after = await lanes.evaluateAll((items) =>
        items.map((item) => item.scrollTop),
      );
      expect(
        after.every((top, i) => i === index || top === bounds[i]?.top),
      ).toBe(true);
      return { laneCount: 4, otherLanesStable: true };
    });
    await step("global-search-enter-clear", ["MVP-UI-53"], async () => {
      await visit(page, "/projects");
      const search = page.locator("#global-search");
      await search.fill("kodex-ui-no-match");
      await search.press("Enter");
      await expect(page.locator("#global-search-results")).toBeVisible();
      await search.fill("");
      await expect(page.locator("#global-search-results")).toHaveCount(0);
      return { cleared: true };
    });
    for (const nextWidth of [1440, 768, 390]) {
      width = nextWidth;
      await page.setViewportSize({ width, height: width === 390 ? 844 : 1024 });
      await step(
        `assistant-history-shell-${String(width)}`,
        ["MVP-UI-09"],
        async () => {
          await visit(page, "/");
          const dialog = assistantDialog(page, locale);
          try {
            await observeCondition("ASSISTANT_OPEN", () =>
              page
                .getByRole("button", {
                  name: locale === "ru" ? "Открыть Kodex" : "Open Kodex",
                  exact: true,
                })
                .click(),
            );
            await observeCondition(
              "ASSISTANT_VISIBLE",
              () => expect(dialog).toBeVisible(),
              () => dialog.isVisible(),
              true,
            );
            const result = await geometry(page);
            checkGeometry(result);
            await focused("ASSISTANT_FOCUS", dialog);
            await page.keyboard.press("Escape");
            await observeCondition(
              "ASSISTANT_ESCAPE",
              () => expect(dialog).toHaveCount(0),
              () => dialog.count(),
              0,
            );
            return { openedAndClosed: true, overflow: result.overflow };
          } finally {
            const cleaned = await cleanupAssistant(page, locale).then(
              () => true,
              () => false,
            );
            if (!cleaned) {
              commonBlocked = true;
              await record(
                `assistant-cleanup-${String(width)}`,
                ["MVP-UI-09"],
                "FAIL",
                "UI_ASSERTION_FAILED",
                { measurementAvailable: false },
                "CLEANUP",
              );
            }
          }
        },
      );
    }
    if (!selection && !projects.length)
      await record(
        "project-scope-required-fixture",
        ["MVP-UI-27", "MVP-UI-44", "MVP-UI-48"],
        "NOT RUN",
        "FIXTURE_UNAVAILABLE",
      );
    await record(
      "remaining-product-variants",
      [
        "MVP-UI-11",
        "MVP-UI-16",
        "MVP-UI-29",
        "MVP-UI-35",
        "MVP-UI-36",
        "MVP-UI-47",
        "MVP-UI-55",
        "MVP-UI-57",
        "MVP-UI-59",
        "MVP-UI-60",
        "MVP-UI-61",
      ],
      "NOT RUN",
      "OUTSIDE_PROFILE",
    );
  } finally {
    // Закрываем страницы до reporter/error-context; персональные данные не снимаются.
    network.setStage("COMPLETE");
    const closed = await page.close().then(
      () => true,
      () => false,
    );
    if (!closed)
      await record(
        "browser-cleanup",
        ["MVP-UI-09", "MVP-UI-11"],
        "FAIL",
        "UI_ASSERTION_FAILED",
      );
    await journal.network(network);
    await journal.close(variants);
    await testInfo.attach("ui-acceptance-safe-evidence", {
      path: journal.path,
      contentType: "application/x-ndjson",
    });
  }
  if (variants.some((value) => value.status === "FAIL"))
    throw new Error("UI acceptance has failed variants; inspect safe evidence");
});
