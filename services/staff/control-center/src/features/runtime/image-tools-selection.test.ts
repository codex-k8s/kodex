import { describe, expect, it, vi } from "vitest";
import {
  verifiedInventoryFixture,
  unavailableInventoryFixture,
} from "@/test-utils/image-inventory-fixture";
import type { RoleImageArtifact } from "@/shared/api/generated/openapi/types.gen";
import {
  assertPromotedRuntimeImage,
  runtimeImageOption,
  runtimeImagePagePresentation,
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
  it("различает одноимённые рецепты без digest в строке и не меняет выбор", () => {
    const options = ["recipe_manager_12345678", "recipe_helper_87654321"].map(
      (recipeRef, index) => ({
        ref: `artifact_${String(index)}`,
        recipeRef,
        title: "Базовый системный образ",
        generation: 7,
        description: artifact().promotedReference,
        meta: "generation 7",
      }),
    );
    const before = JSON.stringify(options);
    const result = runtimeImagePagePresentation(
      options,
      (generation) => `Поколение ${String(generation)}`,
    );
    expect(result.map((item) => item.description)).toEqual([
      "Поколение 7 · …12345678",
      "Поколение 7 · …87654321",
    ]);
    for (const [index, item] of result.entries()) {
      expect(item.ref).toBe(options[index]?.ref);
      expect(item.title).toBe("Базовый системный образ");
      expect(item.meta).toBeUndefined();
      expect(item.tooltip).toContain(options[index]?.recipeRef);
      expect(item.tooltip).toContain(artifact().promotedReference);
      expect(runtimeImageOption(item)).toMatchObject({
        ref: options[index]?.ref,
        recipeRef: options[index]?.recipeRef,
        generation: 7,
      });
    }
    expect(JSON.stringify(options)).toBe(before);
  });

  it("коллизия сокращений на текущей странице оставляет полные recipe refs", () => {
    const refs = ["recipe_manager_12345678", "recipe_helper_12345678"];
    const result = runtimeImagePagePresentation(
      refs.map((recipeRef) => ({
        ref: recipeRef,
        recipeRef,
        title: "Образ",
        generation: 1,
      })),
      () => "Поколение 1",
    );
    expect(result.map((item) => item.description)).toEqual(
      refs.map((ref) => `Поколение 1 · ${ref}`),
    );
  });

  it("не выдумывает подпись для sparse metadata и сохраняет короткий exact ref", () => {
    const items = [
      { ref: "without_recipe", title: "Образ", generation: 2 },
      { ref: "without_generation", title: "Образ", recipeRef: "recipe_one" },
      {
        ref: "invalid_generation",
        title: "Образ",
        recipeRef: "recipe_one",
        generation: 0,
      },
    ];
    const label = vi.fn(() => "Поколение 2");
    const result = runtimeImagePagePresentation(items, label);
    expect(result).toEqual(items);
    expect(result[0]).toBe(items[0]);
    expect(label).not.toHaveBeenCalled();
    expect(runtimeImagePagePresentation([], label)).toEqual([]);
    expect(
      runtimeImagePagePresentation(
        [
          {
            ref: "artifact",
            title: "Образ",
            recipeRef: "recipe_one",
            generation: 2,
          },
        ],
        label,
      )[0]?.description,
    ).toBe("Поколение 2 · recipe_one");
  });

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
