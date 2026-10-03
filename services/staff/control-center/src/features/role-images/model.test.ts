import { describe, expect, it } from "vitest";

import {
  buildIsActive,
  buildRevisionIdentity,
  canPromoteRoleImage,
  canRequestBuild,
  latestBuild,
  roleImageLifecycleNeedsRefresh,
  roleImageState,
  validateDockerfile,
} from "@/features/role-images/model";
import type {
  RoleImageBuild,
  RoleImageArtifact,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";

function recipe(overrides: Partial<RoleImageRecipe> = {}): RoleImageRecipe {
  return {
    scopeKind: "PROJECT",
    organizationRef: "org_synthetic",
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
    projectRef: "project_1",
    scopeKind: "PROJECT",
    organizationRef: "org_synthetic",
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

function admittedArtifact(
  overrides: Partial<RoleImageArtifact> = {},
): RoleImageArtifact {
  return {
    projectRef: "project_1",
    scopeKind: "PROJECT",
    organizationRef: "org_synthetic",
    ref: "artifact_1",
    version: 1,
    recipeRef: "image_1",
    recipeGeneration: 2,
    buildRef: "build_1",
    manifestDigest: "a".repeat(64),
    provenanceSha256: "b".repeat(64),
    admissionVerdict: "ACCEPTED",
    promotionState: "PENDING",
    promotionRequested: false,
    tools: [],
    ...overrides,
  };
}

describe("role image model", () => {
  it("выбирает последнюю созданную сборку, даже если прежняя обновилась позже", () => {
    expect(
      latestBuild([
        build({ updatedAt: "2026-08-29T10:05:00Z" }),
        build({
          ref: "build_2",
          attempt: 2,
          createdAt: "2026-08-29T10:03:00Z",
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
    for (const stage of ["FAILED", "EXPIRED"] as const)
      expect(buildIsActive(build({ stage })), stage).toBe(true);
    for (const stage of ["COMPLETED", "CANCELLED", "DEAD_LETTER"] as const)
      expect(buildIsActive(build({ stage })), stage).toBe(false);
    expect(
      roleImageState(
        recipe({ promotedImageReady: true }),
        build({ stage: "COMPLETED" }),
      ),
    ).toBe("PROMOTED");
  });

  it("продолжает опрос после сборки до решения о допуске и во время публикации", () => {
    const completed = build({ stage: "COMPLETED", progressPercent: 100 });
    expect(
      roleImageLifecycleNeedsRefresh(recipe(), completed, undefined, undefined),
    ).toBe(true);
    expect(
      roleImageLifecycleNeedsRefresh(
        recipe(),
        completed,
        admittedArtifact({ buildRef: "old_build" }),
        undefined,
      ),
    ).toBe(true);
    expect(
      roleImageLifecycleNeedsRefresh(
        recipe(),
        completed,
        admittedArtifact({ promotionRequested: true }),
        undefined,
      ),
    ).toBe(true);
    expect(
      roleImageLifecycleNeedsRefresh(
        recipe({
          promotedImageReady: true,
          activeImageArtifactRef: "artifact_1",
        }),
        completed,
        admittedArtifact({
          promotionRequested: true,
          promotionState: "PROMOTED",
        }),
        undefined,
      ),
    ).toBe(false);
  });

  it("останавливает опрос на ожидающем решения владельца и отказанных состояниях", () => {
    const completed = build({ stage: "COMPLETED" });
    const failedReceipt = {
      ref: "receipt_1",
      recipeRef: "image_1",
      imageArtifactRef: "artifact_1",
      provenanceSha256: "b".repeat(64),
      manifestDigest: "a".repeat(64),
      receiptSha256: "c".repeat(64),
      state: "FAILED" as const,
      createdAt: "2026-09-28T00:00:00Z",
    };
    expect(
      roleImageLifecycleNeedsRefresh(
        recipe(),
        completed,
        admittedArtifact(),
        undefined,
      ),
    ).toBe(false);
    expect(
      roleImageLifecycleNeedsRefresh(
        recipe(),
        completed,
        admittedArtifact({ admissionVerdict: "REJECTED" }),
        undefined,
      ),
    ).toBe(false);
    expect(
      roleImageLifecycleNeedsRefresh(
        recipe(),
        completed,
        admittedArtifact({ promotionRequested: true }),
        failedReceipt,
      ),
    ).toBe(false);
    expect(
      roleImageLifecycleNeedsRefresh(
        recipe(),
        completed,
        admittedArtifact({ ref: "artifact_2", promotionRequested: true }),
        failedReceipt,
      ),
    ).toBe(true);
  });

  it("показывает build snapshot как точную попытку неизменяемого поколения", () => {
    expect(
      buildRevisionIdentity(build({ recipeGeneration: 7, attempt: 3 })),
    ).toEqual({ generation: 7, attempt: 3 });
  });

  it("разрешает promotion только по серверному nextAction и admitted artifact", () => {
    const artifact = {
      projectRef: "project_1",
      scopeKind: "PROJECT" as const,
      organizationRef: "org_synthetic",
      ref: "artifact_1",
      version: 1,
      recipeRef: "image_1",
      recipeGeneration: 2,
      buildRef: "build_1",
      manifestDigest: `sha256:${"a".repeat(64)}`,
      provenanceSha256: "b".repeat(64),
      admissionVerdict: "ACCEPTED" as const,
      promotionState: "PENDING" as const,
      promotionRequested: false,
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
