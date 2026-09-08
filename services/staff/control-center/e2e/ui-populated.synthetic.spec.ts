import { createServer, type ServerResponse } from "node:http";
import { expect, test, type Page } from "@playwright/test";
import {
  validateFixture,
  FixtureUnavailable,
  type FixturePin,
} from "./ui-fixture-manifest";
import { installReadNetworkObserver } from "./ui-read-network";
import { assistantHistory } from "./ui-readonly-forms";
import {
  environmentInspector,
  configurationHistory,
  sourceKeyboard,
  globalSearch,
  vfsRead,
  kanbanPages,
} from "./ui-populated-browser";
import { syntheticCatalogRun } from "./fixtures/catalog-run";
import { permittedRequest } from "./ui-acceptance-proof";
const project = "project_fixture";
const runtime = {
  revision: "0".repeat(64),
  environment: "synthetic",
  apiBaseUrl: "/",
  realtimeUrl: "/api/v1",
  requestTimeoutMs: 10000,
  oidc: {
    authority: "https://identity.invalid",
    clientId: "synthetic",
    redirectUri: "/auth/callback",
    postLogoutRedirectUri: "/",
    scope: "openid",
  },
};
const shell = (body: string) =>
  `<!doctype html><meta charset="UTF-8"><style>body{margin:0}.topbar{height:58px}.app-shell{display:block}</style><div class="app-shell"><div class="topbar"></div><div class="page-header"><h1>Fixture</h1></div>${body}</div>`;
const conversation = (index = 0) => ({
  ref: `conversation_${String(index)}`,
  version: 1,
  title: `Fixture conversation ${String(index)}`,
  state: "ACTIVE",
  titleSource: "USER_EDITED",
  titleRevision: 1,
  context: {
    route: "/",
    entityKind: "HOME",
    entityRef: "",
    entityName: "Fixture",
    allowedOperations: [],
  },
  turns: [],
  updatedAt: `2026-09-08T10:${String(index).padStart(2, "0")}:00Z`,
});
async function component(
  page: Page,
  api: (path: string, params: URLSearchParams) => unknown,
) {
  let writes = 0;
  await page.route("https://kodex.test/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (
      !permittedRequest(
        request.method(),
        url.pathname,
        false,
        request.method() === "POST" ? request.postDataJSON() : undefined,
      )
    ) {
      writes++;
      await route.abort("blockedbyclient");
      return;
    }
    if (url.pathname === "/config/runtime-config.json") {
      await route.fulfill({ json: runtime });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      const body = api(url.pathname, url.searchParams);
      if (body === undefined) throw new Error("Unexpected fixture read");
      await route.fulfill({ json: body });
      return;
    }
    const path =
      request.resourceType() === "document"
        ? "/e2e/fixtures/ui-populated.html"
        : url.pathname;
    const response = await route.fetch({
      url: `http://127.0.0.1:43122${path}`,
      maxRetries: 0,
    });
    await route.fulfill({ response });
  });
  return () => writes;
}
test("populated: exact private fixture readback rejects foreign, stale, forbidden and missing", async ({
  playwright,
}) => {
  const server = createServer((request, response) => {
    const mode = request.url?.split("/").at(-1);
    response.setHeader("content-type", "application/json");
    if (mode === "expired") {
      response.statusCode = 401;
      response.end("{}");
    } else if (mode === "forbidden") {
      response.statusCode = 403;
      response.end("{}");
    } else if (mode === "missing") {
      response.statusCode = 404;
      response.end("{}");
    } else
      response.end(
        JSON.stringify({
          ref: mode,
          version: mode === "stale" ? 3 : 2,
          projectRef: mode === "foreign" ? "other" : project,
        }),
      );
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Fixture listener unavailable");
  const request = await playwright.request.newContext({
    baseURL: `http://127.0.0.1:${String(address.port)}`,
  });
  try {
    await validateFixture(request, {
      slot: "agent",
      kind: "AGENT",
      ref: "ready",
      version: 2,
      projectRef: project,
    });
    for (const ref of ["foreign", "stale", "forbidden", "missing", "expired"])
      await expect(
        validateFixture(request, {
          slot: "agent",
          kind: "AGENT",
          ref,
          version: 2,
          projectRef: project,
        }),
      ).rejects.toBeInstanceOf(FixtureUnavailable);
  } finally {
    await request.dispose();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
test("populated: real same-address fetch cancellation has exact native identity; reset stays unexplained", async ({
  page,
}) => {
  const pending: ServerResponse[] = [];
  const server = createServer((request, response) => {
    if (request.url === "/")
      response.end("<!doctype html><title>Fixture</title>");
    else if (request.url === "/api/v1/projects") pending.push(response);
    else request.socket.destroy();
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Fixture listener unavailable");
  const origin = `http://127.0.0.1:${String(address.port)}`;
  try {
    const failedCodes: string[] = [];
    page.on("requestfailed", (request) =>
      failedCodes.push(request.failure()?.errorText ?? "UNKNOWN"),
    );
    const network = await installReadNetworkObserver(page, origin);
    await page.goto(origin);
    await page.evaluate(() => {
      const controller = new AbortController();
      void fetch("/api/v1/projects", {
        signal: controller.signal,
        cache: "no-store",
      }).catch(() => undefined);
      void fetch("/api/v1/projects", { cache: "no-store" }).catch(
        () => undefined,
      );
      Object.assign(window, { cancelFixture: () => controller.abort() });
    });
    await expect.poll(() => pending.length).toBe(2);
    await page.evaluate(() =>
      (window as unknown as { cancelFixture(): void }).cancelFixture(),
    );
    try {
      await expect
        .poll(() => network.snapshot().confirmedCancellations)
        .toBe(1);
    } catch (error) {
      console.log(
        JSON.stringify({ fixtureNetwork: network.snapshot(), failedCodes }),
      );
      throw error;
    }
    pending[1]?.end("{}");
    await page.evaluate(() => fetch("/api/v1/reset").catch(() => undefined));
    await expect.poll(() => network.snapshot().unexplainedFailures).toBe(1);
    expect(network.snapshot().rawFailedRequests).toBe(2);
  } finally {
    pending.forEach((response) => response.destroy());
    await page.close();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
for (const width of [1440, 768, 390])
  test(`populated: assistant history search ${String(width)} without ready composer`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const writes = await component(page, (path, params) => {
      if (path === "/api/v1/system-assistant")
        return {
          ref: "assistant",
          version: 1,
          name: "Kodex",
          system: true,
          removable: false,
          corePromptRevision: "1",
          ownerInstructions: "",
          runtimeState: "PROVISIONING",
          readinessSummary: "",
          nextActions: [],
        };
      if (path === "/api/v1/assistant-conversations")
        return {
          items: params.get("query")?.includes("absent")
            ? []
            : width === 768 && !params.get("query")
              ? params.get("pageToken")
                ? [conversation(30)]
                : Array.from({ length: 30 }, (_, index) => conversation(index))
              : [conversation()],
          nextPageToken:
            !params.get("query") && !params.get("pageToken") && width === 768
              ? "next"
              : "",
        };
      return undefined;
    });
    const result = await assistantHistory(
      page,
      "en",
      "fixture1341",
      {
        slot: "assistant-primary",
        kind: "ASSISTANT",
        ref: "conversation_0",
        version: 1,
        query: "Fixture conversation",
      },
      width === 768,
    );
    expect(result).toMatchObject({
      openedWithoutComposer: true,
      searchReadback: true,
      clearReadback: true,
    });
    expect(writes()).toBe(0);
  });
test("populated: actual CodeMirror Tab ShiftTab and undo retain source without save", async ({
  page,
}) => {
  const writes = await component(page, () => undefined);
  const pin: FixturePin = {
    slot: "configuration-primary",
    kind: "CONFIGURATION",
    configurationKind: "ROLE_IMAGE",
    ref: "source",
    version: 1,
  };
  expect(await sourceKeyboard(page, pin, "en", true)).toMatchObject({
    indentRoundTrip: true,
    undoRoundTrip: true,
  });
  expect(writes()).toBe(0);
});
test("populated: exact second environment row and history close never choose first fixture", async ({
  page,
}) => {
  let selected = "";
  await page.exposeFunction("selectedFixture", (value: string) => {
    selected = value;
  });
  await page.route("https://kodex.test/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: shell(
        `<table class="environment-table"><tbody>${["other", "exact"].map((ref) => `<tr><td><button onclick="window.selectedFixture('${ref}');document.querySelector('#inspector').innerHTML='<aside class=environment-inspector>Fixture</aside>'">Open</button><a href="/projects/${project}/environments/${ref}">Edit</a></td></tr>`).join("")}</tbody></table><div id="inspector"></div><script>onkeydown=e=>{if(e.key==='Escape')document.querySelector('#inspector').innerHTML=''};</script>`,
      ),
    }),
  );
  await environmentInspector(page, {
    slot: "environment-primary",
    kind: "ENVIRONMENT",
    ref: "exact",
    version: 1,
    projectRef: project,
  });
  expect(selected).toBe("exact");
  await page.unrouteAll({ behavior: "wait" });
  await page.route("https://kodex.test/**", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: shell(
        `<button onclick="document.querySelector('#history').innerHTML='<div role=dialog aria-label=History><button class=configuration-editor__revision>r1</button></div>'">History</button><div id="history"></div><script>onkeydown=e=>{if(e.key==='Escape')document.querySelector('#history').innerHTML=''};</script>`,
      ),
    }),
  );
  expect(
    await configurationHistory(
      page,
      {
        slot: "configuration-primary",
        kind: "CONFIGURATION",
        configurationKind: "ROLE_IMAGE",
        ref: "exact",
        version: 1,
      },
      "en",
    ),
  ).toMatchObject({ rowCount: 1, historyClosedAfterRoute: true });
});
for (const kind of ["PROJECT", "AGENT", "WORKFLOW", "RUN"] as const)
  test(`populated: search ${kind} requires exact route and detail GET`, async ({
    page,
  }) => {
    const pin: FixturePin = {
      slot: "search",
      kind,
      ref: "exact",
      version: 1,
      ...(kind === "PROJECT" ? {} : { projectRef: project }),
      query: "Fixture",
    };
    const route =
        kind === "PROJECT"
          ? "/projects/exact"
          : `/projects/${project}/${kind.toLowerCase()}s/exact`,
      api =
        kind === "PROJECT"
          ? "/api/v1/projects/exact"
          : `/api/v1/${kind.toLowerCase()}s/exact`;
    await page.route("https://kodex.test/**", async (handle) => {
      const url = new URL(handle.request().url());
      if (url.pathname.startsWith("/api/")) {
        await handle.fulfill({ json: { items: [] } });
        return;
      }
      await handle.fulfill({
        contentType: "text/html",
        body: shell(
          `<input id="global-search"><div id="results"></div><script>let timer;document.querySelector('input').oninput=e=>{clearTimeout(timer);if(!e.target.value){document.querySelector('#results').innerHTML='';return;}timer=setTimeout(async()=>{await fetch('/api/v1/search?query='+encodeURIComponent(e.target.value));document.querySelector('#results').innerHTML='<div id=global-search-results><a href="${route}">Fixture</a></div>';document.querySelector('a').onclick=async event=>{event.preventDefault();await fetch('${api}');history.pushState(null,'','${route}');}},500)};</script>`,
        ),
      });
    });
    expect(await globalSearch(page, pin)).toMatchObject({
      exactSearchKindRoute: true,
    });
  });
test("populated: natural search debounce Enter and clear use exact request", async ({
  page,
}) => {
  await page.route("https://kodex.test/**", async (handle) => {
    const url = new URL(handle.request().url());
    if (url.pathname === "/api/v1/search") {
      await handle.fulfill({ json: { items: [] } });
      return;
    }
    await handle.fulfill({
      contentType: "text/html",
      body: shell(
        `<input id="global-search"><div id="results"></div><script>let timer;const field=document.querySelector('input');async function load(){await fetch('/api/v1/search?query='+encodeURIComponent(field.value));document.querySelector('#results').innerHTML='<div id=global-search-results><a href=/projects/exact>Fixture</a></div>'}field.oninput=()=>{clearTimeout(timer);if(!field.value){document.querySelector('#results').innerHTML='';return;}timer=setTimeout(load,500)};field.onkeydown=e=>{if(e.key==='Enter'){clearTimeout(timer);load()}};</script>`,
      ),
    });
  });
  expect(
    await globalSearch(
      page,
      {
        slot: "search-project",
        kind: "PROJECT",
        ref: "exact",
        version: 1,
        query: "Fixture",
      },
      true,
    ),
  ).toMatchObject({ naturalDebounceObserved: true, enterAndClear: true });
});
const node = (ref: string, version = 1) => ({
  ref,
  path: `/projects/${project}/${ref}`,
  parentPath: `/projects/${project}`,
  name: ref,
  kind: "INPUT",
  directory: false,
  projectRef: project,
  entityRef: ref,
  runRef: "",
  sizeBytes: 10,
  digest: "a".repeat(64),
  modifiedAt: "2026-09-08T10:00:00Z",
  version,
  revisionRef: "",
  revision: 0,
  lifecycleState: "ACTIVE",
  scanState: "CLEAN",
  resourceKind: "ARTIFACT",
  selectable: true,
  selectionReason: "AVAILABLE",
  nextActions: ["DELETE"],
});
test("populated: actual VFS selection search and independent cursor keep exact pins", async ({
  page,
}) => {
  const writes = await component(page, (path, params) =>
    path.startsWith("/api/v1/vfs/")
      ? {
          items: [node(params.get("pageToken") ? "second" : "exact")],
          total: 2,
          nextPageToken: params.get("pageToken") ? "" : "next",
        }
      : undefined,
  );
  expect(
    await vfsRead(
      page,
      {
        slot: "vfs-active",
        kind: "VFS",
        ref: "exact",
        version: 1,
        projectRef: project,
        path: `/projects/${project}`,
        lifecycleState: "ACTIVE",
        query: "exact",
      },
      true,
    ),
  ).toMatchObject({
    selectionAndEscape: true,
    pagination: true,
    searched: true,
  });
  expect(writes()).toBe(0);
});
test("populated: actual Kanban reads four independent server cursors", async ({
  page,
}) => {
  const writes = await component(page, (path, params) => {
    if (path === `/api/v1/projects/${project}`)
      return {
        ref: project,
        version: 1,
        name: "Fixture",
        purpose: "",
        language: "en",
        lifecycle: "ACTIVE",
        agentCount: 0,
        workflowCount: 0,
        activeRunCount: 0,
        integrationState: "NONE",
        updatedAt: "2026-09-08T10:00:00Z",
        nextActions: [],
      };
    if (path === "/api/v1/runs") {
      const states = params.getAll("states"),
        index = ["QUEUED", "RUNNING", "WAITING_HUMAN", "SUCCEEDED"].findIndex(
          (value) => states.includes(value),
        ),
        cursor = params.get("pageToken");
      return {
        items: Array.from({ length: cursor ? 1 : 20 }, (_, n) => ({
          ...syntheticCatalogRun(index * 100 + (cursor ? 30 : n), project),
          state: states[0],
        })),
        nextPageToken: cursor ? "" : `lane_${String(index)}`,
      };
    }
    return undefined;
  });
  expect(
    await kanbanPages(page, {
      slot: "project-primary",
      kind: "PROJECT",
      ref: project,
      version: 1,
    }),
  ).toMatchObject({ independentColumns: 4, exactServerCursors: true });
  expect(writes()).toBe(0);
});
