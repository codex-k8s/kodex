import { expect, test } from "@playwright/test";

for (const large of [false, true]) {
  test(`Граф: локальные возвраты, ходы и навигация ${large ? "большой" : "малый"}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (
        url.origin !== "https://kodex.test" ||
        url.pathname.startsWith("/api/")
      ) {
        errors.push(`Unexpected request ${url.origin}${url.pathname}`);
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto(`/e2e/fixtures/run-graph.html${large ? "?large=1" : ""}`);
    const toolbar = page.getByRole("toolbar");
    const toolbarBounds = await toolbar.boundingBox();
    expect(
      toolbarBounds &&
        toolbarBounds.x >= 0 &&
        toolbarBounds.x + toolbarBounds.width <= 1440,
    ).toBe(true);
    const expectedNodes = large ? 102 : 6;
    await expect(page.locator(".run-node")).toHaveCount(expectedNodes);
    await expect(
      page.locator('[data-node-ref="coordinator_0"] .run-node__turn'),
    ).toHaveText("· ход 1");
    const callbacks = page.locator(".run-flow-edge--callback_to");
    await expect(callbacks).toHaveCount(large ? 80 : 2);
    expect(
      await callbacks
        .locator("path")
        .evaluateAll((paths) =>
          paths.every(
            (p) =>
              !!p.getAttribute("d") &&
              !/NaN|Infinity/.test(p.getAttribute("d") ?? ""),
          ),
        ),
    ).toBe(true);
    const history = page.getByRole("button", {
      name: "История запусков (1)",
      exact: true,
    });
    await expect(history).toHaveAttribute("aria-pressed", "false");
    await history.click();
    await expect(page.locator(".run-node")).toHaveCount(expectedNodes + 1);
    await history.click();
    await expect(page.locator(".run-node")).toHaveCount(expectedNodes);
    await page.screenshot({
      path: testInfo.outputPath("run-graph-overview.png"),
    });
    await page
      .getByRole("button", { name: "Список узлов", exact: true })
      .click();
    await expect(page.getByRole("treeitem")).toHaveCount(expectedNodes + 1);
    const last = page.getByRole("treeitem").last();
    await last.focus();
    await page.keyboard.press("Home");
    await expect(page.getByRole("treeitem").first()).toBeFocused();
    await page.keyboard.press("End");
    await expect(last).toBeFocused();
    await last.click();
    await expect(page.getByTestId("selection")).toHaveText(
      large ? "coordinator_20" : "coordinator_2",
    );
    await page
      .getByRole("button", { name: "Граф выполнения", exact: true })
      .click();
    await page.getByRole("button", { name: "Добавить активную ветку" }).click();
    await expect(page.locator(".run-node--active")).toHaveCount(1);
    await expect(page.getByTestId("selection")).toHaveText("live_child");
    await page
      .getByRole("button", { name: "Текущая работа", exact: true })
      .click();
    await expect
      .poll(async () =>
        page
          .locator(".vue-flow__transformationpane")
          .evaluate((el) => new DOMMatrix(getComputedStyle(el).transform).a),
      )
      .toBeGreaterThanOrEqual(0.85);
    await page.screenshot({
      path: testInfo.outputPath(`run-graph-${large ? "large" : "small"}.png`),
    });
    expect(errors).toEqual([]);
  });
}
