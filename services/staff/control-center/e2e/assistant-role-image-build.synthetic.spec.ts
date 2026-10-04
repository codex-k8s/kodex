import { expect, test } from "@playwright/test";
import { providerUsageFixture } from "../src/test-utils/provider-usage-fixture";

import type {
  RoleImageArtifact,
  RoleImageRecipeDetail,
  ProviderAccount,
  ProviderAccountUsageContext,
  RoleEnvironmentPage,
  ModelCapabilityPage,
} from "../src/shared/api/generated/openapi/types.gen";

for (const width of [1440, 390]) {
  for (const scope of ["PROJECT", "ORGANIZATION"] as const) {
    for (const terminal of ["PROMOTED", "REJECTED"] as const) {
      test(`помощник восстанавливает продвижение образа ${scope} ${String(width)}px → ${terminal}`, async ({
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
          projectRef: scope === "PROJECT" ? "project_synthetic_image" : "",
          scopeKind: scope,
          organizationRef: "org_synthetic",
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
            (scope === "PROJECT"
              ? "/api/v1/projects/project_synthetic_image/role-image-recipes/recipe_synthetic_image"
              : "/api/v1/organization/role-image-recipes/recipe_synthetic_image")
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
                scopeKind: scope,
                organizationRef: "org_synthetic",
                ref: "recipe_synthetic_image",
                version: 1,
                projectRef:
                  scope === "PROJECT" ? "project_synthetic_image" : "",
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
                  projectRef:
                    scope === "PROJECT" ? "project_synthetic_image" : "",
                  scopeKind: scope,
                  organizationRef: "org_synthetic",
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
          `https://kodex.test/e2e/fixtures/assistant-role-image-build.html?scope=${scope}`,
        );
        const card = page.locator(".assistant-build-card");
        await expect(
          card.getByRole("link", { name: "Открыть образ" }),
        ).toHaveAttribute(
          "href",
          scope === "PROJECT"
            ? "/projects/project_synthetic_image/role-images/recipe_synthetic_image?assistantForm=1"
            : "/organization/role-images/recipe_synthetic_image?assistantForm=1",
        );
        const state = card.locator(".assistant-build-card__state").last();
        await expect(state.locator('[data-state="QUEUED"]')).toBeVisible();
        await expect(card).toContainText(
          "Состояние обновляется автоматически.",
        );
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
}

for (const width of [1440, 390]) {
  for (const scope of ["PROJECT", "ORGANIZATION"] as const) {
    for (const editor of [
      "runtime",
      "image",
      "runtime-nonreasoning",
      "runtime-multi",
    ] as const) {
      test(`редактирование плана помощника ${editor} ${scope} ${String(width)}px без применения`, async ({
        page,
      }, testInfo) => {
        await page.setViewportSize({
          width,
          height: width === 390 ? 844 : 900,
        });
        const errors: string[] = [];
        page.on("pageerror", (error) => errors.push(error.message));
        page.on("console", (message) => {
          if (message.type() === "error") errors.push(message.text());
        });
        const unexpected: string[] = [];
        const mutations: string[] = [];
        let releaseModel: (() => void) | undefined;
        const modelReady = new Promise<void>((resolve) => {
          releaseModel = resolve;
        });
        await page.route("**/*", async (route) => {
          const request = route.request();
          const url = new URL(request.url());
          if (url.origin !== "https://kodex.test") {
            unexpected.push(url.origin);
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
          if (url.pathname.startsWith("/api/") && request.method() !== "GET") {
            mutations.push(url.pathname);
            await route.abort();
            return;
          }
          if (url.pathname === "/api/v1/role-environments") {
            const catalog: RoleEnvironmentPage = {
              items: [
                {
                  key: "standard",
                  nameMessageKey: "role-environments.standard.name",
                  descriptionMessageKey:
                    "role-environments.standard.description",
                  softwareMessageKeys: [],
                  platforms: [{ os: "linux", architecture: "amd64" }],
                  recommended: true,
                  available: true,
                  customInstallationAllowed: true,
                  dockerfileTemplate: "FROM alpine:3.22\nRUN echo ready\n",
                },
              ],
            };
            await route.fulfill({ json: catalog });
            return;
          }
          if (
            url.pathname.endsWith("/agents") &&
            scope === "PROJECT" &&
            editor === "image"
          ) {
            await route.fulfill({ json: { items: [], nextPageToken: "" } });
            return;
          }
          if (url.pathname === "/api/v1/runtime-selections") {
            await route.fulfill({
              json: {
                items: [
                  {
                    ref: "runtime_synthetic",
                    name: "Профиль каталога",
                    revision: "runtime-revision",
                    ready: true,
                    provider: "openai-codex",
                    model: "model-synthetic",
                  },
                ],
              },
            });
            return;
          }
          if (url.pathname.startsWith("/api/v1/provider-accounts")) {
            const context: ProviderAccountUsageContext = {
              purpose: "CONFIGURE",
              agentRef: "agent_synthetic",
              runtimeProfileRef: "runtime_synthetic",
              providerDefinitionKey: "openai-codex",
              model: url.searchParams.get("usageModel") ?? undefined,
              reasoningEffort:
                url.searchParams.get("usageReasoningEffort") ?? undefined,
            };
            expect(url.searchParams.get("usageAgentRef")).toBe(
              context.agentRef,
            );
            const account: ProviderAccount = {
              ref: url.pathname.endsWith("/pacc_secondary")
                ? "pacc_secondary"
                : "pacc_synthetic",
              version: 1,
              maximumConcurrentExecutions: 10,
              name: "Учётная запись каталога",
              definitionKey: "openai-codex",
              state: "AUTHORIZED",
              enabled: true,
              ready: true,
              externalAccountMasked: "fixture",
              createdAt: "2026-09-25T00:00:00Z",
              updatedAt: "2026-09-25T00:00:00Z",
              nextActions: [],
              usage: providerUsageFixture(context),
            };
            await route.fulfill({
              json: url.pathname.endsWith("/provider-accounts")
                ? { items: [account], nextPageToken: "", nextActions: [] }
                : account,
            });
            return;
          }
          if (url.pathname === "/api/v1/model-capabilities") {
            if (
              editor === "runtime-nonreasoning" &&
              url.searchParams.get("query") === "model-synthetic-new"
            )
              await modelReady;
            const usage = providerUsageFixture();
            const accountRef =
              url.searchParams.get("providerAccountRef") ?? "pacc_synthetic";
            const catalog: ModelCapabilityPage = {
              items: ["model-synthetic", "model-synthetic-new"].map((id) => ({
                id,
                providerDefinitionKey: "openai-codex",
                available: true,
                eligibleProviderAccountRefs: [accountRef],
                reasoningEfforts:
                  editor === "runtime-nonreasoning" &&
                  id === "model-synthetic-new"
                    ? []
                    : accountRef === "pacc_secondary"
                      ? ["high", "medium"]
                      : ["high", "low", "medium"],
                defaultReasoningEffort:
                  editor === "runtime-nonreasoning" &&
                  id === "model-synthetic-new"
                    ? ""
                    : accountRef === "pacc_secondary"
                      ? "medium"
                      : "low",
                readinessBlockers: [],
              })),
              total: 2,
              nextPageToken: "",
              catalogRevision: usage.catalogRevision,
              catalogDigest: usage.catalogDigest,
              catalogStatus: usage.catalogStatus,
            };
            await route.fulfill({ json: catalog });
            return;
          }
          if (url.pathname.startsWith("/api/")) {
            unexpected.push(url.pathname);
            await route.abort();
            return;
          }
          const response = await route.fetch({
            url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
          });
          await route.fulfill({ response });
        });
        await page.goto(
          `https://kodex.test/e2e/fixtures/assistant-role-image-build.html?scope=${scope}&editor=${editor}`,
        );
        if (editor.startsWith("runtime")) {
          const effort = page.locator('select[name$="-effort"]');
          await expect(effort).toBeEnabled();
          await expect(effort.locator("option")).toHaveText([
            ...(editor === "runtime-multi" ? ["low"] : []),
            "По умолчанию из каталога",
            ...(editor === "runtime-multi" ? [] : ["low"]),
            "medium",
            "high",
          ]);
          await effort.selectOption("high");
          await page
            .locator(".provider-model-selector")
            .locator("button.async-picker__trigger")
            .click();
          await page
            .getByRole("option", { name: "model-synthetic-new", exact: false })
            .click();
          if (editor === "runtime-nonreasoning") {
            await expect(effort).toBeDisabled();
            await expect(
              page.getByRole("button", { name: "Сохранить новую ревизию" }),
            ).toHaveCount(0);
            releaseModel?.();
            await expect(effort).toBeEnabled();
            await expect(effort).toHaveValue("high");
            await expect(effort.locator("option")).toHaveText([
              "high",
              "Модель без степени рассуждения",
            ]);
            await expect(
              page.getByRole("button", { name: "Сохранить новую ревизию" }),
            ).toHaveCount(0);
            await effort.selectOption("");
          }
        } else {
          if (scope === "PROJECT") {
            await expect(
              page.getByRole("button", { name: "Сохранить новую ревизию" }),
            ).toHaveCount(0);
            await expect(
              page.getByText("Сотрудник не найден в доступном каталоге"),
            ).toBeVisible();
            expect(mutations).toEqual([]);
            expect(unexpected).toEqual([]);
            expect(errors).toEqual([]);
            expect(
              await page.evaluate(
                () => document.documentElement.scrollWidth <= window.innerWidth,
              ),
            ).toBe(true);
            await page.screenshot({
              path: testInfo.outputPath(
                `assistant-plan-image-PROJECT-${String(width)}-closed.png`,
              ),
              fullPage: true,
            });
            return;
          }
          await page
            .locator('input[name^="assistant-role-image-name-"]')
            .fill("Образ помощника после правки");
        }
        const save = page.getByRole("button", {
          name: "Сохранить новую ревизию",
        });
        await expect(save).toBeVisible();
        await save.click();
        const saved = JSON.parse(
          (await page.getByTestId("saved-operations").textContent()) ?? "[]",
        ) as Array<{
          parameters: Record<string, unknown>;
          after: Record<string, unknown>;
          expectedVersion?: number;
          before: Record<string, unknown>;
          target: { kind: string; ref: string; version: number };
        }>;
        expect(saved).toHaveLength(1);
        const operation = saved[0];
        if (!operation) throw new Error("Missing saved operation");
        expect(operation.parameters.organizationRef).toBe("org_synthetic");
        expect(operation.after.organizationRef).toBe("org_synthetic");
        if (editor.startsWith("runtime")) {
          expect(operation.expectedVersion).toBe(3);
          expect(operation.parameters.reasoningEffort).toBe(
            editor === "runtime-nonreasoning" ? "" : "high",
          );
          expect(operation.parameters.model).toBe("model-synthetic-new");
          expect(operation.parameters.runtimeProfilePin).toEqual({
            ref: "runtime_synthetic",
            version: 7,
            runtimeRevision: "runtime-revision",
          });
          expect(operation.after.runtimeProfilePin).toEqual(
            operation.parameters.runtimeProfilePin,
          );
          expect(operation.before.runtimeProfilePin).toEqual(
            operation.parameters.runtimeProfilePin,
          );
          expect(operation.before.agentVersion).toBe(3);
          expect(operation.target).toMatchObject({
            kind: "AGENT",
            ref: "agent_synthetic",
            version: 3,
          });
          for (const field of [
            "agentRef",
            "assistantScope",
            "scopeKind",
            "organizationRef",
            "projectRef",
            "assistantProfileRef",
          ]) {
            expect(operation.after[field]).toEqual(operation.parameters[field]);
            expect(operation.before[field]).toEqual(
              operation.parameters[field],
            );
          }
          expect(operation.parameters.agentRef).toBe("agent_synthetic");
          expect(operation.parameters.assistantScope).toBe(
            scope === "ORGANIZATION" ? "SYSTEM" : "PROJECT",
          );
        } else {
          expect(operation.parameters.name).toBe(
            "Образ помощника после правки",
          );
          expect(operation.parameters.systemAssistantRef).toBe("agt_system");
          expect(operation.parameters).not.toHaveProperty("projectRef");
        }
        expect(mutations).toEqual([]);
        expect(unexpected).toEqual([]);
        expect(errors).toEqual([]);
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= window.innerWidth,
          ),
        ).toBe(true);
        await page.screenshot({
          path: testInfo.outputPath(
            `assistant-plan-${editor}-${scope}-${String(width)}.png`,
          ),
          fullPage: true,
        });
      });
    }
  }
}
