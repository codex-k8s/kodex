import { describe, expect, it, vi } from "vitest";
import {
  createSSRApp,
  h,
  type ComputedRef,
  type Ref,
  type SetupContext,
} from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { captureSetupState } from "@/test-utils/setup-harness";
import {
  unavailableInventoryFixture,
  verifiedInventoryFixture,
} from "@/test-utils/image-inventory-fixture";
import type {
  RoleImageArtifact,
  RuntimeEnvironmentImage,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import { verifiedImageTools } from "@/shared/lib/verified-image-tools";
import type {
  RuntimeImageCatalog,
  RuntimeImageOption,
} from "./image-tools-selection";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import Component from "./RuntimeEnvironmentImageToolsSelector.vue";
import ToolsEditor from "./RuntimeEnvironmentToolsEditor.vue";

const option: RuntimeImageOption = {
  ref: "imgart_exact",
  recipeRef: "imgrecipe_exact",
  generation: 3,
  title: "Собственный образ",
};

function artifact(ref = option.ref): RoleImageArtifact {
  return {
    scopeKind: "ORGANIZATION",
    organizationRef: "org_fixture",
    projectRef: "",
    ref,
    version: 1,
    recipeRef: option.recipeRef,
    recipeGeneration: option.generation,
    buildRef: "build_fixture",
    manifestDigest: `sha256:${"a".repeat(64)}`,
    promotedReference: `example.invalid/assistant@sha256:${"a".repeat(64)}`,
    provenanceSha256: "b".repeat(64),
    admissionVerdict: "ACCEPTED",
    promotionState: "PROMOTED",
    promotionRequested: true,
    declaredTools: [{ name: "curl", version: "9.0" }],
    verifiedToolInventory: verifiedInventoryFixture(undefined, undefined, [
      "git",
    ]),
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

interface State {
  loadPage(
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
  ): Promise<AsyncEntityOptionPage>;
  loading: Ref<boolean>;
  pickerPlaceholder: ComputedRef<string>;
  pickerTriggerLabel: ComputedRef<string>;
  artifact: Ref<RoleImageArtifact | undefined>;
  problem: Ref<AppProblem | undefined>;
  selected: Ref<RuntimeImageOption | undefined>;
  pinnedImage: ComputedRef<RuntimeEnvironmentImage | undefined>;
  select(option: AsyncEntityOption): Promise<void>;
  clear(value: unknown): void;
}

async function setup(
  catalog: RuntimeImageCatalog,
  imageArtifactRef = "",
  disabled = false,
): Promise<State> {
  return (await captureSetupState(
    Component,
    (app) =>
      app.use(
        createI18n({
          legacy: false,
          locale: "ru",
          messages: {
            ru: {
              roleImages: { generationLabel: "Поколение {generation}" },
              runtime: {
                choosePromotedImage: "Выберите собранный и promoted образ",
                loadingSelectedImage: "Загрузка выбранного образа…",
              },
            },
          },
        }),
      ),
    {
      resourceScope: { kind: "ORGANIZATION", organizationRef: "org_fixture" },
      imageArtifactRef,
      tools: [],
      disabled,
      catalog,
    },
  )) as unknown as State;
}

function catalog() {
  return {
    loadPage: vi
      .fn<RuntimeImageCatalog["loadPage"]>()
      .mockResolvedValue({ items: [] }),
    loadArtifact: vi
      .fn<RuntimeImageCatalog["loadArtifact"]>()
      .mockResolvedValue({
        artifact: artifact(),
        recipeName: option.title,
      }),
  };
}

describe("Состояния выбора собственного подтверждённого образа", () => {
  it.each([true, false])(
    "readonly reference/digest видны только для точного pinned выбора: %s",
    async (exact) => {
      const reader = catalog();
      reader.loadArtifact.mockRejectedValue(
        new AppProblem({
          status: 409,
          code: "IMAGE_ARTIFACT_NOT_CURRENT",
          retryable: false,
          kind: "conflict",
        }),
      );
      const source = Component as unknown as {
        setup(props: object, context: SetupContext): State;
      };
      const hydrated = {
        ...Component,
        async setup(props: object, context: SetupContext) {
          const state = source.setup(props, context);
          await vi.waitFor(() => expect(state.loading.value).toBe(false));
          return state;
        },
      };
      const image = artifact();
      const app = createSSRApp(hydrated, {
        resourceScope: { kind: "ORGANIZATION", organizationRef: "org_fixture" },
        imageArtifactRef: exact ? option.ref : "imgart_other",
        currentImage: {
          artifactRef: option.ref,
          recipeRef: option.recipeRef,
          recipeGeneration: option.generation,
          reference: image.promotedReference,
          digest: image.manifestDigest,
        },
        tools: [],
        disabled: false,
        catalog: reader,
      });
      app.use(
        createI18n({
          legacy: false,
          locale: "ru",
          missingWarn: false,
          fallbackWarn: false,
          messages: {
            ru: {
              runtime: { exactImage: "Точный образ" },
              roleImages: { generationLabel: "Поколение {generation}" },
            },
          },
        }),
      );
      const html = await renderToString(app);
      if (exact) {
        expect(html).toContain('class="image-tools-selector__pinned"');
        expect(html).toContain(image.promotedReference);
        expect(html).toContain(image.manifestDigest);
        expect(html).toContain('aria-label="Точный образ · Поколение 3"');
      } else {
        expect(html).not.toContain('class="image-tools-selector__pinned"');
        expect(html).not.toContain(image.promotedReference);
      }
      expect(html).not.toContain('type="checkbox"');
    },
  );
  it("сохраняет точную опубликованную подпись при смене recipe candidate, не открывая inventory", async () => {
    const reader = catalog();
    reader.loadArtifact.mockRejectedValue(
      new AppProblem({
        status: 409,
        code: "IMAGE_ARTIFACT_NOT_CURRENT",
        retryable: false,
        kind: "conflict",
      }),
    );
    const currentImage = {
      artifactRef: option.ref,
      recipeRef: option.recipeRef,
      recipeGeneration: option.generation,
      reference: artifact().promotedReference ?? "",
      digest: artifact().manifestDigest,
    };
    const state = (await captureSetupState(
      Component,
      (app) =>
        app.use(
          createI18n({
            legacy: false,
            locale: "ru",
            missingWarn: false,
            fallbackWarn: false,
            messages: {
              ru: {
                runtime: { exactImage: "Точный образ" },
                roleImages: { generationLabel: "Поколение {generation}" },
              },
            },
          }),
        ),
      {
        resourceScope: { kind: "ORGANIZATION", organizationRef: "org_fixture" },
        imageArtifactRef: option.ref,
        currentImage,
        tools: [
          {
            name: "Git",
            command: "git",
            description: "Без изменений",
            usageHint: "",
          },
        ],
        disabled: false,
        catalog: reader,
      },
    )) as unknown as State;
    await vi.waitFor(() => expect(state.loading.value).toBe(false));
    expect(state.problem.value?.code).toBe("IMAGE_ARTIFACT_NOT_CURRENT");
    expect(state.selected.value).toMatchObject({
      ref: option.ref,
      generation: option.generation,
      description: currentImage.reference,
    });
    expect(state.pickerTriggerLabel.value).toBe("Точный образ · Поколение 3");
    expect(state.pinnedImage.value).toEqual(currentImage);
    expect(state.artifact.value).toBeUndefined();
    expect(verifiedImageTools(state.artifact.value)).toEqual([]);
    expect(reader.loadArtifact).toHaveBeenCalledTimes(1);
    expect(reader.loadPage).not.toHaveBeenCalled();
  });
  it("SYSTEM список показывает поколение/ref, сохраняя название выбранного образа и exact metadata", async () => {
    const reader = catalog();
    const image = { ...option, description: artifact().promotedReference };
    reader.loadPage.mockResolvedValue({ items: [image] });
    const state = await setup(reader);
    const page = await state.loadPage(
      "",
      undefined,
      new AbortController().signal,
    );
    expect(page.items[0]).toMatchObject({
      ref: option.ref,
      title: option.title,
      description: "Поколение 3 · …pe_exact",
    });
    expect(page.items[0]?.tooltip).toContain(artifact().promotedReference);
    expect(reader.loadPage).toHaveBeenCalledTimes(1);
    expect(reader.loadArtifact).not.toHaveBeenCalled();
    await state.select(page.items[0] ?? option);
    expect(state.selected.value?.title).toBe(option.title);
    expect(state.artifact.value?.promotedReference).toBe(
      artifact().promotedReference,
    );
  });
  it("после exact ORG read доступное имя кнопки совпадает с видимым выбранным образом", async () => {
    const reader = catalog();
    const source = Component as unknown as {
      setup(props: object, context: SetupContext): State;
    };
    const hydrated = {
      ...Component,
      async setup(props: object, context: SetupContext) {
        const state = source.setup(props, context);
        await vi.waitFor(() => expect(state.loading.value).toBe(false));
        return state;
      },
    };
    const app = createSSRApp(hydrated, {
      resourceScope: { kind: "ORGANIZATION", organizationRef: "org_fixture" },
      imageArtifactRef: option.ref,
      currentImage: {
        artifactRef: option.ref,
        recipeRef: option.recipeRef,
        recipeGeneration: option.generation,
        reference: artifact().promotedReference,
      },
      tools: [],
      disabled: false,
      catalog: reader,
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: { runtime: { choosePromotedImage: "Выберите образ" } },
        },
        missingWarn: false,
        fallbackWarn: false,
      }),
    );
    const html = await renderToString(app);
    expect(html).toContain(`aria-label="${option.title}"`);
    expect(html).toMatch(/<strong\b[^>]*>Собственный образ<\/strong>/u);
    expect(html).not.toContain('class="async-picker__placeholder"');
    expect(reader.loadArtifact).toHaveBeenCalledExactlyOnceWith(
      { kind: "ORGANIZATION", organizationRef: "org_fixture" },
      option.recipeRef,
      option.ref,
      expect.any(AbortSignal),
    );
    expect(reader.loadPage).not.toHaveBeenCalled();
  });

  it("доступная подпись не использует имя другого artifact", async () => {
    const state = await setup(catalog(), "imgart_other");
    await vi.waitFor(() => expect(state.loading.value).toBe(false));
    state.selected.value = option;
    expect(state.pickerTriggerLabel.value).toBe(
      "Выберите собранный и promoted образ",
    );
  });

  it("ожидание текущего выбранного образа показывает загрузку, затем точное название", async () => {
    const reader = catalog();
    const pending =
      deferred<Awaited<ReturnType<RuntimeImageCatalog["loadPage"]>>>();
    reader.loadPage.mockReturnValue(pending.promise);
    const state = await setup(reader, option.ref);
    expect(state.loading.value).toBe(true);
    expect(state.selected.value).toBeUndefined();
    expect(state.pickerPlaceholder.value).toBe("Загрузка выбранного образа…");
    expect(state.pickerTriggerLabel.value).toBe("Загрузка выбранного образа…");
    pending.resolve({ items: [option] });
    await vi.waitFor(() => expect(state.loading.value).toBe(false));
    expect(state.selected.value?.title).toBe(option.title);
    expect(state.pickerTriggerLabel.value).toBe(option.title);
    expect(state.pickerPlaceholder.value).toBe(
      "Выберите собранный и promoted образ",
    );
  });
  it("отказ чтения текущего образа не сохраняет ложное состояние загрузки", async () => {
    const reader = catalog();
    const pending =
      deferred<Awaited<ReturnType<RuntimeImageCatalog["loadPage"]>>>();
    reader.loadPage.mockReturnValue(pending.promise);
    const state = await setup(reader, option.ref);
    expect(state.pickerPlaceholder.value).toBe("Загрузка выбранного образа…");
    pending.reject(new Error("Synthetic catalog failure"));
    await vi.waitFor(() => expect(state.loading.value).toBe(false));
    expect(state.problem.value).toBeDefined();
    expect(state.artifact.value).toBeUndefined();
    expect(state.selected.value).toBeUndefined();
    expect(state.pickerTriggerLabel.value).toBe(
      "Выберите собранный и promoted образ",
    );
    expect(state.pickerPlaceholder.value).toBe(
      "Выберите собранный и promoted образ",
    );
  });
  it("первый выбор без закреплённого imageArtifactRef не выдаётся за загрузку выбранного образа", async () => {
    const reader = catalog();
    const pending =
      deferred<Awaited<ReturnType<RuntimeImageCatalog["loadArtifact"]>>>();
    reader.loadArtifact.mockReturnValue(pending.promise);
    const state = await setup(reader);
    const choosing = state.select(option);
    expect(state.loading.value).toBe(true);
    expect(state.pickerPlaceholder.value).toBe(
      "Выберите собранный и promoted образ",
    );
    pending.resolve({ artifact: artifact(), recipeName: option.title });
    await choosing;
  });
  it("пустой выбор не начинает загрузку metadata или инструментов", async () => {
    const reader = catalog();
    const state = await setup(reader);
    expect(state.loading.value).toBe(false);
    expect(state.artifact.value).toBeUndefined();
    expect(state.problem.value).toBeUndefined();
    expect(reader.loadPage).not.toHaveBeenCalled();
    expect(reader.loadArtifact).not.toHaveBeenCalled();
  });

  it("отсутствующий образ завершает загрузку закрытым состоянием, не вечным spinner", async () => {
    const reader = catalog();
    const state = await setup(reader, "imgart_missing");
    await vi.waitFor(() => expect(state.loading.value).toBe(false));
    expect(state.problem.value?.code).toBe("IMAGE_ARTIFACT_NOT_CURRENT");
    expect(state.artifact.value).toBeUndefined();
    expect(reader.loadPage).toHaveBeenCalledTimes(1);
    expect(reader.loadArtifact).not.toHaveBeenCalled();
  });

  it("повреждённый option не запускает чтение и не оставляет загрузку", async () => {
    const reader = catalog();
    const state = await setup(reader);
    const invalid: RuntimeImageOption = { ...option, generation: 0 };
    await state.select(invalid);
    expect(state.loading.value).toBe(false);
    expect(state.problem.value).toBeDefined();
    expect(state.artifact.value).toBeUndefined();
    expect(reader.loadArtifact).not.toHaveBeenCalled();
  });

  it("ошибка metadata завершает загрузку без инструментов из декларации", async () => {
    const reader = catalog();
    reader.loadArtifact.mockRejectedValue(
      new Error("Synthetic metadata failure"),
    );
    const state = await setup(reader);
    await state.select(option);
    expect(state.loading.value).toBe(false);
    expect(state.problem.value).toBeDefined();
    expect(state.artifact.value).toBeUndefined();
    expect(verifiedImageTools(state.artifact.value)).toEqual([]);
  });

  it("historical inventory UNAVAILABLE не превращается в verified declaredTools", async () => {
    const reader = catalog();
    reader.loadArtifact.mockResolvedValue({
      artifact: {
        ...artifact(),
        verifiedToolInventory: unavailableInventoryFixture(),
      },
      recipeName: option.title,
    });
    const state = await setup(reader);
    await state.select(option);
    expect(state.loading.value).toBe(false);
    expect(state.problem.value?.code).toBe("IMAGE_ARTIFACT_NOT_CURRENT");
    expect(state.artifact.value).toBeUndefined();
    expect(verifiedImageTools(state.artifact.value)).toEqual([]);
  });

  it("успешное чтение открывает только observed программы и завершает загрузку", async () => {
    const state = await setup(catalog());
    await state.select(option);
    expect(state.loading.value).toBe(false);
    expect(state.problem.value).toBeUndefined();
    expect(state.selected.value?.title).toBe(option.title);
    expect(verifiedImageTools(state.artifact.value)).toEqual([
      { name: "git", version: "1.2.3" },
    ]);
  });

  it("очистка прерывает ожидание, поздний metadata ACK не возвращает выбранный образ", async () => {
    const reader = catalog();
    const pending =
      deferred<Awaited<ReturnType<RuntimeImageCatalog["loadArtifact"]>>>();
    reader.loadArtifact.mockReturnValue(pending.promise);
    const state = await setup(reader);
    const choosing = state.select(option);
    expect(state.loading.value).toBe(true);
    state.clear("");
    expect(state.loading.value).toBe(false);
    expect(reader.loadArtifact.mock.calls[0]?.[3].aborted).toBe(true);
    pending.resolve({ artifact: artifact(), recipeName: option.title });
    await choosing;
    expect(state.artifact.value).toBeUndefined();
    expect(state.selected.value).toBeUndefined();
    expect(state.problem.value).toBeUndefined();
  });

  it("поздний отказ первого выбора не стирает второй успешный выбор", async () => {
    const reader = catalog();
    const pending =
      deferred<Awaited<ReturnType<RuntimeImageCatalog["loadArtifact"]>>>();
    reader.loadArtifact
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce({
        artifact: artifact("imgart_next"),
        recipeName: "Следующий образ",
      });
    const state = await setup(reader);
    const first = state.select(option);
    await state.select({ ...option, ref: "imgart_next" });
    expect(state.artifact.value?.ref).toBe("imgart_next");
    pending.reject(new Error("Synthetic obsolete failure"));
    await first;
    expect(state.loading.value).toBe(false);
    expect(state.artifact.value?.ref).toBe("imgart_next");
    expect(state.problem.value).toBeUndefined();
  });

  it("disabled selector не запускает изменение или чтение metadata", async () => {
    const reader = catalog();
    const state = await setup(reader, "", true);
    await state.select(option);
    state.clear("");
    expect(state.loading.value).toBe(false);
    expect(reader.loadArtifact).not.toHaveBeenCalled();
  });

  it.each([
    {
      imageSelected: false,
      inventoryAvailable: false,
      loading: false,
      text: "Выберите образ",
    },
    {
      imageSelected: true,
      inventoryAvailable: false,
      loading: false,
      text: "Нет подтверждённого состава",
    },
    {
      imageSelected: true,
      inventoryAvailable: true,
      loading: false,
      text: "Нет доступных программ",
    },
    {
      imageSelected: true,
      inventoryAvailable: false,
      loading: true,
      text: "Загрузка",
    },
  ])(
    "показывает компактное состояние инструментов: $text",
    async ({ text, ...state }) => {
      const app = createSSRApp({
        render: () =>
          h(ToolsEditor, { ...state, tools: [], catalog: [], disabled: true }),
      });
      app.use(
        createI18n({
          legacy: false,
          locale: "ru",
          messages: {
            ru: {
              common: { loading: "Загрузка" },
              runtime: {
                verifiedTools: "Проверенные программы",
                verifiedToolsHelp: "Только подтверждённый состав",
                selectedToolsCount: "Выбрано {selected} из {total}",
                chooseImageFirst: "Выберите образ",
                imageInventoryUnavailable: "Нет подтверждённого состава",
                noVerifiedTools: "Нет доступных программ",
              },
            },
          },
        }),
      );
      const html = await renderToString(app);
      expect(html).toContain(text);
      expect(html).not.toContain('type="checkbox"');
      if (!state.loading) expect(html).not.toContain("Загрузка");
    },
  );
});
