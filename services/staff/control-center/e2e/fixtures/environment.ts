import { expect, type Page } from "@playwright/test";
import type {
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentDraftSpecification,
  RuntimeEnvironmentSet,
  RoleImageRecipeDetail,
  RuntimeEnvironmentPolicyInput,
  RuntimeEnvironmentConsumer,
  RuntimeEnvironmentRebindInput,
  RevisionImpactPlan,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function installEnvironmentFixture(
  page: Page,
  projectRef: string,
) {
  const events: string[] = [];
  const failures: string[] = [];
  let inspectorFailure:
    | { endpoint: "readiness" | "agents"; status: 404 | 503 }
    | undefined;
  let publicationPlan: RevisionImpactPlan | undefined;
  const now = "2026-09-05T00:00:00Z";
  const digest = "b".repeat(64);
  const policy: RuntimeEnvironmentPolicyInput = {
    resources: {
      cpuRequestMilli: 2000,
      cpuLimitMilli: 2000,
      memoryRequestMib: 4096,
      memoryLimitMib: 4096,
      ephemeralStorageRequestMib: 1024,
      ephemeralStorageLimitMib: 4096,
    },
    volumes: [],
    networkDestinations: ["DNS", "PROVIDER_PROXY", "RUNTIME_CALLBACK"],
    kubernetesAccess: "NONE",
  };
  const empty: RuntimeEnvironmentDraftSpecification = {
    name: "",
    description: "",
    imageArtifactRef: "",
    tools: [],
    values: [],
    secretBindings: [],
  };
  let draft: RuntimeEnvironmentDraft = {
    ref: "draft_synthetic_environment",
    projectRef,
    version: 1,
    expectedEnvironmentVersion: 0,
    savedAt: "2026-09-05T00:00:00Z",
    state: "DRAFT",
    specification: empty,
    diagnostics: [],
  };
  const prepared: RuntimeEnvironmentDraft = {
    ...draft,
    ref: "draft_prepared_environment",
    specification: {
      ...empty,
      name: "Окружение synthetic",
      imageArtifactRef: "artifact_synthetic_image",
      policy,
    },
  };
  const environment: RuntimeEnvironmentSet = {
    ref: "environment_synthetic",
    projectRef,
    version: 1,
    name: prepared.specification.name,
    description: "",
    state: "ACTIVE",
    ready: true,
    readinessBlockers: [],
    nextActions: ["UPDATE"],
    updatedAt: now,
    currentVersion: {
      ref: "environment_revision_synthetic",
      version: 1,
      revision: 1,
      values: [],
      secretDescriptors: [],
      tools: [],
      digest,
      createdAt: now,
      image: {
        artifactRef: "artifact_synthetic_image",
        recipeRef: "recipe_synthetic_image",
        recipeGeneration: 1,
        reference: `registry.invalid/test@sha256:${digest}`,
        digest,
      },
      policy: {
        resources: policy.resources,
        volumes: [],
        network: { denyByDefault: true, egress: [] },
        kubernetesAccess: { kind: "NONE", namespace: "kodex-runtime" },
        resourcesDigest: digest,
        volumesDigest: digest,
        networkDigest: digest,
        rbacDigest: digest,
      },
    },
  };
  const recipe: RoleImageRecipeDetail = {
    recipe: {
      sourceAvailable: true,
      ref: "recipe_synthetic_image",
      projectRef,
      version: 1,
      name: "Образ synthetic",
      roleDefinitionRef: "role_synthetic",
      state: "ACTIVE",
      generation: 1,
      promotedImageReady: true,
      environment: {
        environmentKey: "synthetic",
        dockerfile: "FROM scratch\n",
      },
      createdAt: now,
      updatedAt: now,
      nextActions: [],
    },
    builds: [],
    activeArtifact: {
      ref: "artifact_synthetic_image",
      version: 1,
      recipeRef: "recipe_synthetic_image",
      recipeGeneration: 1,
      manifestDigest: digest,
      provenanceSha256: digest,
      admissionVerdict: "ACCEPTED",
      tools: [],
    },
  };
  const consumer: RuntimeEnvironmentConsumer = {
    agentRef: "agent_impact_synthetic",
    agentVersion: 3,
    bindingRef: "binding_impact_synthetic",
    bindingVersion: 4,
    versionRef: "source_impact_synthetic",
    projectRef,
  };
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/api/v1/revision-impact-plans/environment_plan_synthetic") {
      if (!publicationPlan)
        throw new Error("Environment plan was not prepared");
      await route.fulfill({
        json: { plan: publicationPlan, items: [], total: 0 },
      });
      return;
    }
    if (
      path === `/api/v1/runtime-environment-drafts/${draft.ref}/impact-plans`
    ) {
      expect(draft.state).toBe("VALID");
      expect(request.headers()["if-match"]).toBe(`"${String(draft.version)}"`);
      publicationPlan = {
        ref: "environment_plan_synthetic",
        version: 1,
        kind: "RUNTIME_ENVIRONMENT",
        sourceVersion: 0,
        draftRef: draft.ref,
        draftVersion: draft.version,
        targetDigest: digest,
        digest: "environment-plan-digest",
        total: 0,
        state: "PREPARED",
        createdAt: now,
        expiresAt: "2099-09-06T00:00:00Z",
      };
      events.push("prepare");
      await route.fulfill({ status: 201, json: publicationPlan });
      return;
    }
    const impactBase = `/api/v1/runtime-environments/${environment.ref}/versions/${environment.currentVersion.ref}`;
    if (path === `${impactBase}/impact`) {
      await route.fulfill({
        json: {
          environmentRef: environment.ref,
          environmentVersion: environment.version,
          targetVersionRef: environment.currentVersion.ref,
          targetDigest: digest,
          consumers: [consumer],
          total: 1,
          nextPageToken: "",
        },
      });
      return;
    }
    if (path === `${impactBase}/consumer-bindings`) {
      expect(request.method()).toBe("POST");
      expect(request.headers()["if-match"]).toBe('"1"');
      const body = request.postDataJSON() as RuntimeEnvironmentRebindInput;
      expect(body).toEqual({ consumers: [consumer] });
      events.push("rebind");
      await route.fulfill({
        json: {
          bindings: [
            {
              ref: consumer.bindingRef,
              version: 5,
              agentRef: consumer.agentRef,
              environmentRef: environment.ref,
              versionRef: environment.currentVersion.ref,
              digest,
            },
          ],
        },
      });
      return;
    }
    if (
      path === `/api/v1/projects/${projectRef}/runtime-environments` &&
      request.method() === "GET"
    ) {
      events.push("list");
      await route.fulfill({
        json: { items: inspectorFailure?.status === 404 ? [] : [environment] },
      });
      return;
    }
    if (path === `/api/v1/projects/${projectRef}/runtime-environment-drafts`) {
      events.push("create");
      const body = request.postDataJSON() as {
        specification: RuntimeEnvironmentDraftSpecification;
      };
      if (request.method() !== "POST" || !request.headers()["idempotency-key"])
        failures.push("Invalid draft create request");
      draft = {
        ...draft,
        ref: "draft_synthetic_environment",
        specification: body.specification,
        state: "DRAFT",
        version: 1,
        diagnostics: [],
      };
      await route.fulfill({
        status: 201,
        headers: { ETag: '"1"' },
        json: draft,
      });
      return;
    }
    if (path.startsWith("/api/v1/runtime-environment-drafts/")) {
      if (
        path === `/api/v1/runtime-environment-drafts/${prepared.ref}` &&
        request.method() === "GET" &&
        draft.ref !== prepared.ref
      )
        draft = structuredClone(prepared);
      const action = path.endsWith("/validation")
        ? "validate"
        : path.endsWith("/publication")
          ? "publish"
          : request.method() === "PUT"
            ? "save"
            : request.method() === "DELETE"
              ? "discard"
              : "read";
      events.push(action);
      if (action !== "read") {
        if (
          request.headers()["if-match"] !== `"${String(draft.version)}"` ||
          !request.headers()["idempotency-key"]
        )
          failures.push("Invalid draft OCC headers");
        draft.version += 1;
      }
      if (action === "save") {
        draft.savedAt = "2026-09-05T00:05:00Z";
        draft.specification =
          request.postDataJSON() as RuntimeEnvironmentDraftSpecification;
        draft.state = "DRAFT";
        draft.diagnostics = [];
        delete draft.validationDigest;
      }
      if (action === "validate") {
        const valid =
          !!draft.specification.name &&
          !!draft.specification.imageArtifactRef &&
          !!draft.specification.policy;
        draft.state = valid ? "VALID" : "INVALID";
        draft.diagnostics = valid ? [] : ["ENVIRONMENT_VALIDATION_FAILED"];
        if (valid) draft.validationDigest = digest;
      }
      if (action === "publish") {
        if (!publicationPlan)
          throw new Error("Environment publication requires plan");
        expect(request.postDataJSON()).toEqual({
          planRef: publicationPlan.ref,
          selectedItemRefs: [],
        });
        if (draft.state !== "VALID") failures.push("Published non-VALID draft");
        draft.state = "PUBLISHED";
        draft.publishedEnvironmentRef = environment.ref;
        publicationPlan = {
          ...publicationPlan,
          state: "APPLIED",
          version: 2,
          publishedRevisionRef: environment.currentVersion.ref,
        };
      }
      if (action === "discard") draft.state = "DISCARDED";
      if (action === "publish") {
        await route.fulfill({
          status: 504,
          json: { type: "about:blank", title: "Gateway Timeout", status: 504 },
        });
        return;
      }
      await route.fulfill({
        json: draft,
        headers: { ETag: `"${String(draft.version)}"` },
      });
      return;
    }
    if (path === `/api/v1/runtime-environments/${environment.ref}`) {
      events.push("environment-readback");
      await route.fulfill({ json: environment, headers: { ETag: '"1"' } });
      return;
    }
    if (path === `/api/v1/runtime-environments/${environment.ref}/versions`) {
      await route.fulfill({ json: { items: [environment.currentVersion] } });
      return;
    }
    if (
      inspectorFailure &&
      path ===
        `/api/v1/runtime-environments/${environment.ref}/${inspectorFailure.endpoint}`
    ) {
      events.push("inspector-failure");
      const status = inspectorFailure.status;
      await route.fulfill({
        status,
        contentType: "application/problem+json",
        json: {
          type: "about:blank",
          title: "Synthetic inspector failure",
          status,
          code: status === 404 ? "NOT_FOUND" : "UNAVAILABLE",
          retryable: status === 503,
        },
      });
      return;
    }
    if (path === `/api/v1/runtime-environments/${environment.ref}/agents`) {
      events.push("agents");
      await route.fulfill({ json: { items: [], nextActions: [] } });
      return;
    }
    if (path === `/api/v1/runtime-environments/${environment.ref}/readiness`) {
      events.push("readiness");
      await route.fulfill({
        json: {
          environmentRef: environment.ref,
          environmentVersion: 1,
          publishedVersionRef: environment.currentVersion.ref,
          publishedVersionDigest: digest,
          ready: true,
          blockers: [],
          observedAt: now,
        },
      });
      return;
    }
    if (
      path ===
      `/api/v1/projects/${projectRef}/role-image-recipes/recipe_synthetic_image`
    ) {
      await route.fulfill({ json: recipe });
      return;
    }
    await route.fallback();
  });
  return {
    events,
    failures,
    preparedRef: prepared.ref,
    environment,
    setInspectorFailure: (failure: typeof inspectorFailure) => {
      inspectorFailure = failure;
    },
  };
}
