import { expect, test } from "@playwright/test";
const now = "2026-10-04T00:00:00Z",
  digest = "a".repeat(64);
const artifact = {
  ref: "artifact_fixture",
  projectRef: "",
  scopeKind: "ORGANIZATION",
  organizationRef: "org_fixture",
  version: 1,
  recipeRef: "recipe_fixture",
  recipeGeneration: 1,
  buildRef: "build_fixture",
  manifestDigest: `sha256:${digest}`,
  provenanceSha256: digest,
  admissionVerdict: "ACCEPTED",
  promotionState: "PROMOTED",
  promotionRequested: true,
  promotedReference: `example.invalid/assistant@sha256:${digest}`,
  tools: [{ name: "git", version: "2.53" }],
};
const recipe = {
  ref: artifact.recipeRef,
  projectRef: "",
  scopeKind: "ORGANIZATION",
  organizationRef: "org_fixture",
  version: 1,
  name: "Среда помощника",
  roleDefinitionRef: "role_fixture",
  environment: { environmentKey: "standard" },
  generation: 1,
  promotedImageReady: true,
  activeImageArtifactRef: artifact.ref,
  promotedImageReference: artifact.promotedReference,
  state: "ACTIVE",
  sourceAvailable: false,
  nextActions: [],
  createdAt: now,
  updatedAt: now,
};
const resources = {
  cpuRequestMilli: 500,
  cpuLimitMilli: 2000,
  memoryRequestMib: 512,
  memoryLimitMib: 2048,
  ephemeralStorageRequestMib: 512,
  ephemeralStorageLimitMib: 4096,
};
const environment = {
  ref: "environment_fixture",
  scopeKind: "ORGANIZATION",
  organizationRef: "org_fixture",
  projectRef: "",
  version: 2,
  name: "Окружение помощника",
  description:
    "Образ, инструменты и защищённые ресурсы общесистемного помощника",
  state: "ACTIVE",
  ready: true,
  readinessBlockers: [],
  nextActions: ["UPDATE"],
  updatedAt: now,
  currentVersion: {
    ref: "environment_version_fixture",
    version: 2,
    revision: 2,
    createdAt: now,
    digest,
    image: {
      artifactRef: artifact.ref,
      recipeRef: artifact.recipeRef,
      recipeGeneration: 1,
      reference: artifact.promotedReference,
      digest,
    },
    values: [{ name: "APP_MODE", value: "development" }],
    secretDescriptors: [],
    tools: [
      {
        name: "Git",
        command: "git",
        description: "Проверка рабочего дерева",
        usageHint: "status",
      },
    ],
    policy: {
      resources,
      volumes: [],
      network: {
        webAccess: { mode: "NONE", rules: [] },
        denyByDefault: true,
        egress: [
          { destination: "DNS", protocol: "TCP", port: 53 },
          { destination: "DNS", protocol: "UDP", port: 53 },
          { destination: "PROVIDER_PROXY", protocol: "TCP", port: 8084 },
          { destination: "RUNTIME_CALLBACK", protocol: "TCP", port: 8444 },
        ],
      },
      kubernetesAccess: "NONE",
    },
  },
};
for (const width of [1440, 390])
  for (const locale of ["ru", "en"])
    test(`Общесистемное окружение ${String(width)}px ${locale}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 1000 });
      const errors: string[] = [],
        calls: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      page.on("console", (message) => {
        if (["warning", "error"].includes(message.type()))
          errors.push(message.text());
      });
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (url.pathname === "/config/runtime-config.json")
          return route.fulfill({
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
        if (
          url.pathname === "/api/v1/agents/agent_fixture/runtime-configuration"
        ) {
          calls.push(url.pathname);
          return route.fulfill({
            json: {
              configuration: { agentRef: "agent_fixture" },
              environmentBinding: {
                agentRef: "agent_fixture",
                environmentRef: environment.ref,
              },
              environment,
            },
          });
        }
        if (
          url.pathname ===
          "/api/v1/organization/role-image-recipes/recipe_fixture"
        ) {
          calls.push(url.pathname);
          return route.fulfill({
            json: { recipe, builds: [], activeArtifact: artifact },
          });
        }
        if (url.pathname.startsWith("/api/"))
          throw new Error(`Unexpected synthetic request ${url.pathname}`);
        await route.fulfill({
          response: await route.fetch({
            url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
          }),
        });
      });
      await page.goto(
        `/e2e/fixtures/assistant-environment.html?locale=${locale}`,
      );
      await expect(
        page.locator(".assistant-environment-settings"),
      ).toBeVisible();
      await expect(
        page.locator(".assistant-environment-settings__image"),
      ).toContainText("Среда помощника");
      await expect(page.locator(".environment-draft-actions")).toBeVisible();
      await expect(page.locator(".runtime-resource-links a")).toHaveCount(4);
      await expect(
        page.locator(".assistant-environment-settings__advanced"),
      ).not.toHaveAttribute("open");
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        )
        .toBe(true);
      expect(calls).toEqual([
        "/api/v1/agents/agent_fixture/runtime-configuration",
        "/api/v1/organization/role-image-recipes/recipe_fixture",
      ]);
      await page.screenshot({
        path: testInfo.outputPath(
          `assistant-environment-${String(width)}-${locale}.png`,
        ),
        fullPage: true,
      });
      expect(errors).toEqual([]);
    });
