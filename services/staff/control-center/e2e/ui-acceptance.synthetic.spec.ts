import { expect, test } from "@playwright/test";
import { geometry, visit } from "./ui-acceptance-browser";
import { permittedRequest } from "./ui-acceptance-proof";

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
