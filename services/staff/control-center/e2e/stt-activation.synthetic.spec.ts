import { expect, test } from "@playwright/test";

for (const width of [390, 2900]) {
  for (const scenario of ["first", "change", "stale", "unknown"] as const) {
    test(`synthetic: активация STT ${scenario} ${String(width)}px`, async ({
      page,
      context,
    }, info) => {
      await page.setViewportSize({ width, height: 900 });
      await context.addCookies([
        {
          name: "__Host-kodex-csrf",
          value: "s".repeat(64),
          domain: "kodex.test",
          path: "/",
          secure: true,
          sameSite: "Strict",
        },
      ]);
      const failures: string[] = [];
      page.on("pageerror", (error) => failures.push(error.message));
      let posts = 0,
        applied = false,
        version = 3,
        globalReads = 0;
      let forbidden = scenario === "first";
      const current = () => ({
        configurationRef: applied
          ? "configuration_target"
          : "configuration_previous",
        revisionRef: applied ? "revision_target" : "revision_previous",
        revision: applied ? 2 : 7,
        ready: false,
        readinessBlockers: ["STT_PROVIDER_ACCOUNT_INELIGIBLE"],
      });
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
              requestTimeoutMs: 1000,
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
        if (url.pathname === "/api/v1/system-stt-configuration") {
          globalReads++;
          await route.fulfill(
            !applied && scenario !== "change"
              ? { status: 404, json: { status: 404, code: "NOT_FOUND" } }
              : { json: current() },
          );
          return;
        }
        if (url.pathname.endsWith("/revisions")) {
          const previous = url.pathname.includes("configuration_previous");
          await route.fulfill({
            json: {
              configuration: {
                ref: previous
                  ? "configuration_previous"
                  : "configuration_target",
                kind: "SYSTEM_STT",
                managedBy: "UI",
                version,
                name: previous
                  ? "Прежнее распознавание"
                  : "Русское распознавание",
                source: "UI",
                sourceRevision: "",
                updatedAt: "2026-09-07T00:00:00Z",
              },
              items: [],
              total: 0,
            },
          });
          return;
        }
        if (url.pathname.endsWith("/impact")) {
          if (forbidden) {
            await route.fulfill({
              status: 403,
              json: { status: 403, code: "FORBIDDEN" },
            });
            return;
          }
          const previous = url.pathname.includes("configuration_previous");
          await route.fulfill({
            json: {
              configurationRef: previous
                ? "configuration_previous"
                : "configuration_target",
              targetRevisionRef: previous
                ? "revision_previous"
                : "revision_target",
              digest: "a".repeat(64),
              total: previous || applied ? 1 : 0,
              consumers:
                previous || applied
                  ? [
                      {
                        kind: "STT_SERVICE",
                        ref: "stt-tts-service",
                        revisionRef: previous
                          ? "revision_previous"
                          : "revision_target",
                        version: previous ? 19 : 1,
                      },
                    ]
                  : [],
            },
          });
          return;
        }
        if (
          url.pathname ===
          "/api/v1/system-stt-configurations/configuration_target/revisions/revision_target/consumer-bindings"
        ) {
          posts++;
          expect(route.request().method()).toBe("POST");
          expect(route.request().headers()["if-match"]).toBe(
            `"${String(version)}"`,
          );
          expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
          expect(route.request().headers()["idempotency-key"]).toBeTruthy();
          expect(route.request().postDataJSON()).toEqual({
            impactDigest: "a".repeat(64),
            consumers: [
              scenario === "change"
                ? {
                    kind: "STT_SERVICE",
                    ref: "stt-tts-service",
                    revisionRef: "revision_previous",
                    version: 19,
                    expectedAbsent: false,
                  }
                : {
                    kind: "STT_SERVICE",
                    ref: "stt-tts-service",
                    expectedAbsent: true,
                  },
            ],
          });
          if (scenario === "stale" && posts === 1) {
            version++;
            await route.fulfill({
              status: 412,
              json: { status: 412, code: "VERSION_OR_STATE_CONFLICT" },
            });
            return;
          }
          if (scenario === "unknown") {
            await route.abort("timedout");
            return;
          }
          applied = true;
          await route.fulfill({ json: {} });
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
      await page.goto("/e2e/fixtures/stt-activation.html");
      if (forbidden) {
        await page
          .getByRole("button", { name: "Подготовить активацию" })
          .click();
        await expect(page.getByRole("alert")).toBeVisible();
        await expect(
          page.getByRole("button", { name: "Активировать эту ревизию" }),
        ).toHaveCount(0);
        expect(globalReads).toBe(0);
        expect(posts).toBe(0);
        forbidden = false;
      }
      await page.getByRole("button", { name: "Подготовить активацию" }).click();
      await expect(
        page.getByText(
          scenario === "change"
            ? /Сменить «Прежнее распознавание», ревизия 7/
            : /Будет создана первая привязка/,
        ),
      ).toBeVisible();
      await page.getByRole("button", { name: "Отменить выбор" }).click();
      expect(posts).toBe(0);
      await page.getByRole("button", { name: "Подготовить активацию" }).click();
      await page
        .getByRole("button", { name: "Активировать эту ревизию" })
        .click();
      if (scenario === "stale") {
        await expect(page.getByText(/Данные изменились/)).toBeVisible();
        expect(posts).toBe(1);
        await page.getByRole("button", { name: "Обновить редактор" }).click();
        await page
          .getByRole("button", { name: "Подготовить активацию" })
          .click();
        await page
          .getByRole("button", { name: "Активировать эту ревизию" })
          .click();
      }
      if (scenario === "unknown") {
        await expect(
          page
            .getByRole("alert")
            .filter({ hasText: "Исход команды неизвестен" }),
        ).toBeVisible();
        await page.getByRole("button", { name: "Закрыть / открыть" }).click();
        await page.getByRole("button", { name: "Закрыть / открыть" }).click();
        await expect(
          page.getByRole("button", { name: "Подготовить активацию" }),
        ).toBeDisabled();
        await page
          .getByRole("button", { name: "Проверить активную конфигурацию" })
          .click();
        await expect(
          page.getByRole("button", { name: "Подготовить активацию" }),
        ).toBeDisabled();
        applied = true;
        await page
          .getByRole("button", { name: "Проверить активную конфигурацию" })
          .click();
        await expect(page.getByText(/Это чтение состояния/)).toBeVisible();
      }
      await expect(page.getByTestId("stt-readiness")).toContainText(
        "распознавание пока не готово",
      );
      expect(posts).toBe(scenario === "stale" ? 2 : 1);
      expect(globalReads).toBeGreaterThan(2);
      expect(failures).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: info.outputPath(`stt-${scenario}-${String(width)}.png`),
        fullPage: true,
      });
    });
  }
}
