import { expect, test } from "@playwright/test";

for (const width of [1440, 390]) {
  test(`synthetic: D3 availability и D5 credential ${String(width)}px`, async ({
    page,
    context,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const failures: string[] = [];
    const agents: string[] = [];
    const queries: string[] = [];
    let mutations = 0;
    page.on("pageerror", () => failures.push("Page error"));
    page.on("console", (entry) => {
      if (["warning", "error"].includes(entry.type()))
        failures.push("Console error");
    });
    page.on("requestfailed", (request) => {
      if (request.failure()?.errorText !== "net::ERR_ABORTED")
        failures.push("Request failed");
    });
    await context.addCookies([
      {
        name: "__Host-kodex-csrf",
        value: "s".repeat(43),
        domain: "kodex.test",
        path: "/",
        secure: true,
        sameSite: "Strict",
      },
    ]);
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.origin !== "https://kodex.test") {
        failures.push("Unexpected origin");
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
            requestTimeoutMs: 10000,
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
      if (
        url.pathname === "/api/v1/projects/project_synthetic/template-variables"
      ) {
        agents.push(url.searchParams.get("agentRef") ?? "");
        queries.push(url.searchParams.get("query") ?? "");
        await route.fulfill({
          json: {
            total: 2,
            items: [
              {
                name: "agent.ref",
                available: true,
                reason: "AVAILABLE",
                valueType: "OPAQUE_REF",
                description: "Агент",
                example: "{{ .agent.ref }}",
                source: "AGENT",
                collection: false,
                itemFields: [],
              },
              {
                name: "runtime.ref",
                available: false,
                reason: "RUNTIME_CONTEXT_REQUIRED",
                valueType: "OPAQUE_REF",
                description: "Выполнение",
                example: "{{ .runtime.ref }}",
                source: "RUNTIME",
                collection: false,
                itemFields: [],
              },
            ],
          },
        });
        return;
      }
      if (
        url.pathname ===
        "/api/v1/integration-connections/connection_synthetic/email-mailbox/credential"
      ) {
        expect(route.request().method()).toBe("PUT");
        expect(route.request().headers()["if-match"]).toBe('"3"');
        expect(route.request().headers()["idempotency-key"]).toBeTruthy();
        expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
        expect(route.request().postDataJSON()).toEqual({
          kind: "AUTH_SECRET",
          value: "  synthetic credential  ",
        });
        mutations++;
        await route.fulfill({
          headers: { ETag: '"4"' },
          json: {
            name: "credential_synthetic",
            generation: 1,
            kind: "AUTH_SECRET",
            connectionRef: "connection_synthetic",
            connectionVersion: 4,
          },
        });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unhandled API ${url.pathname}`);
        await route.abort();
        return;
      }
      const response = await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      });
      if (!response.ok()) failures.push("Asset failure");
      await route.fulfill({ response });
    });
    await page.goto("/e2e/fixtures/checkpoint.html");
    await expect(
      page.getByRole("option", { name: /runtime.ref/ }),
    ).toBeDisabled();
    await expect(page.getByText("Требуется ревизия выполнения")).toBeVisible();
    await page.getByRole("option", { name: /agent.ref/ }).click();
    await expect(page.locator("output")).toHaveText("agent.ref");
    await page.getByRole("button", { name: "Другой агент" }).click();
    await expect.poll(() => agents.at(-1)).toBe("agent_second");
    await page.getByRole("combobox").first().fill("agent");
    await page.waitForTimeout(250);
    expect(queries.at(-1)).toBe("");
    await expect.poll(() => queries.at(-1)).toBe("agent");
    const panel = page.getByRole("region", { name: "Учётные данные почты" });
    await expect(
      panel.getByRole("button", { name: /запис|диктов|микроф/i }),
    ).toHaveCount(0);
    await panel.getByLabel("Новое значение").fill("  synthetic credential  ");
    await panel.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(panel.getByLabel("Новое значение")).toHaveValue("");
    await expect(panel).toContainText("credential_synthetic");
    await expect(panel).toContainText("конфигурация почты не опубликована");
    expect(mutations).toBe(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`d3-d5-${String(width)}.png`),
      fullPage: true,
    });
    expect(failures).toEqual([]);
  });
}
