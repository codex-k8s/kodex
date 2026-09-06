import { describe, expect, it } from "vitest";

import {
  buildIsActive,
  buildRevisionIdentity,
  canPromoteRoleImage,
  canRequestBuild,
  latestBuild,
  roleImageState,
  validateDockerfile,
} from "@/features/role-images/model";
import type {
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";

function recipe(overrides: Partial<RoleImageRecipe> = {}): RoleImageRecipe {
  return {
    sourceAvailable: true,
    ref: "image_1",
    version: 1,
    projectRef: "project_1",
    roleDefinitionRef: "role_1",
    name: "Среда аналитика",
    state: "ACTIVE",
    environment: {
      environmentKey: "standard",
      dockerfile: "FROM registry.example/base@sha256:" + "a".repeat(64),
    },
    generation: 2,
    promotedImageReady: false,
    createdAt: "2026-08-29T10:00:00Z",
    updatedAt: "2026-08-29T10:00:00Z",
    nextActions: ["OPEN", "UPDATE", "REQUEST_BUILD", "ARCHIVE"],
    ...overrides,
  };
}

function build(overrides: Partial<RoleImageBuild> = {}): RoleImageBuild {
  return {
    sourceAvailable: true,
    ref: "build_1",
    version: 1,
    recipeRef: "image_1",
    recipeGeneration: 2,
    dockerfile: "FROM registry.example/base@sha256:" + "a".repeat(64),
    attempt: 1,
    stage: "SOLVING",
    progressPercent: 45,
    createdAt: "2026-08-29T10:01:00Z",
    updatedAt: "2026-08-29T10:02:00Z",
    ...overrides,
  };
}

describe("role image model", () => {
  it("выбирает последнюю попытку по readback timestamp", () => {
    expect(
      latestBuild([
        build(),
        build({
          ref: "build_2",
          attempt: 2,
          updatedAt: "2026-08-29T10:03:00Z",
        }),
      ])?.ref,
    ).toBe("build_2");
  });

  it("не выдаёт build action локально без серверного nextAction", () => {
    expect(canRequestBuild(recipe())).toBe(true);
    expect(canRequestBuild(recipe({ nextActions: ["OPEN"] }))).toBe(false);
    expect(canRequestBuild(recipe({ state: "ARCHIVED" }))).toBe(false);
  });

  it("отличает активную сборку и promoted состояние", () => {
    expect(buildIsActive(build())).toBe(true);
    expect(buildIsActive(build({ stage: "FAILED" }))).toBe(false);
    expect(
      roleImageState(
        recipe({ promotedImageReady: true }),
        build({ stage: "COMPLETED" }),
      ),
    ).toBe("PROMOTED");
  });

  it("показывает build snapshot как точную попытку неизменяемого поколения", () => {
    expect(
      buildRevisionIdentity(build({ recipeGeneration: 7, attempt: 3 })),
    ).toEqual({ generation: 7, attempt: 3 });
  });

  it("разрешает promotion только по серверному nextAction и admitted artifact", () => {
    const artifact = {
      ref: "artifact_1",
      version: 1,
      recipeRef: "image_1",
      recipeGeneration: 2,
      manifestDigest: `sha256:${"a".repeat(64)}`,
      provenanceSha256: "b".repeat(64),
      admissionVerdict: "ACCEPTED" as const,
      tools: [],
    };
    expect(
      canPromoteRoleImage(
        recipe({ nextActions: ["OPEN", "PROMOTE"] }),
        artifact,
      ),
    ).toBe(true);
    expect(
      canPromoteRoleImage(recipe({ nextActions: ["OPEN"] }), artifact),
    ).toBe(false);
    expect(
      canPromoteRoleImage(recipe({ nextActions: ["PROMOTE"] }), {
        ...artifact,
        admissionVerdict: "REJECTED",
      }),
    ).toBe(false);
  });

  it("валидирует минимальный source без подмены backend validation", () => {
    expect(validateDockerfile("")).toEqual([
      "roleImages.validation.dockerfileRequired",
    ]);
    expect(validateDockerfile("RUN true")).toEqual([
      "roleImages.validation.fromRequired",
    ]);
    expect(
      validateDockerfile("FROM registry.example/base@sha256:" + "a".repeat(64)),
    ).toEqual([]);
  });
});
