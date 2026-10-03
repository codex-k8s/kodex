import { describe, expect, it } from "vitest";
import type { RoleImageArtifact } from "@/shared/api/generated/openapi/types.gen";
import {
  assertPromotedRuntimeImage,
  runtimeImageOption,
  toolsForRuntimeImage,
} from "./image-tools-selection";

const expected = {
  artifactRef: "imgart_exact",
  recipeRef: "imgrecipe_exact",
  recipeGeneration: 3,
};
function artifact(
  overrides: Partial<RoleImageArtifact> = {},
): RoleImageArtifact {
  return {
    ref: expected.artifactRef,
    recipeRef: expected.recipeRef,
    recipeGeneration: expected.recipeGeneration,
    manifestDigest: `sha256:${"a".repeat(64)}`,
    promotedReference: `example.invalid/assistant@sha256:${"a".repeat(64)}`,
    admissionVerdict: "ACCEPTED",
    promotionState: "PROMOTED",
    tools: [{ name: "git", version: "2.53" }],
    ...overrides,
  } as RoleImageArtifact;
}

describe("Выбор образа и проверенных инструментов", () => {
  it("принимает только точную promoted версию с совпадающим digest", () => {
    expect(() =>
      assertPromotedRuntimeImage(artifact(), expected),
    ).not.toThrow();
    expect(() =>
      assertPromotedRuntimeImage(
        artifact({ manifestDigest: "a".repeat(64) }),
        expected,
      ),
    ).not.toThrow();
  });

  it.each([
    { ref: "imgart_other" },
    { recipeRef: "imgrecipe_other" },
    { recipeGeneration: 4 },
    { admissionVerdict: "REJECTED" },
    { promotionState: "AUTHORIZED" },
    { promotedReference: "example.invalid/assistant:latest" },
    { promotedReference: `example.invalid/assistant@sha256:${"b".repeat(64)}` },
    { manifestDigest: "invalid" },
  ] as Partial<RoleImageArtifact>[])(
    "не открывает инструменты для недопущенного или stale образа %o",
    (change) => {
      expect(() =>
        assertPromotedRuntimeImage(artifact(change), expected),
      ).toThrow();
    },
  );

  it("при смене образа сохраняет настройки только общих проверенных программ", () => {
    const tools = [
      {
        name: "Git",
        command: "git",
        description: "Описание",
        usageHint: "status",
      },
      {
        name: "Curl",
        command: "curl",
        description: "Не входит в новый образ",
        usageHint: "",
      },
    ];
    const selected = toolsForRuntimeImage(tools, artifact());
    expect(selected).toEqual([tools[0]]);
    const selectedTool = selected[0];
    if (!selectedTool) throw new Error("Fixture selected tool is missing");
    selectedTool.description = "Изменено";
    expect(tools[0]?.description).toBe("Описание");
  });

  it("не восстанавливает recipe/generation из названия или opaque ref", () => {
    expect(() =>
      runtimeImageOption({ ref: "imgart_exact", title: "Образ" }),
    ).toThrow();
    expect(
      runtimeImageOption({
        ref: "imgart_exact",
        title: "Образ",
        recipeRef: "imgrecipe_exact",
        generation: 3,
      } as Parameters<typeof runtimeImageOption>[0]),
    ).toMatchObject({ recipeRef: "imgrecipe_exact", generation: 3 });
  });
});
