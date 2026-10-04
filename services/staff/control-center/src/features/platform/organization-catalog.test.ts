import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import type {
  BootstrapState,
  RoleImageRecipe,
  RuntimeSecret,
} from "@/shared/api/generated/openapi/types.gen";
import { selectProjectRef } from "@/shared/project-context";
import { usePlatformStore } from "./store";
const recipe = {
  ref: "recipe_system",
  scopeKind: "ORGANIZATION",
  organizationRef: "org_fixture",
  projectRef: "",
  version: 1,
} as RoleImageRecipe;
const secret = {
  ref: "secret_system",
  scopeKind: "ORGANIZATION",
  organizationRef: "org_fixture",
  projectRef: "",
  version: 1,
  currentRevision: 1,
  state: "ACTIVE",
  valueType: "STRING",
  name: "TOKEN",
  description: "",
  nextActions: [],
  createdAt: "2026-10-04T00:00:00Z",
  updatedAt: "2026-10-04T00:00:00Z",
} as RuntimeSecret;
beforeEach(() => {
  setActivePinia(createPinia());
  selectProjectRef(undefined);
});
function store() {
  const value = usePlatformStore();
  value.bootstrap = {
    organizationRef: "org_fixture",
    platformRole: "OWNER",
  } as BootstrapState;
  return value;
}
describe("Общесистемные realtime каталоги", () => {
  it("при all-projects сохраняет ORG отдельно, не выдавая его за проектный", () => {
    const value = store();
    value.applyPlatformSnapshot("ROLE_IMAGE_RECIPE", undefined, {
      catalog: {
        recipes: [],
        organizationRecipes: [recipe],
        organizationRecipesPage: { nextPageToken: "next" },
        roleEnvironments: [],
      },
    });
    expect(Object.keys(value.roleImageRecipes)).toEqual([]);
    expect(value.organizationRoleImageRecipes[recipe.ref]).toEqual(recipe);
    expect(value.organizationRoleImagePage?.nextPageToken).toBe("next");
  });
  it("до cache replace отклоняет чужую организацию и project-as-ORG", () => {
    const value = store();
    for (const invalid of [
      { ...recipe, organizationRef: "org_foreign" },
      { ...recipe, scopeKind: "PROJECT", projectRef: "project_foreign" },
    ]) {
      expect(() =>
        value.applyPlatformSnapshot("ROLE_IMAGE_RECIPE", undefined, {
          catalog: {
            recipes: [],
            organizationRecipes: [invalid],
            organizationRecipesPage: {},
            roleEnvironments: [],
          },
        }),
      ).toThrow();
      expect(Object.keys(value.organizationRoleImageRecipes)).toEqual([]);
    }
  });
  it("scope selected project не смешивает ORG и PROJECT и очищает закрытый каталог", () => {
    const value = store();
    selectProjectRef("project_fixture");
    value.applyPlatformSnapshot("RUNTIME_SECRET", "project_fixture", {
      catalog: {
        secrets: [
          {
            ...secret,
            ref: "secret_project",
            scopeKind: "PROJECT",
            projectRef: "project_fixture",
          },
        ],
        organizationSecrets: [secret],
        organizationSecretsPage: {},
      },
    });
    expect(Object.keys(value.runtimeSecrets)).toEqual(["secret_project"]);
    expect(Object.keys(value.organizationRuntimeSecrets)).toEqual([
      "secret_system",
    ]);
    value.applyPlatformSnapshot("RUNTIME_SECRET", "project_fixture", {
      catalog: {
        secrets: [],
        organizationSecrets: [],
        organizationSecretsPage: {},
      },
    });
    expect(Object.keys(value.organizationRuntimeSecrets)).toEqual([]);
    value.applyRealtimeAvailability([], "project_fixture");
    expect(value.organizationRuntimeSecretPage).toBeUndefined();
  });
  it("missing ORG array/page и отсутствие bootstrap anchor закрыто отклоняются", () => {
    const value = store();
    expect(() =>
      value.applyPlatformSnapshot("RUNTIME_SECRET", undefined, {
        catalog: { secrets: [] },
      }),
    ).toThrow();
    value.bootstrap = undefined;
    expect(() =>
      value.applyPlatformSnapshot("RUNTIME_SECRET", undefined, {
        catalog: {
          secrets: [],
          organizationSecrets: [],
          organizationSecretsPage: {},
        },
      }),
    ).toThrow();
  });
});
