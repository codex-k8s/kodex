import { expect, test } from "@playwright/test";
import type { OwnerGate } from "../src/shared/api/generated/openapi/types.gen";
for (const width of [390, 2900]) {
  test(`synthetic: exact ссылка на решение ${String(width)}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    const reads: string[] = [];
    const gate: OwnerGate = {
      ref: "gate_addressed",
      version: 2,
      projectRef: "project_navigation",
      runRef: "run_navigation",
      nodeRef: "node_navigation",
      title: "Адресованное решение",
      contextSummary: "Точный контекст",
      consequencesSummary: "Точные последствия",
      requestedBy: { ref: "user_navigation", displayName: "Владелец" },
      state: "OPEN",
      allowedDecisions: ["APPROVE"],
      decisionConsequences: [
        {
          decision: "APPROVE",
          safeSummary: "Продолжить",
          executesExternalEffect: false,
          terminalForRun: false,
        },
      ],
      openedAt: "2026-09-05T00:00:00Z",
      nextActions: ["RESOLVE_GATE"],
    };
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
      if (url.pathname === "/api/v1/owner-gates") {
        await route.fulfill({
          json: {
            items: [
              {
                ...gate,
                ref: "gate_other",
                title: "Другое решение первой страницы",
              },
            ],
            nextPageToken: "more_gates",
          },
        });
        return;
      }
      if (url.pathname.startsWith("/api/v1/owner-gates/")) {
        const reference = url.pathname.split("/").at(-1) ?? "";
        reads.push(reference);
        if (reference === "gate_hidden") {
          await route.fulfill({
            status: 404,
            json: { status: 404, code: "NOT_FOUND" },
          });
          return;
        }
        await route.fulfill({
          json:
            reference === "gate_history"
              ? {
                  ...gate,
                  ref: reference,
                  title: "Адресованная история",
                  state: "APPROVED",
                  allowedDecisions: [],
                  nextActions: [],
                  decision: "APPROVE",
                  decidedAt: "2026-09-05T01:00:00Z",
                }
              : gate,
        });
        return;
      }
      if (
        ["/api/v1/projects", "/api/v1/runs", "/api/v1/audit-events"].includes(
          url.pathname,
        )
      ) {
        await route.fulfill({
          json: { items: [], nextPageToken: "", total: 0 },
        });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unhandled API ${url.pathname}`);
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto("/e2e/fixtures/gate-navigation.html");
    const pendingTab = page.getByRole("button", {
      name: "Решения, ожидающие ответа",
    });
    await expect(pendingTab).toHaveText("Ожидают");
    await expect(pendingTab).toHaveAttribute(
      "title",
      "Решения, ожидающие ответа",
    );
    await expect(page.locator(".decision-detail h2")).toHaveText(
      "Адресованное решение",
    );
    expect(reads).toContain("gate_addressed");
    await page
      .getByRole("button", {
        name: "Открыть адресованную историю",
        exact: true,
      })
      .click();
    await expect(page.locator(".decision-detail h2")).toHaveText(
      "Адресованная история",
    );
    await expect(page.locator(".decision-detail")).toContainText("Одобрено");
    await page.screenshot({
      path: testInfo.outputPath(`gate-navigation-${String(width)}.png`),
      fullPage: true,
    });
    await page
      .getByRole("button", { name: "Открыть скрытое решение", exact: true })
      .click();
    await expect.poll(() => reads.includes("gate_hidden")).toBe(true);
    await expect(page.locator(".decision-detail")).toHaveCount(0);
    await expect(page.locator(".problem-notice")).toBeVisible();
    await page.unrouteAll({ behavior: "wait" });
    expect(failures).toEqual([]);
  });
}
