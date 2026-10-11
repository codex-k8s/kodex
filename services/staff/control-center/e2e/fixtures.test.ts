import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { observeHTTPResponse } from "./fixtures";
import type { Page, Route } from "@playwright/test";
import {
  scopedSyntheticSnapshot,
  type SyntheticSnapshotReader,
} from "./fixtures/runs-catalog";
import { installProviderFixture } from "./fixtures/providers";
import { installEnvironmentFixture } from "./fixtures/environment";
import {
  type RoleImageRecipePage,
  type RoleImageRecipeDetail,
} from "../src/shared/api/generated/openapi/types.gen";
import { checkGrantCandidates } from "./fixtures/grant-candidates";
import { checkEmailEffects } from "./fixtures/email-effects";
import { checkWorkflowEditor } from "./fixtures/workflow";
import {
  syntheticSecretDraftImpactPage,
  syntheticSecretCatalog,
} from "./fixtures/secrets";
import { validRuntimeResourceIdentity } from "../src/features/runtime/resource-scope";
import type {
  RuntimeSecretDraftImpactPlan,
  RuntimeSecret,
} from "../src/shared/api/generated/openapi/types.gen";

describe("synthetic typed catalog producers", () => {
  it("следует текущим disclosure/confirmation/table границам без устаревших browser dialogs", () => {
    const source = (file: string) =>
      readFileSync(new URL(file, import.meta.url), "utf8");
    const workflow = source("./fixtures/workflow.ts");
    const bindings = source("./fixtures/context-bindings.ts");
    const catalog = source("./fixtures/context-resources.ts");
    expect(workflow).not.toContain('page.once("dialog"');
    expect(workflow).toContain('name: "Подтвердите действие"');
    expect(workflow).toContain('name: "Отмена"');
    expect(workflow).toContain('".workflow-step-disclosure"');
    expect(bindings).not.toContain('page.once("dialog"');
    expect(bindings).toContain('"Отвязать эту ревизию от ИИ-сотрудника?"');
    expect(bindings).toContain('":scope > dl dd"');
    expect(bindings).toContain('".context-binding__technical dd"');
    expect(catalog).toContain('".context-catalog__scroll"');
    expect(catalog).not.toContain('"Развернуть список"');
    expect(
      source("../src/features/context-resources/ContextCatalog.vue"),
    ).toContain("max-height: 576px;");
  });
  it("workflow/assigned-agent HTTP и scoped bootstrap используют текущий mutable state", async () => {
    const stop = new Error("Synthetic UI boundary");
    const handlers = new Map<string, (route: Route) => Promise<void>>();
    const readers = new Map<string, SyntheticSnapshotReader>();
    const page = {
      route: (pattern: string, handler: (route: Route) => Promise<void>) => {
        handlers.set(pattern, handler);
      },
      goto: () => {
        throw stop;
      },
    } as unknown as Page;
    await expect(
      checkWorkflowEditor(
        page,
        "project_owned",
        undefined,
        undefined,
        (kind, read) => {
          readers.set(kind, read);
        },
      ),
    ).rejects.toBe(stop);
    const read = readers.get("WORKFLOW");
    const readAgent = readers.get("AGENT");
    const handler = handlers.get("**/api/v1/workflows/workflow_synthetic");
    const agentHandler = handlers.get(
      "**/api/v1/agents/agent_workflow_synthetic",
    );
    if (!read || !readAgent || !handler || !agentHandler)
      throw new Error("Synthetic workflow producer is missing");
    expect(read("project_foreign")).toBeUndefined();
    expect(readAgent("project_foreign")).toBeUndefined();
    const current = read("project_owned")?.catalog?.workflows?.[0];
    if (!current) throw new Error("Synthetic workflow snapshot is missing");
    let response: unknown;
    const fulfill = (value: { json: unknown }) => {
      response = structuredClone(value.json);
    };
    await agentHandler({ fulfill } as unknown as Route);
    expect(readAgent("project_owned")?.catalog?.agents).toEqual([response]);
    await handler({
      request: () => ({
        method: () => "PATCH",
        headers: () => ({ "if-match": '"1"', "idempotency-key": mutationKey }),
        postDataJSON: () => ({
          name: "Changed",
          purpose: current.purpose,
          coordinatorAgentRef: current.coordinatorAgentRef,
          steps: current.steps,
        }),
      }),
      fulfill,
    } as unknown as Route);
    expect(read("project_owned")?.catalog?.workflows).toEqual([response]);
    expect(read("project_owned")?.catalog?.workflows?.[0]?.version).toBe(2);
    expect(current.version).toBe(1);
  });
  it("secret HTTP/WS catalog материализует только published mutable state без алиасов", () => {
    expect(syntheticSecretCatalog().items).toEqual([]);
    const secret: RuntimeSecret = {
      scopeKind: "PROJECT",
      organizationRef: "org_synthetic",
      projectRef: "project_owned",
      ref: "secret_synthetic",
      version: 1,
      name: "Synthetic",
      description: "",
      valueType: "JSON",
      currentRevision: 1,
      state: "ACTIVE",
      nextActions: ["ROTATE", "REVOKE"],
      createdAt: "2026-09-05T00:00:00Z",
      updatedAt: "2026-09-05T00:00:00Z",
    };
    const before = syntheticSecretCatalog(secret);
    expect(before.items).toEqual([secret]);
    secret.version = 2;
    expect(syntheticSecretCatalog(secret).items[0]?.version).toBe(2);
    expect(before.items[0]?.version).toBe(1);
    const read = syntheticSecretCatalog(secret);
    const row = read.items[0];
    if (!row) throw new Error("Synthetic secret catalog row is missing");
    row.name = "changed";
    expect(syntheticSecretCatalog(secret).items[0]?.name).toBe("Synthetic");
    expect(validRuntimeResourceIdentity(secret, "org_foreign")).toBe(false);
  });
  it("secret impact HTTP сохраняет exact owner/scope и immutable plan pins после APPLIED", () => {
    const plan: RuntimeSecretDraftImpactPlan = {
      ref: "plan_synthetic",
      draftRef: "draft_synthetic",
      draftVersion: 2,
      secretRef: "secret_synthetic",
      secretVersion: 1,
      sourceRevision: 0,
      digest: "a".repeat(64),
      total: 1,
      expiresAt: "2026-09-06T00:00:00Z",
      state: "PREPARED",
    };
    const prepared = syntheticSecretDraftImpactPage(plan, "project_owned");
    const row = prepared.items[0];
    if (!row) throw new Error("Synthetic secret impact item is missing");
    expect(validRuntimeResourceIdentity(row.consumer, "org_synthetic")).toBe(
      true,
    );
    expect(validRuntimeResourceIdentity(row.consumer, "org_foreign")).toBe(
      false,
    );
    expect(
      validRuntimeResourceIdentity(
        { ...row.consumer, scopeKind: "UNSPECIFIED" },
        "org_synthetic",
      ),
    ).toBe(false);
    expect(row.consumer.projectRef).toBe("project_owned");
    expect(row.outcome).toBe("PENDING");
    const applied = syntheticSecretDraftImpactPage(
      { ...plan, state: "APPLIED" },
      "project_owned",
    );
    expect(applied.plan).toEqual({ ...plan, state: "APPLIED" });
    expect(applied.items[0]).toEqual({
      ...row,
      outcome: "APPLIED",
      resultEnvironmentVersionRef: "new_environment_synthetic",
    });
    applied.plan.digest = "changed";
    expect(plan.digest).toBe("a".repeat(64));
    expect(prepared.plan.state).toBe("PREPARED");
  });
  it("ENV image проходит protected LIST → exact detail → verified inventory без fallback", async () => {
    let handler: ((route: Route) => Promise<void>) | undefined;
    const page = {
      route: (_pattern: string, value: typeof handler) => {
        handler = value;
      },
    } as unknown as Page;
    await installEnvironmentFixture(page, "project_owned");
    const paths: string[] = [];
    async function read(path: string): Promise<unknown> {
      if (!handler) throw new Error("Missing synthetic image route");
      paths.push(path);
      let response: unknown;
      await handler({
        request: () => ({
          url: () => `https://kodex.test${path}`,
          method: () => "GET",
        }),
        fulfill: (value: { json: unknown }) => {
          response = structuredClone(value.json);
        },
        fallback: () => {
          throw new Error("Unhandled synthetic image route");
        },
      } as unknown as Route);
      return response;
    }
    const result = (await read(
      "/api/v1/projects/project_owned/role-image-recipes",
    )) as RoleImageRecipePage;
    expect(result.total).toBe(1);
    expect(result.nextPageToken).toBe("");
    const recipe = result.items[0];
    if (!recipe) throw new Error("Missing synthetic recipe");
    const detail = (await read(
      `/api/v1/projects/project_owned/role-image-recipes/${recipe.ref}`,
    )) as RoleImageRecipeDetail;
    expect(detail.recipe).toEqual(recipe);
    const artifact = detail.activeArtifact;
    if (!artifact) throw new Error("Missing synthetic active artifact");
    expect(recipe).toMatchObject({
      scopeKind: "PROJECT",
      organizationRef: "org_synthetic",
      projectRef: "project_owned",
      state: "ACTIVE",
      promotedImageReady: true,
      activeImageArtifactRef: artifact.ref,
    });
    expect(artifact).toMatchObject({
      scopeKind: "PROJECT",
      organizationRef: "org_synthetic",
      projectRef: "project_owned",
      recipeRef: recipe.ref,
      recipeGeneration: recipe.generation,
      admissionVerdict: "ACCEPTED",
      promotionState: "PROMOTED",
    });
    expect(artifact.promotedReference).toBe(
      `registry.invalid/test@sha256:${artifact.manifestDigest}`,
    );
    expect(artifact.verifiedToolInventory.status).toBe("VERIFIED");
    expect(artifact.verifiedToolInventory.imageDigest).toBe(
      `sha256:${artifact.manifestDigest.replace(/^sha256:/, "")}`,
    );
    expect(artifact.verifiedToolInventory.provenanceSha256).toBe(
      artifact.provenanceSha256,
    );
    expect(paths).toHaveLength(2);
    await expect(
      read("/api/v1/projects/project_foreign/role-image-recipes"),
    ).rejects.toThrow("Unhandled synthetic image route");
    await expect(
      read("/api/v1/projects/project_owned/role-image-recipes/recipe_foreign"),
    ).rejects.toThrow("Unhandled synthetic image route");
  });

  it("email и grant bootstrap читают собственный HTTP state, включая grant mutation", async () => {
    for (const kind of ["email", "grant"] as const) {
      const handlers = new Map<string, (route: Route) => Promise<void>>();
      let read: SyntheticSnapshotReader | undefined;
      const navigation = new Error("Synthetic navigation boundary");
      const page = {
        route: (pattern: string, handler: (route: Route) => Promise<void>) => {
          handlers.set(pattern, handler);
        },
        context: () => ({ route: () => undefined }),
        goto: () => Promise.reject(navigation),
      } as unknown as Page;
      const install = (_kind: string, value: SyntheticSnapshotReader) => {
        expect(_kind).toBe("INTEGRATION_CONNECTION");
        read = value;
      };
      await expect(
        kind === "email"
          ? checkEmailEffects(page, () => Promise.resolve(), install)
          : checkGrantCandidates(page, "project_owned", install),
      ).rejects.toBe(navigation);
      const first = read?.()?.connections?.connections?.[0];
      expect(first?.ref).toBe(
        kind === "email" ? "email_synthetic" : "connection_candidates",
      );
      if (!first || !read)
        throw new Error("Missing synthetic integration connection");
      first.version = 99;
      expect(read()?.connections?.connections?.[0]?.version).toBe(
        kind === "email" ? 5 : 7,
      );
      if (kind === "grant") {
        const handler = handlers.get(
          "**/api/v1/integration-connections/connection_candidates/grants",
        );
        if (!handler) throw new Error("Missing synthetic grant route");
        let response: unknown;
        await handler({
          request: () => ({
            headers: () => ({
              "if-match": '"7"',
              "idempotency-key": mutationKey,
              "x-csrf-token": "synthetic",
            }),
            postDataJSON: () => ({
              capabilityKey: "github.read",
              agentRef: "agent_candidate",
              enabled: true,
            }),
          }),
          fulfill: (value: { json: unknown }) => {
            response = value.json;
          },
        } as unknown as Route);
        expect(read()?.connections?.connections).toEqual([response]);
        expect(read()?.connections?.connections?.[0]?.version).toBe(8);
      }
    }
  });

  it("читает текущее состояние только точного проекта и не отдаёт mutable alias", () => {
    let revision = 1;
    const read = scopedSyntheticSnapshot("project_owned", () => ({
      catalog: { page: { nextPageToken: String(revision) } },
    }));
    expect(read()).toBeUndefined();
    expect(read("project_foreign")).toBeUndefined();
    const first = read("project_owned");
    expect(first?.catalog?.page?.nextPageToken).toBe("1");
    if (!first?.catalog?.page) throw new Error("Missing synthetic catalog");
    first.catalog.page.nextPageToken = "forged";
    expect(read("project_owned")?.catalog?.page?.nextPageToken).toBe("1");
    revision = 2;
    expect(read("project_owned")?.catalog?.page?.nextPageToken).toBe("2");
  });

  it("provider bootstrap отражает те же account mutations, что HTTP fixture", async () => {
    let handler: ((route: Route) => Promise<void>) | undefined;
    let read: SyntheticSnapshotReader | undefined;
    const page = {
      route: (_pattern: string, value: typeof handler) => {
        handler = value;
      },
    } as unknown as Page;
    const fixture = await installProviderFixture(page, (kind, value) => {
      expect(kind).toBe("PROVIDER_ACCOUNT");
      read = value;
    });
    expect(read?.()?.catalog?.accounts).toEqual([]);
    const request = {
      url: () => "https://kodex.test/api/v1/provider-accounts",
      method: () => "POST",
      headers: () => ({ "idempotency-key": mutationKey }),
      postDataJSON: () => ({
        name: "Synthetic account",
        definitionKey: "openai-codex",
      }),
    };
    let response: unknown;
    if (!handler || !read) throw new Error("Missing synthetic producer");
    await handler({
      request: () => request,
      fulfill: (value: { json: unknown }) => {
        response = value.json;
      },
    } as unknown as Route);
    expect(read()?.catalog?.accounts).toEqual([response]);
    const old = read();
    const account = old?.catalog?.accounts?.[0];
    if (!account) throw new Error("Missing synthetic account");
    account.version = 99;
    expect(read()?.catalog?.accounts?.[0]?.version).toBe(1);
    await handler({
      request: () => ({
        ...request,
        url: () =>
          "https://kodex.test/api/v1/provider-accounts/pacc_synthetic_0/device-authorization",
        headers: () => ({ "idempotency-key": mutationKey, "if-match": '"1"' }),
      }),
      fulfill: (value: { json: unknown }) => {
        response = value.json;
      },
    } as unknown as Route);
    expect(read()?.catalog?.accounts).toEqual([response]);
    expect(read()?.catalog?.accounts?.[0]?.version).toBe(2);
    expect(read()?.catalog?.accounts?.[0]?.authorization?.state).toBe(
      "PENDING",
    );
    expect(read()?.catalog?.accounts?.[0]?.version).toBe(2);
    for (const [endpoint, version, state] of [
      ["authorization-refresh", 2, "AUTHORIZED"],
      ["device-reauthorizations", 3, "PENDING"],
      ["authorization-refresh", 4, "AUTHORIZED"],
    ] as const) {
      await handler({
        request: () => ({
          ...request,
          url: () =>
            `https://kodex.test/api/v1/provider-accounts/pacc_synthetic_0/${endpoint}`,
          headers: () => ({
            "idempotency-key": mutationKey,
            "if-match": `"${String(version)}"`,
          }),
        }),
        fulfill: (value: { json: unknown }) => {
          response = value.json;
        },
      } as unknown as Route);
      expect(read()?.catalog?.accounts).toEqual([response]);
      expect(read()?.catalog?.accounts?.[0]?.version).toBe(version + 1);
      expect(read()?.catalog?.accounts?.[0]?.authorization?.state).toBe(state);
    }
    expect(fixture.events.filter((event) => event === "poll")).toHaveLength(2);
    expect(fixture.events).not.toContain("verify");
    await handler({
      request: () => ({
        ...request,
        url: () =>
          "https://kodex.test/api/v1/provider-accounts/pacc_synthetic_0",
        method: () => "DELETE",
        headers: () => ({ "idempotency-key": mutationKey, "if-match": '"5"' }),
      }),
      fulfill: (value: { json: unknown }) => {
        response = value.json;
      },
    } as unknown as Route);
    expect(read()?.catalog?.accounts).toEqual([response]);
    expect(read()?.catalog?.accounts?.[0]?.deletion?.state).toBe(
      "CLEANUP_QUEUED",
    );
    await handler({
      request: () => ({
        ...request,
        url: () =>
          "https://kodex.test/api/v1/provider-accounts/pacc_synthetic_0",
        method: () => "GET",
      }),
      fulfill: (value: { json: unknown }) => {
        response = value.json;
      },
    } as unknown as Route);
    expect(response).toMatchObject({
      ref: "pacc_synthetic_0",
      version: 7,
      state: "DELETED",
      deletion: { state: "DELETED", pendingCleanup: 0 },
    });
    expect(read()?.catalog?.accounts).toEqual([]);
    expect(fixture.events).toContain("deleted-readback");
    expect(fixture.failures).toEqual([]);
  });

  it("environment bootstrap сохраняет exact scope и авторитетное отсутствие после404", async () => {
    let read: SyntheticSnapshotReader | undefined;
    const page = { route: () => undefined } as unknown as Page;
    const fixture = await installEnvironmentFixture(
      page,
      "project_owned",
      (kind, value) => {
        expect(kind).toBe("RUNTIME_ENVIRONMENT");
        read = value;
      },
    );
    expect(read?.("project_foreign")).toBeUndefined();
    expect(read?.("project_owned")?.catalog?.environments).toEqual([
      fixture.environment,
    ]);
    fixture.setInspectorFailure({ endpoint: "readiness", status: 404 });
    expect(read?.("project_owned")?.catalog?.environments).toEqual([]);
    fixture.setInspectorFailure(undefined);
    expect(read?.("project_owned")?.catalog?.environments).toEqual([
      fixture.environment,
    ]);
  });
});

const mutationKey = "11111111-1111-4111-8111-111111111111";
const endpoint = "https://control.kodex.works/api/v1/example";

describe("browser diagnostics HTTP recovery", () => {
  it("закрывает 503 только подтверждённым успехом той же mutation", () => {
    const pending = new Map<string, string>();

    expect(
      observeHTTPResponse(pending, 503, "POST", endpoint, mutationKey),
    ).toEqual({});
    expect(pending.size).toBe(1);
    expect(
      observeHTTPResponse(pending, 200, "POST", endpoint, mutationKey),
    ).toEqual({
      recovered: `response:503:POST:${endpoint}`,
    });
    expect(pending.size).toBe(0);
  });

  it("сохраняет незавершённый 5xx и не связывает другой mutation key", () => {
    const pending = new Map<string, string>();
    observeHTTPResponse(pending, 503, "POST", endpoint, mutationKey);

    expect(
      observeHTTPResponse(
        pending,
        200,
        "POST",
        endpoint,
        "22222222-2222-4222-8222-222222222222",
      ),
    ).toEqual({});
    expect([...pending.values()]).toEqual([`response:503:POST:${endpoint}`]);
  });

  it("немедленно фиксирует 5xx без idempotency key", () => {
    const pending = new Map<string, string>();

    expect(observeHTTPResponse(pending, 503, "GET", endpoint)).toEqual({
      failure: `response:503:GET:${endpoint}`,
    });
    expect(pending.size).toBe(0);
  });
});
