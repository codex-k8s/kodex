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
  });

  it("останавливает активную сборку только через версионную команду ARCHIVE", () => {
    expect(source).toContain('current.nextActions.includes("ARCHIVE")');
    expect(source).toContain(
      'commandRoleImage(exact.projectRef, current, "ARCHIVE")',
    );
    expect(source).toContain("assistant.roleImageBuild.stopConfirm");
    expect(source).toContain("onCleanup");
  });
});
