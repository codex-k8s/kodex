import { expect, test } from "@playwright/test";
import {
  geometry,
  focused,
  observeCondition,
  visit,
  observeActionResponse,
} from "./ui-acceptance-browser";
import { permittedRequest, conditionFailure } from "./ui-acceptance-proof";

// Настоящий Chromium проверяет helper и границу оснастки; это не live PWA/OIDC.
test("synthetic: чтение геометрии и блокировка незапланированного POST", async ({
  context,
  page,
}) => {
  let blocked = 0;
  await context.route("https://kodex.test/**", async (route) => {
    const request = route.request();
    if (
      !permittedRequest(
        request.method(),
        new URL(request.url()).pathname,
        false,
      )
    ) {
      blocked++;
      await route.abort();
      return;
    }
    await route.fulfill({
      contentType: "text/html",
      body: '<!doctype html><style>body{margin:0}.topbar{height:48px}</style><div class="app-shell"><div class="topbar"></div><div class="page-header"><h1>Fixture</h1></div></div>',
    });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  expect(await visit(page, "/projects")).toMatchObject({
    overflow: 0,
    headerHeight: 48,
    headings: 1,
    alerts: 0,
  });
  await page.evaluate(() =>
    fetch("/api/v1/runs", { method: "POST" }).catch(() => undefined),
  );
  expect(blocked).toBe(1);
  await page.evaluate(() => {
    const alert = document.createElement("p");
    alert.setAttribute("role", "alert");
    alert.textContent = "Synthetic error";
    document.body.append(alert);
  });
  expect((await geometry(page)).alerts).toBe(1);
  await page.evaluate(() => {
    document.body.style.width = "2000px";
  });
  expect((await geometry(page)).overflow).toBeGreaterThan(1);
});

test("synthetic: ранний отказ действия не оставляет необработанный timeout ответа", async ({
  page,
}) => {
  await expect(
    observeActionResponse(
      page,
      () => false,
      () => Promise.reject(new Error("Expected fixture action failure")),
      50,
    ),
  ).rejects.toThrow("Expected fixture action failure");
  // Playwright фиксирует поздний unhandled rejection как FAIL самого теста.
  await page.waitForTimeout(100);
});

for (const [kind, expectedCondition, actual, expectedValue] of [
  ["overflow", "DOCUMENT_OVERFLOW", 560, 1],
  ["alert", "VISIBLE_ALERT", 1, 0],
  ["route", "ROUTE_MISMATCH", false, true],
] as const) {
  test(`synthetic: ${kind} сохраняет закрытое измерение без DOM`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.route("https://kodex.test/**", (route) =>
      route.fulfill({
        contentType: "text/html",
        body: `<!doctype html><style>body{margin:0;${kind === "overflow" ? "width:2000px" : ""}}.topbar{height:48px}</style><div class="app-shell"><div class="topbar"></div><div class="page-header"><h1>cookie=private-sentinel</h1></div></div>${kind === "alert" ? '<p role="alert">private-sentinel</p>' : ""}${kind === "route" ? '<script>history.replaceState(null,"","/unexpected?private-sentinel")</script>' : ""}`,
      }),
    );
    const failure = await visit(page, "/projects").then(
      () => null,
      conditionFailure,
    );
    expect(failure).toEqual({
      condition: expectedCondition,
      metrics: { measurementAvailable: true, actual, expected: expectedValue },
    });
    expect(JSON.stringify(failure)).not.toContain("private-sentinel");
  });
}
test("synthetic: ранний click и фокус дают разные закрытые причины", async ({
  page,
}) => {
  await page.setContent(
    '<input id="field"><button id="trigger">private-sentinel</button>',
  );
  const early = await observeCondition("SELECTOR_OPEN", () =>
    page.locator("#missing").click({ timeout: 50 }),
  ).then(() => null, conditionFailure);
  expect(early).toEqual({
    condition: "SELECTOR_OPEN",
    metrics: { measurementAvailable: false },
  });
  const focus = await focused("SELECTOR_FOCUS", page.locator("#field")).then(
    () => null,
    conditionFailure,
  );
  expect(focus).toEqual({
    condition: "SELECTOR_FOCUS",
    metrics: { measurementAvailable: true, actual: false, expected: true },
  });
});
