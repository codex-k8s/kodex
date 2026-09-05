import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  commandRoleImage,
  createRoleImage,
  loadRoleDefinitionOptions,
  loadRoleImageDependencies,
  loadRoleImagePage,
  loadRoleImageRevisionPage,
  promoteRoleImageArtifact,
  updateRoleImage,
} from "@/features/role-images/api";
import type { RoleImageRecipe } from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({
  commandRoleImageRecipe: vi.fn(),
  createRoleImageRecipe: vi.fn(),
  getRoleImageRecipe: vi.fn(),
  listAgents: vi.fn(),
  listRoleEnvironments: vi.fn(),
  listRoleImageRecipeRevisions: vi.fn(),
  listRoleImageRecipes: vi.fn(),
  listRuntimeEnvironmentSets: vi.fn(),
  promoteRoleImage: vi.fn(),
  updateRoleImageRecipe: vi.fn(),
}));
const mutation = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", () => mutation);

function response<T>(data: T) {
  return Promise.resolve({
    data,
    response: new Response(null, { status: 200 }),
  });
}

const recipe: RoleImageRecipe = {
  ref: "image_1",
  version: 3,
  projectRef: "project_1",
  roleDefinitionRef: "role_1",
  name: "Образ аналитика",
  state: "ACTIVE",
  environment: {
    environmentKey: "standard",
    dockerfile: "FROM registry.example/base@sha256:" + "a".repeat(64),
  },
  generation: 2,
  promotedImageReady: false,
  createdAt: "2026-08-29T10:00:00Z",
  updatedAt: "2026-08-29T10:00:00Z",
  nextActions: ["OPEN", "REQUEST_BUILD"],
};

describe("role image API adapter", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mutation.mutate.mockImplementation(
      async (request: (headers: Record<string, string>) => Promise<unknown>) =>
        request({
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "csrf_1",
        }),
    );
  });

  it("передаёт project и cursor в настоящий list endpoint", async () => {
    api.listRoleImageRecipes.mockReturnValueOnce(
      response({ items: [recipe], nextPageToken: "page_2" }),
    );
    const page = await loadRoleImagePage(
      "project_1",
      "page_1",
      new AbortController().signal,
      { query: "Среда", state: "ACTIVE", roleDefinitionRef: "role_1" },
    );
    expect(api.listRoleImageRecipes).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_1" },
        query: {
          pageSize: 40,
          pageToken: "page_1",
          query: "Среда",
          state: "ACTIVE",
          roleDefinitionRef: "role_1",
        },
      }),
    );
    expect(page.nextPageToken).toBe("page_2");
  });

  it("собирает роли только из авторитетных agent roleDefinitionRef", async () => {
    api.listAgents
      .mockReturnValueOnce(
        response({
          items: [
            {
              ref: "agent_1",
              roleDefinitionRef: "role_1",
              roleDefinitionName: "Аналитик",
            },
            { ref: "agent_without_role", name: "Без роли" },
          ],
          nextPageToken: "page_2",
        }),
      )
      .mockReturnValueOnce(
        response({
          items: [
            {
              ref: "agent_2",
              roleDefinitionRef: "role_1",
              roleDefinitionName: "Аналитик",
            },
          ],
        }),
      );

    await expect(loadRoleDefinitionOptions("project_1")).resolves.toEqual([
      { ref: "role_1", label: "Аналитик", agentCount: 2 },
    ]);
    expect(api.listAgents).toHaveBeenCalledTimes(2);
  });

  it("выполняет только существующую versioned build command", async () => {
    api.commandRoleImageRecipe.mockReturnValueOnce(
      response({ recipe, reused: false }),
    );
    await commandRoleImage("project_1", recipe, "REQUEST_BUILD");
    expect(mutation.mutate).toHaveBeenCalledWith(expect.any(Function), 3);
    expect(api.commandRoleImageRecipe).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_1", recipeRef: "image_1" },
        body: { action: "REQUEST_BUILD" },
        headers: {
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "csrf_1",
        },
      }),
    );
  });

  it("создаёт и обновляет immutable recipe с точным Dockerfile", async () => {
    api.createRoleImageRecipe.mockReturnValueOnce(response(recipe));
    api.updateRoleImageRecipe.mockReturnValueOnce(
      response({ ...recipe, version: 4, generation: 3 }),
    );
    const environment = {
      environmentKey: "standard",
      dockerfile: recipe.environment.dockerfile + "\nRUN true\n",
    };

    await createRoleImage("project_1", {
      roleDefinitionRef: "role_1",
      name: "Образ аналитика",
      environment,
    });
    await updateRoleImage("project_1", recipe, {
      name: "Образ аналитика v2",
      environment,
    });

    const createRequest: unknown = api.createRoleImageRecipe.mock.calls[0]?.[0];
    const updateRequest: unknown = api.updateRoleImageRecipe.mock.calls[0]?.[0];
    expect(createRequest).toMatchObject({
      path: { projectRef: "project_1" },
      body: { environment },
    });
    expect(updateRequest).toMatchObject({
      path: { projectRef: "project_1", recipeRef: "image_1" },
      body: { environment },
      headers: { "If-Match": '"3"' },
    });
  });

  it("возвращает только окружения, закреплённые за exact artifact", async () => {
    api.listRuntimeEnvironmentSets.mockReturnValueOnce(
      response({
        items: [
          {
            ref: "environment_1",
            currentVersion: {
              image: { artifactRef: "imgart_target" },
            },
          },
          {
            ref: "environment_2",
            currentVersion: {
              image: { artifactRef: "imgart_other" },
            },
          },
        ],
      }),
    );

    await expect(
      loadRoleImageDependencies("project_1", "imgart_target"),
    ).resolves.toEqual([expect.objectContaining({ ref: "environment_1" })]);
  });

  it("читает immutable revisions и продвигает exact artifact по provenance", async () => {
    api.listRoleImageRecipeRevisions.mockReturnValueOnce(
      response({ items: [], nextPageToken: "page_2" }),
    );
    api.promoteRoleImage.mockReturnValueOnce(
      response({
        ref: "promotion_1",
        recipeRef: recipe.ref,
        imageArtifactRef: "artifact_1",
        provenanceSha256: "b".repeat(64),
        manifestDigest: `sha256:${"c".repeat(64)}`,
        receiptSha256: "d".repeat(64),
        state: "QUEUED",
        createdAt: "2026-08-30T10:00:00Z",
      }),
    );

    await loadRoleImageRevisionPage("project_1", recipe.ref, "page_1");
    await promoteRoleImageArtifact(
      "project_1",
      recipe,
      "artifact_1",
      "b".repeat(64),
    );

    expect(api.listRoleImageRecipeRevisions).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_1", recipeRef: recipe.ref },
        query: { pageSize: 40, pageToken: "page_1" },
      }),
    );
    expect(api.promoteRoleImage).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_1", recipeRef: recipe.ref },
        body: {
          imageArtifactRef: "artifact_1",
          expectedProvenanceSha256: "b".repeat(64),
        },
        headers: {
          "Idempotency-Key": "idem_1",
          "If-Match": '"3"',
          "X-CSRF-Token": "csrf_1",
        },
      }),
    );
  });
});
