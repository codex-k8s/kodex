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

describe("synthetic typed catalog producers", () => {
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
    await installProviderFixture(page, (kind, value) => {
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
