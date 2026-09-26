import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantRoleImageBuildCard.vue", import.meta.url),
  "utf8",
);

describe("AssistantRoleImageBuildCard", () => {
  it("читает точный ресурс квитанции и показывает авторитетный прогресс", () => {
    expect(source).toContain("assistantRoleImageBuildTarget");
    expect(source).toContain("loadRoleImageDetail");
    expect(source).toContain("next.recipe.ref !== value.recipeRef");
    expect(source).toContain("build.progressPercent");
    expect(source).toContain("build.safeErrorCode");
    expect(source).toContain("role-image");
    expect(source).toContain("@click=\"emit('navigate')\"");
  });

  it("отменяет точную сборку без архивирования рецепта", () => {
    expect(source).toContain(
      '["COMPLETED", "FAILED", "CANCELLED", "EXPIRED", "DEAD_LETTER"]',
    );
    expect(source).toContain('current.nextActions.includes("CANCEL_BUILD")');
    expect(source).toMatch(
      /commandRoleImage\(\s*exact\.projectRef,\s*current,\s*"CANCEL_BUILD",\s*build\.value\.ref,?\s*\)/,
    );
    expect(source).toContain("assistant.roleImageBuild.stopConfirm");
    expect(source).toContain("onCleanup");
  });

  it("показывает допуск и публикует только текущий допущенный артефакт один раз", () => {
    expect(source).toContain("artifact?.buildRef === build.value.ref");
    expect(source).toContain(
      "artifact.recipeGeneration === build.value.recipeGeneration",
    );
    expect(source).toContain("candidate?.admissionVerdict");
    expect(source).toContain("canPromoteRoleImage(recipe, artifact)");
    expect(source).toContain("awaitingAdmission.value");
    expect(source).toContain("admissionPolls >= 120");
    expect(source).toContain("promotionPolls >= 120");
    expect(source).toContain("candidate.value?.promotionRequested === true");
    expect(source).toContain('candidate.value.promotionState === "REJECTED"');
    expect(source).toContain("attemptedArtifactRef.value = artifact.ref");
    expect(source).toContain("promoteRoleImageArtifact(");
    expect(source).toContain("receipt.imageArtifactRef !== artifact.ref");
    expect(source).toContain("assistant.roleImageBuild.promotionUnknown");
  });

  it("передаёт сотруднику только безопасную диагностику точной попытки", () => {
    expect(source).toContain('["FAILED", "EXPIRED", "DEAD_LETTER"]');
    expect(source).toContain('t("assistant.roleImageBuild.debugPrompt"');
    expect(source).toContain("recipeRef: exact.recipeRef");
    expect(source).toContain("buildRef: current.ref");
    expect(source).toContain("attempt: current.attempt");
    expect(source).toContain("diagnosticCode: current.diagnosticCode");
    expect(source).toContain("diagnosticSummary: current.diagnosticSummary");
    expect(source).toContain('emit(\n    "debug"');
  });
});
