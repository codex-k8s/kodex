import { expect, test } from "@playwright/test";

import type {
  RoleImageArtifact,
  RoleImageRecipeDetail,
} from "../src/shared/api/generated/openapi/types.gen";

for (const width of [1440, 390]) {
  for (const terminal of ["PROMOTED", "REJECTED"] as const) {
    test(`помощник восстанавливает продвижение образа ${String(width)}px → ${terminal}`, async ({
      page,
    }) => {
      await page.setViewportSize({
        width,
        height: width === 390 ? 844 : 900,
      });
      const pageErrors: string[] = [];
      page.on("pageerror", (error) => pageErrors.push(error.name));
      const now = "2026-09-25T00:00:00Z";
      const digest = "a".repeat(64);
      const artifact: RoleImageArtifact = {
        ref: "artifact_synthetic_image",
        version: 1,
        recipeRef: "recipe_synthetic_image",
        recipeGeneration: 1,
        buildRef: "build_synthetic_image",
        manifestDigest: `sha256:${digest}`,
        provenanceSha256: digest,
        admissionVerdict: "ACCEPTED",
        promotionState: "PENDING",
        promotionRequested: true,
        tools: [],
      };
      let promotionState: RoleImageArtifact["promotionState"] = "PENDING";
      let reads = 0;
      let mutations = 0;
      await page.route("**/*", async (route) => {
        const request = route.request();
        const url = new URL(request.url());
        if (url.origin !== "https://kodex.test") {
          await route.abort();
          throw new Error("Unexpected synthetic origin");
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
          url.pathname ===
          "/api/v1/projects/project_synthetic_image/role-image-recipes/recipe_synthetic_image"
        ) {
          if (request.method() !== "GET") {
            mutations += 1;
            await route.abort();
            return;
          }
          reads += 1;
          const candidate = { ...artifact, promotionState };
          const promoted = promotionState === "PROMOTED";
          const detail: RoleImageRecipeDetail = {
            recipe: {
              ref: "recipe_synthetic_image",
              version: 1,
              projectRef: "project_synthetic_image",
              roleDefinitionRef: "role_synthetic_image",
              name: "Проверочный образ",
              state: "ACTIVE",
              environment: { environmentKey: "standard" },
              sourceAvailable: true,
              generation: 1,
              promotedImageReady: promoted,
              nextActions: [],
              createdAt: now,
              updatedAt: now,
            },
            builds: [
              {
                ref: "build_synthetic_image",
                version: 1,
                recipeRef: "recipe_synthetic_image",
                recipeGeneration: 1,
                sourceAvailable: true,
                attempt: 1,
                stage: "COMPLETED",
                progressPercent: 100,
                createdAt: now,
                updatedAt: now,
              },
            ],
            promotionCandidate: candidate,
            ...(promoted ? { activeArtifact: candidate } : {}),
          };
          await route.fulfill({ json: detail });
          return;
        }
        if (url.pathname.startsWith("/api/")) {
          await route.abort();
          throw new Error(`Unexpected synthetic API ${url.pathname}`);
        }
        const response = await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        });
        await route.fulfill({ response });
      });

      await page.goto(
        "https://kodex.test/e2e/fixtures/assistant-role-image-build.html",
      );
      const card = page.locator(".assistant-build-card");
      const state = card.locator(".assistant-build-card__state").last();
      await expect(state.locator('[data-state="QUEUED"]')).toBeVisible();
      await expect(card).toContainText("Состояние обновляется автоматически.");
      await page.reload({ waitUntil: "domcontentloaded" });
      await expect.poll(() => reads).toBeGreaterThanOrEqual(2);
      await expect(state.locator('[data-state="QUEUED"]')).toBeVisible();

      promotionState = "CLAIMED";
      await card.getByRole("button", { name: "Обновить" }).click();
      await expect(state.locator('[data-state="PROMOTING"]')).toBeVisible();

      promotionState = terminal;
      await card.getByRole("button", { name: "Обновить" }).click();
      await expect(
        state.locator(
          `[data-state="${terminal === "PROMOTED" ? "PROMOTED" : "FAILED"}"]`,
        ),
      ).toBeVisible();
      if (terminal === "PROMOTED") {
        await expect(card).toContainText(
          "Образ опубликован и готов к использованию.",
        );
      } else {
        await expect(card).toContainText("Публикация не завершилась.");
      }
      expect(mutations).toBe(0);
      expect(pageErrors).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
    });
  }
}
