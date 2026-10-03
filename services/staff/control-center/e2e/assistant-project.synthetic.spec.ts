import { expect, test } from "@playwright/test";

for (const width of [1440, 390]) {
  for (const locale of ["ru", "en"]) {
    test(`Помощник проекта: результат и шапка ${String(width)}px ${locale}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 1000 });
      const calls: string[] = [];
      const failures: string[] = [];
      page.on("pageerror", (error) => failures.push(error.message));
      page.on("console", (message) => {
        if (["warning", "error"].includes(message.type()))
          failures.push(message.text());
      });
      await installFixture(page, calls);
      await page.goto(
        `/e2e/fixtures/ui-proof.html?fixture=project-assistant&locale=${locale}`,
      );
      await page.locator(".assistant-fab").click();
      const card = page.locator(".assistant-created-entity");
      await expect(card.locator("a")).toHaveAttribute(
        "href",
        "/projects/project_fixture/agents/agent_helper_fixture?assistantForm=1",
      );
      expect(calls).toEqual([
        "/api/v1/projects/project_fixture/assistant",
        "/api/v1/agents/agent_helper_fixture",
      ]);
      const header = page.locator(".assistant-drawer__header");
      const identity = await header
        .locator(".assistant-drawer__identity")
        .boundingBox();
      const picker = await header.locator("select").boundingBox();
      expect(identity?.width).toBeGreaterThan(70);
      expect(picker?.width).toBeGreaterThan(200);
      expect(picker?.height).toBe(32);
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        )
        .toBe(true);
      await page.screenshot({
        path: testInfo.outputPath(
          `project-assistant-${String(width)}-${locale}.png`,
        ),
        fullPage: true,
      });
      expect(failures).toEqual([]);
    });
  }
}

for (const mismatch of [
  "profile-ref",
  "profile-project",
  "agent-ref",
  "agent-project",
]) {
  test(`Помощник проекта: нет перехода при несовпадении ${mismatch}`, async ({
    page,
  }) => {
    const calls: string[] = [];
    await installFixture(page, calls, mismatch);
    await page.goto("/e2e/fixtures/ui-proof.html?fixture=project-assistant");
    await page.locator(".assistant-fab").click();
    const card = page.locator(".assistant-created-entity");
    await expect(card.getByRole("alert")).toBeVisible();
    await expect(card.locator("a")).toHaveCount(0);
    expect(calls).toEqual(
      mismatch.startsWith("profile")
        ? ["/api/v1/projects/project_fixture/assistant"]
        : [
            "/api/v1/projects/project_fixture/assistant",
            "/api/v1/agents/agent_helper_fixture",
          ],
    );
  });
}

async function installFixture(
  page: import("@playwright/test").Page,
  calls: string[],
  mismatch?: string,
) {
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/config/runtime-config.json") {
      await route.fulfill({
        json: {
          revision: "0".repeat(64),
          environment: "synthetic",
          apiBaseUrl: "/",
          realtimeUrl: "/api/v1",
          requestTimeoutMs: 10000,
          oidc: {
            authority: "https://identity.kodex.test/realms/kodex",
            clientId: "kodex-test",
            redirectUri: "https://kodex.test/auth/callback",
            postLogoutRedirectUri: "https://kodex.test/",
            scope: "openid profile email",
          },
        },
      });
    } else if (url.pathname === "/api/v1/projects/project_fixture/assistant") {
      calls.push(url.pathname);
      await route.fulfill({
        json: {
          ref: mismatch === "profile-ref" ? "asstp_wrong" : "asstp_fixture",
          projectRef:
            mismatch === "profile-project"
              ? "project_wrong"
              : "project_fixture",
          agentRef: "agent_helper_fixture",
          name: "Помощник проекта",
          state: "ACTIVE",
          version: 1,
          createdAt: "2026-10-04T00:00:00Z",
          updatedAt: "2026-10-04T00:00:00Z",
        },
      });
    } else if (url.pathname === "/api/v1/agents/agent_helper_fixture") {
      calls.push(url.pathname);
      await route.fulfill({
        json: {
          ref:
            mismatch === "agent-ref" ? "agent_wrong" : "agent_helper_fixture",
          projectRef:
            mismatch === "agent-project" ? "project_wrong" : "project_fixture",
          name: "Помощник проекта разработки платформы",
          version: 1,
          purpose: "Помощь",
          roleDescription: "Помощник",
          state: "DRAFT",
          enabled: true,
          system: false,
          runtimeRef: "runtime_fixture",
          runtimeName: "Базовый",
          runtimeReady: false,
          capabilities: [],
          integrations: [],
          knowledgeArtifactRefs: [],
          nextActions: ["EDIT"],
          updatedAt: "2026-10-04T00:00:00Z",
        },
      });
    } else if (
      url.origin === "https://kodex.test" &&
      !url.pathname.startsWith("/api/")
    ) {
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    } else {
      throw new Error(`Unexpected fixture request ${url.pathname}`);
    }
  });
}
