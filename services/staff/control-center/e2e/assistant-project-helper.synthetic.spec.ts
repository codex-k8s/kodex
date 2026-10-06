import { expect, test } from "@playwright/test";
import { overlaySchemaFixture } from "../src/test-utils/runtime-catalog-fixture";
import {
  helperPins,
  helperPolicy,
  projectHelperPlan,
} from "./fixtures/assistant-project-helper";
import type {
  Agent,
  AgentRuntimeConfigurationView,
  AssistantPlanOperationInput,
  RuntimeEnvironmentSet,
  RoleImageArtifact,
  RoleImageRecipe,
  RuntimeEnvironmentDraft,
} from "../src/shared/api/generated/openapi/types.gen";

const now = "2026-10-04T00:00:00Z";
const digest = helperPins.runtimeEnvironmentDigest;
const owner = {
  scopeKind: "PROJECT" as const,
  organizationRef: helperPins.organizationRef,
  projectRef: helperPins.projectRef,
};
const artifact: RoleImageArtifact = {
  ...owner,
  ref: "imgart_project_helper",
  version: 1,
  recipeRef: "recipe_project_helper",
  recipeGeneration: 1,
  buildRef: "build_project_helper",
  manifestDigest: `sha256:${digest}`,
  provenanceSha256: digest,
  promotedReference: `registry.invalid/helper@sha256:${digest}`,
  admissionVerdict: "ACCEPTED",
  promotionState: "PROMOTED",
  promotionRequested: true,
  declaredTools: [],
  verifiedToolInventory: {
    status: "UNAVAILABLE",
    sha256: "",
    imageDigest: "",
    provenanceSha256: "",
    platforms: [],
  },
};
const recipe: RoleImageRecipe = {
  ...owner,
  ref: artifact.recipeRef,
  version: 1,
  roleDefinitionRef: "role_project_helper",
  name: "Образ помощника проекта",
  state: "ACTIVE",
  environment: { environmentKey: "standard" },
  sourceAvailable: true,
  generation: 1,
  promotedImageReady: true,
  activeImageArtifactRef: artifact.ref,
  promotedImageReference: artifact.promotedReference,
  createdAt: now,
  updatedAt: now,
  nextActions: [],
};
const environment: RuntimeEnvironmentSet = {
  ...owner,
  ref: "env_project_helper",
  version: 5,
  name: "Рабочая среда проектного помощника",
  description: "Настройка из общесистемного диалога",
  state: "ACTIVE",
  ready: true,
  readinessBlockers: [],
  nextActions: ["UPDATE"],
  updatedAt: now,
  currentVersion: {
    ref: helperPins.runtimeEnvironmentVersionRef,
    version: 1,
    revision: 1,
    digest,
    createdAt: now,
    values: [],
    secretDescriptors: [],
    tools: [],
    image: {
      artifactRef: artifact.ref,
      recipeRef: artifact.recipeRef,
      recipeGeneration: 1,
      reference: artifact.promotedReference ?? "",
      digest,
    },
    policy: {
      resources: helperPolicy.resources,
      volumes: [],
      network: {
        denyByDefault: true,
        webAccess: helperPolicy.webAccess,
        egress: [
          { destination: "DNS", protocol: "TCP", port: 53 },
          { destination: "DNS", protocol: "UDP", port: 53 },
          { destination: "PROVIDER_PROXY", protocol: "TCP", port: 8084 },
          { destination: "RUNTIME_CALLBACK", protocol: "TCP", port: 8444 },
        ],
      },
      kubernetesAccess: { kind: "NONE", namespace: "kodex-runtime" },
      resourcesDigest: digest,
      volumesDigest: digest,
      networkDigest: digest,
      rbacDigest: digest,
    },
  },
};
const agent: Agent = {
  ref: helperPins.projectAssistantRef,
  projectRef: helperPins.projectRef,
  version: helperPins.agentVersion,
  name: "Помощник проекта",
  purpose: "Помощь участникам проекта",
  roleDescription: "Проектный помощник",
  state: "READY",
  enabled: true,
  system: false,
  runtimeRef: "runtime_project_helper",
  runtimeName: "Модель помощника",
  runtimeReady: true,
  capabilities: [],
  integrations: [],
  knowledgeArtifactRefs: [],
  nextActions: ["EDIT"],
  updatedAt: now,
  draftInstructions: {
    ref: "instruction_project_helper",
    version: 1,
    revision: 1,
    state: "DRAFT",
    content:
      "Помогай участникам проекта и проверяй результат перед завершением задачи.",
    validationMessages: [],
    createdAt: now,
  },
};
const bindingEnvironment: RuntimeEnvironmentSet = {
  ...environment,
  ref: "env_project_helper_next",
  name: "Следующее окружение помощника",
  version: 1,
  currentVersion: {
    ...environment.currentVersion,
    ref: "envver_project_helper_next",
  },
};
const draft: RuntimeEnvironmentDraft = {
  ...owner,
  ref: "envdraft_project_helper",
  version: 1,
  environmentRef: environment.ref,
  expectedEnvironmentVersion: environment.version,
  baseVersionRef: helperPins.runtimeEnvironmentVersionRef,
  baseRevision: 1,
  savedAt: now,
  state: "DRAFT",
  specification: {
    name: environment.name,
    description: environment.description,
    imageArtifactRef: artifact.ref,
    tools: [],
    values: [],
    secretBindings: [],
    policy: helperPolicy,
  },
  diagnostics: [],
};
const runtimeView: AgentRuntimeConfigurationView = {
  configuration: {
    ref: "configuration_project_helper",
    version: 1,
    agentRef: agent.ref,
    runtimeProfileRef: agent.runtimeRef,
    provider: "fixture",
    model: "model-fixture",
    digest,
    createdAt: now,
    providerPolicy: {
      ref: "provider_policy_helper",
      version: 1,
      mode: "FIXED",
      accountCandidates: [],
      digest,
      createdAt: now,
    },
  },
  publishedOverlay: {
    ref: "overlay_project_helper",
    version: 1,
    revision: 1,
    state: "PUBLISHED",
    content: "",
    digest,
    validationMessages: [],
    createdAt: now,
  },
  environmentBinding: {
    ref: helperPins.runtimeEnvironmentBindingRef,
    version: 1,
    agentRef: agent.ref,
    environmentRef: environment.ref,
    versionRef: helperPins.runtimeEnvironmentVersionRef,
    digest,
  },
  environment,
  safeEffectiveConfig: "",
  agentVersion: agent.version,
  skillBindings: [],
  memoryBindings: [],
  overlaySchema: overlaySchemaFixture,
};

for (const width of [1440, 390])
  for (const kind of ["ENV", "INSTR", "BIND"])
    for (const state of ["DRAFT", "APPLIED"])
      for (const source of ["SYSTEM", "CONTEXT"])
        test(`Общесистемный план ${source} настраивает ${kind} проекта ${state} ${String(width)}px`, async ({
          page,
        }, testInfo) => {
          await page.setViewportSize({ width, height: 1000 });
          const locale = kind === "INSTR" && source === "CONTEXT" ? "en" : "ru";
          const errors: string[] = [],
            calls: string[] = [],
            mutations: string[] = [];
          page.on("pageerror", (error) => errors.push(error.message));
          page.on("console", (message) => {
            if (["warning", "error"].includes(message.type()))
              errors.push(message.text());
          });
          await page.route("**/*", async (route) => {
            const request = route.request(),
              url = new URL(request.url());
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
            if (url.pathname.startsWith("/api/")) {
              calls.push(url.pathname);
              if (request.method() !== "GET")
                mutations.push(`${request.method()} ${url.pathname}`);
              if (
                url.pathname ===
                `/api/v1/projects/${helperPins.projectRef}/assistant`
              )
                return route.fulfill({
                  json: {
                    ref: helperPins.assistantProfileRef,
                    projectRef: helperPins.projectRef,
                    agentRef: agent.ref,
                    name: agent.name,
                    state: "ACTIVE",
                    version: 1,
                    createdAt: now,
                    updatedAt: now,
                  },
                });
              if (url.pathname === `/api/v1/agents/${agent.ref}`)
                return route.fulfill({
                  json: {
                    ...agent,
                    version: state === "APPLIED" && kind !== "ENV" ? 4 : 3,
                  },
                });
              if (
                url.pathname ===
                `/api/v1/agents/${agent.ref}/runtime-configuration`
              )
                return route.fulfill({
                  json: {
                    ...runtimeView,
                    agentVersion: 4,
                    environment: bindingEnvironment,
                    environmentBinding: {
                      ...runtimeView.environmentBinding,
                      version: 2,
                      environmentRef: bindingEnvironment.ref,
                      versionRef: bindingEnvironment.currentVersion.ref,
                    },
                  },
                });
              if (
                url.pathname ===
                `/api/v1/runtime-environments/${environment.ref}`
              )
                return route.fulfill({ json: environment });
              if (
                url.pathname ===
                `/api/v1/runtime-environments/${bindingEnvironment.ref}`
              )
                return route.fulfill({ json: bindingEnvironment });
              if (
                url.pathname ===
                `/api/v1/runtime-environment-drafts/${draft.ref}`
              )
                return route.fulfill({ json: draft, headers: { ETag: '"1"' } });
              if (
                url.pathname ===
                `/api/v1/projects/${helperPins.projectRef}/runtime-environments`
              )
                return route.fulfill({
                  json: { items: [environment, bindingEnvironment] },
                });
              if (
                url.pathname ===
                `/api/v1/projects/${helperPins.projectRef}/role-image-recipes`
              )
                return route.fulfill({ json: { items: [recipe], total: 1 } });
              if (
                url.pathname ===
                `/api/v1/projects/${helperPins.projectRef}/role-image-recipes/${recipe.ref}`
              )
                return route.fulfill({
                  json: { recipe, builds: [], activeArtifact: artifact },
                });
              if (
                url.pathname ===
                `/api/v1/projects/${helperPins.projectRef}/runtime-secrets`
              )
                return route.fulfill({ json: { items: [], total: 0 } });
              await route.abort();
              errors.push(`Unexpected synthetic API ${url.pathname}`);
              return;
            }
            return route.fulfill({
              response: await route.fetch({
                url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
              }),
            });
          });
          await page.goto(
            `https://kodex.test/e2e/fixtures/assistant-environment.html?helper=${kind}&state=${state}&source=${source}&locale=${locale}`,
          );
          await expect(page.getByTestId("helper-plan-project")).toHaveText(
            source === "CONTEXT"
              ? "prj_source_context"
              : "SYSTEM_WITHOUT_PROJECT",
          );
          if (state === "DRAFT") {
            const save = page.getByRole("button", {
              name:
                locale === "en"
                  ? "Save new revision"
                  : "Сохранить новую ревизию",
              exact: true,
            });
            await expect(save).toBeVisible();
            if (kind === "ENV") {
              await expect(
                page.locator(".assistant-environment-tools"),
              ).toBeVisible();
              await page
                .locator(".assistant-environment-revision input")
                .fill("Изменённая среда помощника");
            }
            if (kind === "INSTR") {
              await expect(page.locator(".shared-code-editor")).toBeVisible();
              await expect(page.locator(".shared-code-editor")).toContainText(
                locale === "en"
                  ? "Assistant instructions"
                  : "Инструкции помощнику",
              );
              await expect(
                page.locator(".shared-code-editor .cm-content"),
              ).toHaveAttribute("contenteditable", "true");
            }
            if (kind === "BIND") {
              await expect(
                page.locator(".assistant-agent-environment-binding-form"),
              ).toContainText(agent.name);
            }
            await save.click();
            const raw = await page.getByTestId("helper-saved").textContent();
            const saved = JSON.parse(
              raw ?? "[]",
            ) as AssistantPlanOperationInput[];
            expect(saved).toHaveLength(1);
            const op = saved[0];
            expect(op).toBeDefined();
            if (!op) throw new Error("Missing saved operation");
            for (const key of Object.keys(helperPins)) {
              expect(op.parameters[key]).toBe(
                helperPins[key as keyof typeof helperPins],
              );
              expect(op.before[key]).toBe(
                helperPins[key as keyof typeof helperPins],
              );
              expect(op.after[key]).toBe(
                helperPins[key as keyof typeof helperPins],
              );
            }
            expect(op.target).toEqual(
              projectHelperPlan(kind, false).operations[0]?.target,
            );
          } else {
            const card = page.locator(
              kind === "ENV"
                ? ".assistant-environment-card"
                : kind === "INSTR"
                  ? ".instruction-draft-card"
                  : ".assistant-agent-environment-binding-card",
            );
            await expect(card.getByRole("link")).toHaveAttribute(
              "href",
              kind === "ENV"
                ? `/projects/${helperPins.projectRef}/environments/${environment.ref}?draftRef=${draft.ref}&assistantForm=1`
                : `/projects/${helperPins.projectRef}/agents/${agent.ref}?tab=${kind === "INSTR" ? "instructions" : "environment"}&assistantForm=1`,
            );
            expect(calls).toContain(
              `/api/v1/projects/${helperPins.projectRef}/assistant`,
            );
            expect(calls).toContain(`/api/v1/agents/${agent.ref}`);
            if (kind === "INSTR") {
              await expect(card).toContainText(
                locale === "en"
                  ? "Assistant instruction draft"
                  : "Черновик инструкций помощника",
              );
              await expect(
                page.locator(".assistant-plan-friendly"),
              ).toContainText(
                locale === "en"
                  ? "The assistant instruction draft was saved."
                  : "Черновик инструкций помощника сохранён.",
              );
            }
            if (kind === "ENV")
              await expect(
                page.locator(".assistant-environment-revision"),
              ).toContainText("Черновик новой ревизии окружения сохранён.");
          }
          expect(calls.some((path) => path.includes("/projects//"))).toBe(
            false,
          );
          expect(calls.some((path) => path.includes("/organization/"))).toBe(
            false,
          );
          expect(mutations).toEqual([]);
          expect(errors).toEqual([]);
          expect(
            await page.evaluate(
              () => document.documentElement.scrollWidth <= innerWidth,
            ),
          ).toBe(true);
          await page.screenshot({
            path: testInfo.outputPath(
              `assistant-helper-${kind}-${state}-${String(width)}.png`,
            ),
            fullPage: true,
          });
        });

for (const tamper of ["project", "organization", "locator", "receipt"])
  test(`Проектная цель закрыто отклоняется при ${tamper}`, async ({ page }) => {
    const errors: string[] = [],
      calls: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
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
      if (url.pathname.startsWith("/api/")) {
        calls.push(url.pathname);
        return route.abort();
      }
      return route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto(
      `https://kodex.test/e2e/fixtures/assistant-environment.html?helper=INSTR&state=APPLIED&tamper=${tamper}`,
    );
    await expect(page.getByTestId("helper-plan-project")).toHaveText(
      "SYSTEM_WITHOUT_PROJECT",
    );
    await expect(page.locator(".instruction-draft-card")).toHaveCount(0);
    await expect(
      page.getByRole("button", {
        name: "Сохранить новую ревизию",
        exact: true,
      }),
    ).toHaveCount(0);
    expect(calls).toEqual([]);
    expect(errors).toEqual([]);
  });
