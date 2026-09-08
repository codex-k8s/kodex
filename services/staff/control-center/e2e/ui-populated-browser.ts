import { expect, type Page, type Response } from "@playwright/test";
import {
  checkCondition,
  geometry,
  observeActionResponse,
  visit,
} from "./ui-acceptance-browser";
import {
  FixtureUnavailable,
  fixturePath,
  type FixturePin,
} from "./ui-fixture-manifest";

export function exactRead(
  response: Response,
  path: string,
  query?: Record<string, string>,
): boolean {
  const url = new URL(response.url());
  return (
    response.request().method() === "GET" &&
    url.pathname === path &&
    Object.entries(query ?? {}).every(
      ([key, value]) => (url.searchParams.get(key) ?? "") === value,
    )
  );
}
export async function environmentInspector(page: Page, pin: FixturePin) {
  if (pin.kind !== "ENVIRONMENT" || !pin.projectRef)
    throw new FixtureUnavailable("MISSING");
  await visit(page, `/projects/${pin.projectRef}/environments`);
  if (pin.query) {
    const response = await observeActionResponse(
      page,
      (value) =>
        exactRead(
          value,
          `/api/v1/projects/${String(pin.projectRef)}/runtime-environments`,
          { query: pin.query ?? "" },
        ),
      () =>
        page
          .locator(".environment-toolbar input[type=search]")
          .first()
          .fill(pin.query ?? ""),
    );
    checkCondition("HTTP_STATUS", response.status(), 200);
  }
  const row = page.locator(".environment-table tbody tr").filter({
    has: page.locator(
      `a[href="/projects/${pin.projectRef}/environments/${pin.ref}"]`,
    ),
  });
  if (!(await row.count())) throw new FixtureUnavailable("NOT_FOUND");
  await expect(row).toHaveCount(1);
  await row.locator("td button").first().click();
  await expect(page.locator(".environment-inspector")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".environment-inspector")).toHaveCount(0);
  return {
    exactEnvironmentSelected: true,
    escapeCleared: true,
    ...(await geometry(page)),
  };
}
export async function configurationHistory(
  page: Page,
  pin: FixturePin,
  locale: "ru" | "en",
) {
  if (pin.kind !== "CONFIGURATION") throw new FixtureUnavailable("MISSING");
  const query = pin.projectRef ? `?projectRef=${pin.projectRef}` : "";
  await visit(
    page,
    `/configurations/${String(pin.configurationKind)}/${pin.ref}${query}`,
  );
  const history = page.getByRole("button", {
    name: locale === "ru" ? "История" : "History",
    exact: true,
  });
  await history.click();
  const dialog = page.getByRole("dialog", {
    name: locale === "ru" ? "История" : "History",
    exact: true,
  });
  await expect(dialog).toBeVisible();
  const rows = dialog.locator(".configuration-editor__revision");
  const rowCount = await rows.count();
  if (!rowCount) throw new FixtureUnavailable("EMPTY_PAGE");
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await visit(page, "/projects");
  await expect(dialog).toHaveCount(0);
  return { exactConfiguration: true, rowCount, historyClosedAfterRoute: true };
}
export async function sourceKeyboard(
  page: Page,
  pin: FixturePin,
  locale: "ru" | "en",
  create = false,
) {
  if (pin.kind !== "CONFIGURATION") throw new FixtureUnavailable("MISSING");
  const query = pin.projectRef ? `?projectRef=${pin.projectRef}` : "";
  await visit(
    page,
    `/configurations/${String(pin.configurationKind)}/${create ? "new" : pin.ref}${query}`,
  );
  const editor = page.locator(".configuration-editor");
  const source = editor.getByRole("button", {
    name: locale === "ru" ? "Источник" : "Source",
    exact: true,
  });
  if (await source.count()) await source.click();
  const code = editor.locator(".cm-content[contenteditable=true]").first();
  if (!(await code.count())) throw new FixtureUnavailable("FORBIDDEN");
  const digest = () =>
    code.evaluate(async (node) =>
      Array.from(
        new Uint8Array(
          await crypto.subtle.digest(
            "SHA-256",
            new TextEncoder().encode(node.textContent),
          ),
        ),
      )
        .map((value) => value.toString(16).padStart(2, "0"))
        .join(""),
    );
  await code.focus();
  await page.keyboard.press("Control+Home");
  const before = await digest();
  await page.keyboard.press("Tab");
  await expect(code).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  checkCondition("UI_ACTION", (await digest()) === before, true);
  await page.keyboard.press("Control+End");
  await page.keyboard.insertText(" ui-proof-unsaved");
  await page.keyboard.press("Control+z");
  checkCondition("UI_ACTION", (await digest()) === before, true);
  await page.keyboard.press("Escape");
  return {
    sourceKeyboard: true,
    indentRoundTrip: true,
    undoRoundTrip: true,
    projectScope: !!pin.projectRef,
    ...(await geometry(page)),
  };
}
export async function globalSearch(
  page: Page,
  pin: FixturePin,
  debounce = false,
) {
  if (!["PROJECT", "AGENT", "WORKFLOW", "RUN"].includes(pin.kind) || !pin.query)
    throw new FixtureUnavailable("MISSING");
  if (pin.kind !== "PROJECT" && !pin.projectRef)
    throw new FixtureUnavailable("MISSING");
  await visit(page, "/projects");
  const field = page.locator("#global-search");
  if (!(await field.isVisible()))
    await page
      .locator(".topbar > button.mobile-only")
      .filter({ has: page.locator("svg.lucide-search") })
      .click();
  let requestedAt = 0;
  const listener = (request: import("@playwright/test").Request) => {
    const url = new URL(request.url());
    if (
      url.pathname === "/api/v1/search" &&
      url.searchParams.get("query") === pin.query
    )
      requestedAt = Date.now();
  };
  page.on("request", listener);
  try {
    let start = 0;
    const response = await observeActionResponse(
      page,
      (value) => exactRead(value, "/api/v1/search", { query: pin.query ?? "" }),
      async () => {
        start = await field.evaluate((node, term) => {
          (node as HTMLInputElement).value = term;
          const at = Date.now();
          node.dispatchEvent(new Event("input", { bubbles: true }));
          return at;
        }, pin.query ?? "");
      },
    );
    // Реальный HTTP read после естественных 500 ms, без fake clock.
    checkCondition("HTTP_STATUS", response.status(), 200);
    if (debounce)
      checkCondition(
        "UI_ACTION",
        requestedAt - start,
        500,
        requestedAt - start >= 500,
      );
    const route =
      pin.kind === "PROJECT"
        ? `/projects/${pin.ref}`
        : `/projects/${String(pin.projectRef)}/${pin.kind === "AGENT" ? "agents" : pin.kind === "WORKFLOW" ? "workflows" : "runs"}/${pin.ref}`;
    const link = page.locator(`#global-search-results a[href="${route}"]`);
    if (!(await link.count())) throw new FixtureUnavailable("NOT_FOUND");
    await expect(link).toHaveCount(1);
    if (!debounce) {
      const detail = await observeActionResponse(
        page,
        (value) => exactRead(value, fixturePath(pin)),
        () => link.click(),
      );
      checkCondition("HTTP_STATUS", detail.status(), 200);
      await expect(page).toHaveURL((url) => url.pathname === route);
      checkCondition(
        "ROUTE_MISMATCH",
        new URL(page.url()).pathname === route,
        true,
      );
    } else {
      await field.fill(pin.query + " absent");
      const next = await observeActionResponse(
        page,
        (value) =>
          exactRead(value, "/api/v1/search", {
            query: String(pin.query) + " absent",
          }),
        () => field.press("Enter"),
      );
      checkCondition("HTTP_STATUS", next.status(), 200);
      await field.fill("");
      await expect(page.locator("#global-search-results")).toHaveCount(0);
      await visit(page, "/");
      await expect(page.locator("#global-search-results")).toHaveCount(0);
    }
    return {
      exactSearchKindRoute: !debounce,
      naturalDebounceObserved: debounce,
      ...(debounce
        ? { debounceMs: requestedAt - start, enterAndClear: true }
        : {}),
    };
  } finally {
    page.off("request", listener);
  }
}
export async function vfsRead(page: Page, pin: FixturePin, paginate = false) {
  if (pin.kind !== "VFS" || !pin.projectRef || !pin.path)
    throw new FixtureUnavailable("MISSING");
  const query = new URLSearchParams({
    vfsTrail: JSON.stringify([{ path: pin.path, name: "QA fixture" }]),
    vfsState: pin.lifecycleState ?? "ACTIVE",
    view: "vfs",
  });
  const response = await observeActionResponse(
    page,
    (value) =>
      exactRead(value, "/api/v1/vfs/nodes", {
        projectRef: pin.projectRef ?? "",
        path: pin.path ?? "",
        pageToken: "",
      }),
    () =>
      visit(
        page,
        `/projects/${String(pin.projectRef)}/files?${query.toString()}`,
      ).then(() => undefined),
  );
  checkCondition("HTTP_STATUS", response.status(), 200);
  const body = (await response.json()) as {
    items: Array<{ ref: string; version: number; name: string }>;
    total: number;
    nextPageToken: string;
  };
  if (
    !Array.isArray(body.items) ||
    !Number.isSafeInteger(body.total) ||
    typeof body.nextPageToken !== "string"
  )
    throw new Error("Invalid VFS page");
  const index = body.items.findIndex(
    (item) => item.ref === pin.ref && item.version === pin.version,
  );
  if (index < 0) throw new FixtureUnavailable("NOT_FOUND");
  const rows = page.locator(".vfs-row");
  await expect(rows).toHaveCount(body.items.length);
  await rows.nth(index).click();
  await expect(page.locator(".vfs-inspector")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".vfs-inspector")).toHaveCount(0);
  if (paginate) {
    if (!body.nextPageToken) throw new FixtureUnavailable("EMPTY_PAGE");
    const next = await observeActionResponse(
      page,
      (value) =>
        exactRead(value, "/api/v1/vfs/nodes", {
          pageToken: body.nextPageToken,
          projectRef: pin.projectRef ?? "",
          path: pin.path ?? "",
        }),
      () => page.locator(".vfs-list > button").click(),
    );
    checkCondition("HTTP_STATUS", next.status(), 200);
    const nextBody = (await next.json()) as {
      items: Array<{ ref: string }>;
      total: number;
    };
    if (
      !Array.isArray(nextBody.items) ||
      nextBody.total !== body.total ||
      nextBody.items.some((item) =>
        body.items.some((old) => old.ref === item.ref),
      )
    )
      throw new Error("Invalid VFS cursor continuity");
    await expect(rows).toHaveCount(body.items.length + nextBody.items.length);
  }
  if (pin.query) {
    const found = await observeActionResponse(
      page,
      (value) =>
        exactRead(value, "/api/v1/vfs/search", {
          query: pin.query ?? "",
          projectRef: pin.projectRef ?? "",
        }),
      () => page.locator(".vfs-search input").fill(pin.query ?? ""),
    );
    checkCondition("HTTP_STATUS", found.status(), 200);
    const clear = await observeActionResponse(
      page,
      (value) =>
        exactRead(value, "/api/v1/vfs/nodes", {
          projectRef: pin.projectRef ?? "",
          path: pin.path ?? "",
          pageToken: "",
        }),
      () => page.locator(".vfs-search input").fill(""),
    );
    checkCondition("HTTP_STATUS", clear.status(), 200);
  }
  return {
    exactVfsPin: true,
    selectionAndEscape: true,
    pagination: paginate,
    searched: !!pin.query,
    total: body.total,
    ...(await geometry(page)),
  };
}

export async function kanbanPages(page: Page, pin: FixturePin) {
  if (pin.kind !== "PROJECT") throw new FixtureUnavailable("MISSING");
  const states = [
    ["QUEUED"],
    ["RUNNING", "CANCELLING"],
    ["WAITING_HUMAN"],
    ["SUCCEEDED", "FAILED", "CANCELLED"],
  ];
  const initial = new Map<
    number,
    { items: Array<{ ref: string; projectRef: string }>; nextPageToken: string }
  >();
  const reads: Array<{ lane: number; cursor: string }> = [],
    pending = new Set<Promise<void>>();
  const readState = { failed: false };
  const assertReadState = () => {
    if (readState.failed) throw new Error("Invalid Kanban response");
  };
  const listener = (response: Response) => {
    if (!exactRead(response, "/api/v1/runs", { projectRef: pin.ref })) return;
    const url = new URL(response.url()),
      requested = url.searchParams.getAll("states").sort().join(",");
    const lane = states.findIndex(
      (value) => [...value].sort().join(",") === requested,
    );
    if (lane < 0) return;
    const cursor = url.searchParams.get("pageToken") ?? "";
    reads.push({ lane, cursor });
    const read = response.json().then((raw: unknown) => {
      if (
        !raw ||
        typeof raw !== "object" ||
        !Array.isArray((raw as { items?: unknown }).items)
      )
        throw new Error("Invalid Kanban page");
      const body = raw as {
        items: Array<{ ref: string; projectRef: string }>;
        nextPageToken: string;
      };
      if (body.items.some((item) => item.projectRef !== pin.ref))
        throw new Error("Kanban scope mismatch");
      if (!cursor) initial.set(lane, body);
    });
    pending.add(read);
    void read
      .catch(() => {
        readState.failed = true;
      })
      .finally(() => pending.delete(read));
  };
  page.on("response", listener);
  try {
    await visit(page, `/projects/${pin.ref}/runs`);
    await expect.poll(() => initial.size).toBe(4);
    await Promise.all(pending);
    assertReadState();
    const lanes = page.locator(".runs-lane__body");
    await expect(lanes).toHaveCount(4);
    for (const lane of [0, 1, 2, 3]) {
      const before = initial.get(lane);
      if (!before?.items.length || !before.nextPageToken)
        throw new FixtureUnavailable("EMPTY_PAGE");
      const otherBefore = reads.filter((value) => value.lane !== lane).length;
      const next = await observeActionResponse(
        page,
        (value) =>
          exactRead(value, "/api/v1/runs", {
            projectRef: pin.ref,
            pageToken: before.nextPageToken,
          }) &&
          new URL(value.url()).searchParams
            .getAll("states")
            .sort()
            .join(",") === [...(states[lane] ?? [])].sort().join(","),
        () =>
          lanes.nth(lane).evaluate((node) => {
            node.scrollTop = node.scrollHeight;
            node.dispatchEvent(new Event("scroll"));
          }),
      );
      checkCondition("HTTP_STATUS", next.status(), 200);
      const body = (await next.json()) as {
        items: Array<{ ref: string; projectRef: string }>;
      };
      if (
        !Array.isArray(body.items) ||
        body.items.some(
          (item) =>
            item.projectRef !== pin.ref ||
            before.items.some((old) => old.ref === item.ref),
        )
      )
        throw new Error("Kanban page continuity failed");
      checkCondition(
        "UI_ACTION",
        reads.filter((value) => value.lane !== lane).length,
        otherBefore,
      );
    }
    await Promise.all(pending);
    assertReadState();
    return {
      independentColumns: 4,
      exactServerCursors: true,
      ...(await geometry(page)),
    };
  } finally {
    page.off("response", listener);
    await Promise.allSettled(pending);
  }
}
