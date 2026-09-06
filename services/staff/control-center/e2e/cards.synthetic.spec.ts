import { expect, test, type Locator } from "@playwright/test";

async function metric(card: Locator, name: string, value: string) {
  await expect(card.locator(`[data-metric="${name}"] dd`)).toHaveText(value);
}

for (const width of [390, 2900]) {
  for (const scope of ["global", "project"] as const) {
    test(`Карточки ${scope}: проекции и действия на ${String(width)} px`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 1100 });
      const failures: string[] = [];
      page.on("pageerror", (error) => failures.push(error.message));
      page.on("console", (message) => {
        if (["warning", "error"].includes(message.type()))
          failures.push(message.text());
      });
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        // Оснастка не обращается к API: агрегаты уже доставлены в DTO карточек.
        if (
          url.origin !== "https://kodex.test" ||
          url.pathname.startsWith("/api/")
        ) {
          failures.push(`Unexpected request ${url.origin}${url.pathname}`);
          await route.abort();
          return;
        }
        await route.fulfill({
          response: await route.fetch({
            url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
          }),
        });
      });
      await page.goto(`/e2e/fixtures/cards.html?scope=${scope}`);
      await expect(page.locator("main")).toHaveAttribute("data-scope", scope);
      await expect(page.locator("section[data-project]")).toHaveCount(
        scope === "global" ? 2 : 1,
      );
      await expect(page.locator(".agent-card")).toHaveCount(
        scope === "global" ? 4 : 3,
      );
      await expect(page.locator(".workflow-card")).toHaveCount(
        scope === "global" ? 4 : 3,
      );

      const projects = page.locator(".project-list__item");
      await expect(projects).toHaveCount(scope === "global" ? 4 : 1);
      const project = projects.first();
      await expect(project.locator("dl dd")).toContainText([
        "13",
        "17",
        "19",
        "23",
      ]);
      await metric(project, "integrationState", "Нет подключений");
      await expect(
        project.locator('[data-metric="lastActivityAt"] time'),
      ).toHaveAttribute("datetime", "2026-09-06T12:34:00Z");
      await expect(
        project.getByRole("link", {
          name: "Запустить сотрудника",
          exact: true,
        }),
      ).toHaveAttribute(
        "href",
        "/projects/project_cards_0/runs/new?targetType=AGENT",
      );
      await expect(
        project.getByRole("link", { name: "Запустить Процесс", exact: true }),
      ).toHaveAttribute(
        "href",
        "/projects/project_cards_0/runs/new?targetType=WORKFLOW",
      );
      if (scope === "global") {
        for (const [index, label] of [
          "Готовы",
          "Требуют внимания",
          "Неизвестно",
        ].entries()) {
          const denied = projects.nth(index + 1);
          await metric(denied, "integrationState", label);
          await expect(
            denied.locator('[data-metric="lastActivityAt"]'),
          ).toContainText("Нет данных");
          await expect(
            denied.locator('[data-metric="lastActivityAt"] time'),
          ).toHaveCount(0);
          await expect(
            denied.getByRole("link", { name: /^Запустить / }),
          ).toHaveCount(0);
        }
      }

      const ready = page.locator('[data-card-ref="agent_ready"]');
      const running = page.locator('[data-card-ref="agent_running"]');
      const deniedAgent = page.locator('[data-card-ref="agent_denied"]');
      await expect(
        ready.locator('.status-badge[data-state="READY"]'),
      ).toHaveCount(1);
      await expect(ready.locator(".agent-card__activity")).toHaveCount(0);
      await expect(
        ready.getByRole("link", { name: "Открыть запуск", exact: true }),
      ).toHaveCount(0);
      await expect(deniedAgent.locator(".agent-card__activity")).toHaveCount(0);
      await expect(
        deniedAgent.getByRole("link", { name: "Открыть запуск", exact: true }),
      ).toHaveCount(0);
      await expect(running.locator(".agent-card__activity")).toContainText(
        "Проверяет входные данные",
      );
      const runLink = running.getByRole("link", {
        name: "Открыть запуск",
        exact: true,
      });
      await expect(runLink).toHaveAttribute("href", "/runs/run_cards_first");
      await runLink.click();
      await expect(page.getByLabel("Последний переход")).toHaveText(
        "/runs/run_cards_first",
      );

      const published = page.locator('[data-card-ref="workflow_published"]');
      const draft = page.locator('[data-card-ref="workflow_draft"]');
      const deniedWorkflow = page.locator('[data-card-ref="workflow_denied"]');
      for (const [name, value] of Object.entries({
        stageCount: "31",
        uniqueAgentCount: "11",
        parallelGroupCount: "5",
        hasHumanGate: "Да",
        activeRunCount: "7",
        pendingGateCount: "3",
      })) {
        await metric(published, name, value);
      }
      await metric(draft, "hasHumanGate", "Нет");
      await metric(draft, "lastActivityAt", "Нет данных");
      await expect(
        published.locator('[data-metric="lastActivityAt"] time'),
      ).toHaveAttribute("datetime", "2026-09-06T12:34:00Z");
      await expect(
        draft.locator('[data-metric="lastActivityAt"] time'),
      ).toHaveCount(0);
      for (const card of [draft, deniedWorkflow]) {
        await expect(
          card.getByRole("button", { name: "Запустить", exact: true }),
        ).toBeDisabled();
        await expect(
          card.getByRole("button", { name: "Запустить", exact: true }),
        ).toHaveAttribute("title", /\S/u);
        await expect(
          card.getByRole("link", { name: "Запустить", exact: true }),
        ).toHaveCount(0);
      }
      await expect(
        deniedWorkflow.getByRole("link", {
          name: "Изменить",
          exact: true,
        }),
      ).toHaveCount(0);
      await expect(
        draft.getByRole("link", { name: "Изменить", exact: true }),
      ).toHaveAttribute(
        "href",
        "/projects/project_cards_0/workflows/workflow_draft#workflow-editor",
      );
      await expect(
        published.getByRole("link", { name: "Открыть", exact: true }),
      ).toHaveAttribute(
        "href",
        "/projects/project_cards_0/workflows/workflow_published",
      );
      await expect(
        published.getByRole("link", { name: "Запуски", exact: true }),
      ).toHaveAttribute("href", "/projects/project_cards_0/runs");
      const launch = published.getByRole("link", {
        name: "Запустить",
        exact: true,
      });
      const launchHref = await launch.getAttribute("href");
      if (!launchHref) throw new Error("Workflow launch route is unavailable");
      const launchUrl = new URL(launchHref, "https://kodex.test");
      expect(launchUrl.pathname).toBe("/projects/project_cards_0/runs/new");
      expect([...launchUrl.searchParams.entries()].sort()).toEqual([
        ["targetRef", "workflow_published"],
        ["targetType", "WORKFLOW"],
      ]);
      await launch.click();
      await expect(page.getByLabel("Последний переход")).toHaveText(
        launchUrl.pathname + launchUrl.search,
      );

      // Проверяется реальная геометрия карточек внутри оснастки, не layout production каталога.
      for (const card of await page
        .locator(".project-list__item, .agent-card, .workflow-card")
        .all()) {
        const box = await card.boundingBox();
        expect(box).not.toBeNull();
        if (!box) throw new Error("Card geometry is unavailable");
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(width + 1);
        expect(
          await card.evaluate(
            (element) => element.scrollWidth <= element.clientWidth + 1,
          ),
        ).toBe(true);
      }
      if (scope === "global") {
        const first = await projects.nth(0).boundingBox();
        const second = await projects.nth(1).boundingBox();
        if (!first || !second)
          throw new Error("Project card geometry is unavailable");
        if (width === 390)
          expect(second.y).toBeGreaterThanOrEqual(first.y + first.height);
        else expect(Math.abs(second.y - first.y)).toBeLessThanOrEqual(1);
      }
      await page.screenshot({
        path: testInfo.outputPath(`cards-${scope}-${String(width)}.png`),
        fullPage: true,
      });
      await page
        .getByRole("button", { name: "Обновить проекцию", exact: true })
        .click();
      await expect(runLink).toHaveAttribute("href", "/runs/run_cards_second");
      await expect(running.locator(".agent-card__activity")).toContainText(
        "Готовит итоговый отчёт",
      );
      await runLink.click();
      await expect(page.getByLabel("Последний переход")).toHaveText(
        "/runs/run_cards_second",
      );
      await page
        .getByRole("button", { name: "Убрать ссылку запуска", exact: true })
        .click();
      await expect(runLink).toHaveCount(0);
      await expect(running.locator(".agent-card__activity")).toHaveCount(0);
      await expect(
        running.locator('.status-badge[data-state="RUNNING"]'),
      ).toHaveCount(1);
      expect(failures).toEqual([]);
    });
  }
}
