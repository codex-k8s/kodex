import { describe, expect, it, vi } from "vitest";
import {
  verifiedInventoryFixture,
  unavailableInventoryFixture,
} from "@/test-utils/image-inventory-fixture";
import type { RoleImageArtifact } from "@/shared/api/generated/openapi/types.gen";
import {
  assertPromotedRuntimeImage,
  runtimeImageOption,
  restoreRuntimeImageOption,
  type RuntimeImageCatalog,
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
    provenanceSha256: "b".repeat(64),
    declaredTools: [{ name: "git", version: "2.53" }],
    verifiedToolInventory: verifiedInventoryFixture(undefined, undefined, [
      "git",
    ]),
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

  it("допускает promoted образ с нативным Chromium и сохраняет его проверенную команду", () => {
    const value = artifact({
      verifiedToolInventory: verifiedInventoryFixture(),
    });
    const chromium = value.verifiedToolInventory.platforms[0]?.tools.find(
      (tool) => tool.name === "chromium",
    );
    if (!chromium) throw new Error("Missing synthetic Chromium observation");
    chromium.path = "/usr/lib/chromium/chromium";
    expect(() => assertPromotedRuntimeImage(value, expected)).not.toThrow();
    const tools = [
      {
        name: "Chromium",
        command: "chromium",
        description: "Браузер",
        usageHint: "--version",
      },
    ];
    expect(toolsForRuntimeImage(tools, value)).toEqual(tools);
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
    { verifiedToolInventory: unavailableInventoryFixture() },
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

  it("восстанавливает выбор черновика из точного scoped каталога по курсору", async () => {
    const option = {
      ref: expected.artifactRef,
      title: "Собственный образ",
      recipeRef: expected.recipeRef,
      generation: expected.recipeGeneration,
    };
    const loadPage = vi
      .fn<RuntimeImageCatalog["loadPage"]>()
      .mockResolvedValueOnce({ items: [], nextPageToken: "next" })
      .mockResolvedValueOnce({ items: [option] });
    const catalog = { loadPage } as unknown as RuntimeImageCatalog;
    const scope = {
      kind: "ORGANIZATION",
      organizationRef: "org_alpha",
    } as const;
    const signal = new AbortController().signal;
    await expect(
      restoreRuntimeImageOption(catalog, scope, option.ref, signal),
    ).resolves.toEqual(option);
    expect(loadPage).toHaveBeenNthCalledWith(2, scope, "", "next", signal);
  });

  it("не восстанавливает отсутствующий образ и останавливает повтор курсора", async () => {
    const scope = {
      kind: "ORGANIZATION",
      organizationRef: "org_alpha",
    } as const;
    const loadPage = vi
      .fn<RuntimeImageCatalog["loadPage"]>()
      .mockResolvedValue({ items: [] });
    const catalog = { loadPage } as unknown as RuntimeImageCatalog;
    await expect(
      restoreRuntimeImageOption(
        catalog,
        scope,
        "imgart_missing",
        new AbortController().signal,
      ),
    ).rejects.toMatchObject({ code: "IMAGE_ARTIFACT_NOT_CURRENT" });
    loadPage.mockResolvedValue({ items: [], nextPageToken: "same" });
    await expect(
      restoreRuntimeImageOption(
        catalog,
        scope,
        "imgart_missing",
        new AbortController().signal,
      ),
    ).rejects.toThrow("repeated cursor");
  });

  it("не принимает поздний ответ каталога после отмены", async () => {
    const controller = new AbortController();
    const loadPage = vi
      .fn<RuntimeImageCatalog["loadPage"]>()
      .mockImplementation(() => {
        controller.abort();
        return Promise.resolve({ items: [] });
      });
    await expect(
      restoreRuntimeImageOption(
        { loadPage } as unknown as RuntimeImageCatalog,
        { kind: "ORGANIZATION", organizationRef: "org_alpha" },
        "imgart_exact",
        controller.signal,
      ),
    ).rejects.toThrow();
  });
});
