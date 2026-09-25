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
    expect(source).toContain('current.nextActions.includes("CANCEL_BUILD")');
    expect(source).toMatch(
      /commandRoleImage\(\s*exact\.projectRef,\s*current,\s*"CANCEL_BUILD",\s*build\.value\.ref,?\s*\)/,
    );
    expect(source).toContain("assistant.roleImageBuild.stopConfirm");
    expect(source).toContain("onCleanup");
  });
});
