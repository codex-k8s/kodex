import { describe, expect, it } from "vitest";

import {
  buildIsActive,
  canRequestBuild,
  defaultDockerfile,
  latestBuild,
  roleImageApiGaps,
  roleImageState,
  tokenizeDockerfileLine,
  validateDockerfile,
} from "@/features/role-images/model";
import type {
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";

function recipe(overrides: Partial<RoleImageRecipe> = {}): RoleImageRecipe {
  return {
    ref: "image_1",
    version: 1,
    projectRef: "project_1",
    roleDefinitionRef: "role_1",
    name: "Среда аналитика",
    state: "ACTIVE",
    environment: { environmentKey: "standard" },
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
    ref: "build_1",
    version: 1,
    recipeRef: "image_1",
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

  it("подсвечивает инструкции и переменные Dockerfile", () => {
    expect(tokenizeDockerfileLine("RUN echo ${HOME}")).toEqual([
      { text: "RUN", tone: "instruction" },
      { text: " echo ", tone: "argument" },
      { text: "${HOME}", tone: "variable" },
    ]);
    expect(tokenizeDockerfileLine("# comment")).toEqual([
      { text: "# comment", tone: "comment" },
    ]);
  });

  it("валидирует минимальный source без подмены backend validation", () => {
    expect(validateDockerfile("")).toEqual([
      "roleImages.validation.dockerfileRequired",
    ]);
    expect(validateDockerfile("RUN true")).toEqual([
      "roleImages.validation.fromRequired",
    ]);
    expect(validateDockerfile(defaultDockerfile())).toEqual([]);
  });

  it("фиксирует все отсутствующие поля публичного контракта", () => {
    expect(roleImageApiGaps.map((gap) => gap.key)).toEqual([
      "dockerfile",
      "revisions",
      "promotion",
      "evidence",
      "executables",
      "environment-links",
    ]);
  });
});
