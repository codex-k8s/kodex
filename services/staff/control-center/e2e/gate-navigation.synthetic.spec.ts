import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";
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
    const lists: URLSearchParams[] = [];
    const gate: OwnerGate = {
      scopeKind: "PROJECT",
      organizationRef: "org_synthetic",
      ref: "gate_addressed",
      version: 2,
      projectRef: "project_navigation",
      runRef: "run_navigation",
      nodeRef: "node_navigation",
      title: "Адресованное решение",
      contextSummary: "Точный контекст",
      consequencesSummary: "Точные последствия",
      integrationIntent: {
        connectionRef: "connection_gate",
        connectionName: "Подключение согласования",
        definitionKey: "github",
        capabilityKey: "github.write",
        operation: "create_issue",
        resourceScope: {
          kind: "GITHUB_REPOSITORY",
          values: { repository: "team/project" },
          digest: "a".repeat(64),
        },
        effectKey: `eff_${"a".repeat(32)}`,
        effectPreview: {
          inputDigest: "b".repeat(64),
          inputBytes: 128,
          risk: "WRITE",
          approvalPolicy: "HUMAN_EACH_EFFECT",
          contentComplete: true,
          fields: [
            {
              key: "body",
              type: "STRING",
              bytes: "i18n:INTERACTION_AUTHORITY_CHANGED".length,
              opaque: false,
              value: "i18n:INTERACTION_AUTHORITY_CHANGED",
            },
            {
              key: "title",
              type: "STRING",
              bytes: 12,
              opaque: false,
              value: "TYPE_LITERAL",
            },
          ],
        },
      },
      requestedBy: { ref: "user_navigation", displayName: "Владелец" },
      state: "OPEN",
      allowedDecisions: ["APPROVE"],
      decisionConsequences: [
        {
          decision: "APPROVE",
          safeSummary: "i18n:GATE_CONSEQUENCE_EXTERNAL_EFFECT",
          executesExternalEffect: true,
          terminalForRun: false,
        },
      ],
      openedAt: "2026-09-05T00:00:00Z",
      nextActions: ["RESOLVE_GATE"],
    };
    // Разные server-owned intent refs и имена сохраняют exact navigation,
    // когда заголовок строится из семантики операции и подключения.
    function addressedVariant(
      ref: string,
      title: string,
      name: string,
      marker: string,
    ): OwnerGate {
      const intent = gate.integrationIntent;
      if (!intent) throw new Error("Fixture integration intent missing");
      return {
        ...gate,
        ref,
        title,
        integrationIntent: {
          ...intent,
          connectionRef: `connection_${ref}`,
          connectionName: name,
          effectKey: `eff_${marker.repeat(32)}`,
        },
      };
    }
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
      if (url.pathname === "/api/v1/owner-gates") {
        lists.push(url.searchParams);
        const history = !url.searchParams.getAll("states").includes("OPEN");
        const more = url.searchParams.has("pageToken");
        await route.fulfill({
          json: {
            items: [
              {
                ...addressedVariant(
                  more ? "gate_next" : "gate_other",
                  more
                    ? "Решение следующей страницы"
                    : "Другое решение первой страницы",
                  more
                    ? "Подключение следующей страницы"
                    : "Подключение другого решения",
                  more ? "c" : "b",
                ),
                state: history ? "APPROVED" : "OPEN",
                nextActions: history ? [] : gate.nextActions,
              },
            ],
            nextPageToken: more ? "" : "more_gates",
            total: 91,
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
                  ...addressedVariant(
                    reference,
                    "Адресованная история",
                    "Подключение адресованной истории",
                    "d",
                  ),
                  state: "APPROVED",
                  nextActions: [],
                  decision: "APPROVE",
                  decidedAt: "2026-09-05T01:00:00Z",
                }
              : gate,
        });
        return;
      }
      if (url.pathname === "/api/v1/projects/project_navigation") {
        await route.fulfill({
          json: {
            ref: "project_navigation",
            version: 1,
            name: "Проект навигации",
            purpose: "Синтетическая проверка",
            language: "ru",
            lifecycle: "ACTIVE",
            agentCount: 1,
            workflowCount: 0,
            activeRunCount: 1,
            pendingGateCount: 1,
            integrationState: "READY",
            updatedAt: "2026-11-15T00:00:00Z",
            nextActions: [],
          },
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
    await page.goto(
      "/e2e/fixtures/gate-navigation.html?projectRef=project_navigation",
    );
    const pendingTab = page.getByRole("button", {
      name: "Решения, ожидающие ответа",
    });
    await expect(pendingTab).toHaveText("Ожидают");
    await expect(pendingTab).toHaveAttribute(
      "title",
      "Решения, ожидающие ответа",
    );
    await expect(page.locator(".decision-detail h2")).toHaveText(
      "Изменить данные через «Подключение согласования»",
    );
    expect(reads).toContain("gate_addressed");
    const intent = page.locator(".decision-integration-intent");
    await expect(intent).toContainText("Подключение согласования");
    await expect(intent).toContainText("team/project");
    await expect(intent).toContainText("TYPE_LITERAL");
    await expect(intent).toContainText("i18n:INTERACTION_AUTHORITY_CHANGED");
    await expect(intent).toContainText(`eff_${"a".repeat(32)}`);
    await expect(page.locator(".decision-option")).toContainText(
      "Результат будет подтверждён отдельной квитанцией.",
    );
    await expect(page.locator(".decision-option")).not.toContainText("i18n:");
    await page
      .getByRole("button", {
        name: "Открыть адресованную историю",
        exact: true,
      })
      .click();
    await expect(page.locator(".decision-detail h2")).toHaveText(
      "Изменить данные через «Подключение адресованной истории»",
    );
    expect(reads.at(-1)).toBe("gate_history");
    await expect(intent).toContainText(`eff_${"d".repeat(32)}`);
    await expect(page.locator(".decision-detail")).toContainText("Одобрено");
    await expect(intent).toContainText("TYPE_LITERAL");
    await expect(page.locator(".decision-toolbar__count")).toContainText("91");
    expect(
      lists.some(
        (params) =>
          params.getAll("states").length === 5 && !params.has("state"),
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`gate-navigation-${String(width)}.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "English", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Audit", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".decision-detail")).toContainText(
      "Run initiator",
    );
    await expect(page.locator(".decision-detail")).toContainText(
      "Decision attachments",
    );
    await expect(page.locator(".audit-unavailable")).toHaveText(
      "No audit events were found for this decision.",
    );
    await expect(
      page.getByRole("button", { name: "Decisions awaiting your answer" }),
    ).toHaveText("Pending");
    await page.screenshot({
      path: testInfo.outputPath(`gate-navigation-en-${String(width)}.png`),
      fullPage: true,
    });
    await page
      .getByRole("button", { name: "Decisions awaiting your answer" })
      .click();
    await expect(page.locator(".decision-detail h2")).toHaveText(
      "Change data through “Подключение другого решения”",
    );
    await expect(intent).toContainText(`eff_${"b".repeat(32)}`);
    expect(lists.at(-1)?.getAll("states")).toEqual(["OPEN"]);
    await page
      .getByRole("button", { name: "Открыть скрытое решение", exact: true })
      .click();
    await expect.poll(() => reads.includes("gate_hidden")).toBe(true);
    await expect(page.locator(".decision-detail")).toHaveCount(0);
    await expect(page.locator(".problem-notice")).toBeVisible();
    await page.getByRole("button", { name: "Home Gates", exact: true }).click();
    await expect(page.locator(".home-gate-catalog > header")).not.toContainText(
      "91",
    );
    await page
      .getByRole("button", { name: "Expand list", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("searchbox").fill("literal %_");
    await expect.poll(() => lists.at(-1)?.get("query")).toBe("literal %_");
    await expect(page.locator(".home-gate-catalog > header")).toContainText(
      "91",
    );
    await expect(dialog).toContainText("91");
    // Поиск начинает новую последовательность без cursor; короткая первая
    // страница затем догружается штатным IntersectionObserver списка.
    const searchedPages = () =>
      lists.filter((params) => params.get("query") === "literal %_");
    expect(searchedPages()[0]?.has("pageToken")).toBe(false);
    await expect(dialog.locator(".home-gate-row")).toHaveCount(2);
    expect(searchedPages().at(-1)?.get("pageToken")).toBe("more_gates");
    await expect(dialog.locator(".home-gate-row")).toContainText([
      "Другое решение первой страницы",
      "Решение следующей страницы",
    ]);
    await page.screenshot({
      path: testInfo.outputPath(`home-gates-${String(width)}.png`),
      fullPage: true,
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.unrouteAll({ behavior: "wait" });
    expect(failures).toEqual([]);
  });
}
