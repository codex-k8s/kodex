import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import {
  createRenderer,
  defineComponent,
  h,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { captureSetupState } from "@/test-utils/setup-harness";
import {
  verifiedInventoryFixture,
  unavailableInventoryFixture,
} from "@/test-utils/image-inventory-fixture";
import type {
  RoleImageArtifact,
  RuntimeEnvironmentInput,
  RuntimeEnvironmentDraft,
} from "@/shared/api/generated/openapi/types.gen";

const runtime = vi.hoisted(() => ({
  environments: {},
  environmentVersions: {},
  environmentVersionCursors: {},
  environmentReadiness: {},
  environmentAgents: {},
  loading: {},
  loadPromotedRoleImageArtifact: vi.fn(),
  loadEnvironment: vi.fn(),
  loadEnvironmentVersions: vi.fn(),
  searchPromotedRoleImagePage: vi.fn(),
}));
const cleanup = vi.hoisted(() => [] as Array<() => void>);
vi.mock("vue", async (original) => ({
  ...(await original<typeof import("vue")>()),
  onMounted: vi.fn(),
  onBeforeUnmount: (callback: () => void) => cleanup.push(callback),
}));
const route = reactive({
  params: { projectRef: "project_1", environmentRef: "environment_1" },
  query: {},
});
vi.mock("vue-router", () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  onBeforeRouteLeave: vi.fn(),
  onBeforeRouteUpdate: vi.fn(),
}));
vi.mock("@/features/runtime/store", () => ({ useRuntimeStore: () => runtime }));
vi.mock("@/features/session/store", () => ({ useSessionStore: () => ({}) }));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import RuntimeEnvironmentEditorPage from "./RuntimeEnvironmentEditorPage.vue";
import { i18n } from "@/app/i18n";
const mountedEditors: Array<() => void> = [];
afterEach(() => {
  for (const dispose of mountedEditors.splice(0)) dispose();
});

async function editor(localized = false) {
  return (await captureSetupState(RuntimeEnvironmentEditorPage, (app) =>
    app.use(localized ? i18n : createI18n({ legacy: false, locale: "ru" })),
  )) as unknown as {
    input: RuntimeEnvironmentInput;
    nameFieldValue: Ref<string>;
    descriptionFieldValue: Ref<string>;
    serverDraft: Ref<RuntimeEnvironmentDraft | undefined>;
    draftSavedAtDisplay: Ref<string>;
    specification: Ref<{ name: string; description: string }>;
    imageArtifact: Ref<RoleImageArtifact | undefined>;
    imageInventoryReady: Ref<boolean>;
    imageLoading: Ref<boolean>;
    imageProblem: Ref<unknown>;
    selectedImage: Ref<{ ref: string; title: string } | undefined>;
    applyRestoredInput(input: RuntimeEnvironmentInput): void;
    applyServerDraft(draft: RuntimeEnvironmentDraft): void;
    loadImageArtifact(recipe: string, artifact: string): Promise<void>;
    load(): Promise<void>;
  };
}

function mountedEditor(): Awaited<ReturnType<typeof editor>> {
  let state!: Awaited<ReturnType<typeof editor>>;
  const renderer = createRenderer<object, object>({
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
    patchProp() {},
  });
  const component = defineComponent({
    setup(props, context) {
      state = (
        RuntimeEnvironmentEditorPage as unknown as {
          setup(
            props: object,
            context: SetupContext,
          ): Awaited<ReturnType<typeof editor>>;
        }
      ).setup(props, context);
      return () => h("div");
    },
  });
  const app = renderer.createApp(component).use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  mountedEditors.push(() => app.unmount());
  return state;
}
function pending<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((accept, refuse) => {
    resolve = accept;
    reject = refuse;
  });
  return { promise, resolve, reject };
}
beforeEach(() => {
  vi.resetAllMocks();
  cleanup.length = 0;
  route.params.projectRef = "project_1";
  route.params.environmentRef = "environment_1";
});

describe("компактная шапка редактора окружения", () => {
  it.each(["ru", "en"] as const)(
    "дата сохранения учитывает locale=%s без изменения server metadata",
    async (locale) => {
      i18n.global.locale.value = locale;
      const state = await editor(true);
      const savedAt = "2026-10-06T11:17:00Z";
      const draft = {
        savedAt,
        version: 3,
        validationDigest: "a".repeat(64),
      } as RuntimeEnvironmentDraft;
      state.serverDraft.value = draft;
      expect(state.draftSavedAtDisplay.value).toBe(
        new Intl.DateTimeFormat(locale, {
          dateStyle: "short",
          timeStyle: "short",
        }).format(new Date(savedAt)),
      );
      expect(state.draftSavedAtDisplay.value).not.toBe(savedAt);
      expect(state.serverDraft.value).toEqual(draft);
      expect(runtime.loadEnvironment).not.toHaveBeenCalled();
      i18n.global.locale.value = "ru";
    },
  );
  it("отсутствующая/некорректная дата не становится сырой строкой", async () => {
    const state = await editor(true);
    for (const savedAt of [undefined, "invalid-date"]) {
      state.serverDraft.value = { savedAt } as RuntimeEnvironmentDraft;
      expect(state.draftSavedAtDisplay.value).toBe(
        i18n.global.t("runtimeOverlay.environmentSavedUnknown"),
      );
    }
  });
  it("оставляет три primary controls, убирает второстепенные и полный digest под native details", () => {
    const source = readFileSync(
      new URL("./RuntimeEnvironmentEditorPage.vue", import.meta.url),
      "utf8",
    );
    const actions = source.slice(
      source.indexOf("<template #actions>"),
      source.indexOf("</template>", source.indexOf("<template #actions>")),
    );
    const primary = actions.slice(
      0,
      actions.indexOf('<details class="environment-secondary-actions">'),
    );
    expect(
      primary.match(/@click="(?:save|validateDraft|preparePublication)"/g),
    ).toHaveLength(3);
    expect(primary).not.toContain('@click="setEnabled');
    expect(actions).toContain('@click="discardDraftOpen = true"');
    expect(actions).toContain(
      "v-if=\"current && hasEnvironmentAction(current, 'DELETE')\"",
    );
    expect(actions).toContain(':disabled="busy || localChanges"');
    const details = source.slice(
      source.indexOf('<details v-if="serverDraft"'),
      source.indexOf(
        "</details>",
        source.indexOf('<details v-if="serverDraft"'),
      ),
    );
    expect(details).not.toMatch(/\sopen(?:[\s=>])/);
    expect(details).toContain(
      "compactIdentifier(serverDraft.validationDigest)",
    );
    expect(details).toContain("{{ serverDraft.validationDigest }}");
    expect(source).toContain(
      "grid-template-columns: repeat(2, minmax(0, 1fr))",
    );
    expect(source).toContain("min-height: var(--control-height, 32px)");
  });
});

function promotedArtifact(ref: string): RoleImageArtifact {
  return {
    scopeKind: "PROJECT",
    organizationRef: "org_synthetic",
    projectRef: "project_1",
    ref,
    version: 1,
    recipeRef: "recipe_own",
    recipeGeneration: 3,
    buildRef: "build_own",
    manifestDigest: `sha256:${"a".repeat(64)}`,
    provenanceSha256: "b".repeat(64),
    promotedReference: `registry.example.test/own@sha256:${"a".repeat(64)}`,
    admissionVerdict: "ACCEPTED",
    promotionState: "PROMOTED",
    promotionRequested: true,
    declaredTools: [],
    verifiedToolInventory: verifiedInventoryFixture(),
  };
}
const ownImageOption = {
  ref: "image_own",
  recipeRef: "recipe_own",
  artifactRef: "image_own",
  generation: 3,
  title: "Свой образ",
};

describe("восстановление точного образа серверного draft", () => {
  it("после замены baseline читает exact promoted artifact и сохраняет tools", async () => {
    const state = await editor(true);
    state.input.imageArtifactRef = "image_baseline";
    state.imageArtifact.value = promotedArtifact("image_baseline");
    const selectedTools = [
      { name: "Git", command: "git", description: "", usageHint: "" },
    ];
    runtime.searchPromotedRoleImagePage.mockResolvedValueOnce({
      items: [ownImageOption],
    });
    runtime.loadPromotedRoleImageArtifact.mockResolvedValueOnce({
      artifact: promotedArtifact("image_own"),
      recipeName: "Свой образ",
    });
    state.applyServerDraft({
      environmentRef: "environment_1",
      specification: {
        ...state.input,
        imageArtifactRef: "image_own",
        tools: selectedTools,
      },
    } as RuntimeEnvironmentDraft);
    expect(state.imageLoading.value).toBe(true);
    expect(state.imageArtifact.value).toBeUndefined();
    await vi.waitFor(() => expect(state.imageInventoryReady.value).toBe(true));
    expect(runtime.searchPromotedRoleImagePage.mock.calls[0]?.[0]).toBe(
      "project_1",
    );
    expect(
      runtime.loadPromotedRoleImageArtifact.mock.calls[0]?.slice(0, 3),
    ).toEqual(["project_1", "recipe_own", "image_own"]);
    expect(state.input.tools).toEqual(selectedTools);
    expect(state.selectedImage.value?.title).toBe("Свой образ");
  });
  it("смена проекта во время exact locator lookup отменяет read и не загружает чужой recipe", async () => {
    const state = mountedEditor();
    const page = pending<{ items: (typeof ownImageOption)[] }>();
    runtime.searchPromotedRoleImagePage.mockReturnValueOnce(page.promise);
    state.applyRestoredInput({ ...state.input, imageArtifactRef: "image_own" });
    const signal = runtime.searchPromotedRoleImagePage.mock
      .calls[0]?.[3] as AbortSignal;
    route.params.projectRef = "project_2";
    expect(signal.aborted).toBe(true);
    page.resolve({ items: [ownImageOption] });
    await vi.waitFor(() => expect(state.imageLoading.value).toBe(false));
    expect(runtime.loadPromotedRoleImageArtifact).not.toHaveBeenCalled();
    expect(state.imageArtifact.value).toBeUndefined();
    expect(state.imageProblem.value).toBeUndefined();
  });
  it("новое чтение отклоняет несовпадающую generation и не ослабляет verified gate", async () => {
    const state = await editor();
    runtime.searchPromotedRoleImagePage.mockResolvedValueOnce({
      items: [ownImageOption],
    });
    runtime.loadPromotedRoleImageArtifact.mockResolvedValueOnce({
      artifact: { ...promotedArtifact("image_own"), recipeGeneration: 2 },
      recipeName: "Старое поколение",
    });
    state.applyRestoredInput({ ...state.input, imageArtifactRef: "image_own" });
    await vi.waitFor(() =>
      expect(state.imageProblem.value).toMatchObject({
        code: "IMAGE_ARTIFACT_NOT_CURRENT",
      }),
    );
    expect(state.imageArtifact.value).toBeUndefined();
    expect(state.imageInventoryReady.value).toBe(false);
  });
});

describe("i18n-поля окружения не изменяют серверную спецификацию", () => {
  it.each(["ru", "en"] as const)(
    "отображает локализованные поля, locale=%s",
    async (locale) => {
      i18n.global.locale.value = locale;
      const state = await editor(true);
      state.input.name = "i18n:DEFAULT_RUNTIME_ENVIRONMENT";
      state.input.description = "i18n:DEFAULT_RUNTIME_ENVIRONMENT_DESCRIPTION";
      expect(state.nameFieldValue.value).toBe(
        i18n.global.t("serverMessages.DEFAULT_RUNTIME_ENVIRONMENT"),
      );
      expect(state.descriptionFieldValue.value).toBe(
        i18n.global.t("serverMessages.DEFAULT_RUNTIME_ENVIRONMENT_DESCRIPTION"),
      );
      const displayedName = state.nameFieldValue.value;
      const displayedDescription = state.descriptionFieldValue.value;
      state.nameFieldValue.value = displayedName;
      state.descriptionFieldValue.value = displayedDescription;
      expect(state.specification.value).toMatchObject({
        name: "i18n:DEFAULT_RUNTIME_ENVIRONMENT",
        description: "i18n:DEFAULT_RUNTIME_ENVIRONMENT_DESCRIPTION",
      });
      state.descriptionFieldValue.value = "Моё описание";
      expect(state.specification.value).toMatchObject({
        name: "i18n:DEFAULT_RUNTIME_ENVIRONMENT",
        description: "Моё описание",
      });
      i18n.global.locale.value = "ru";
    },
  );
});

describe("ответы образа принадлежат текущему окружению", () => {
  it("публикация требует inventory точного выбранного artifact, а не декларации", async () => {
    const state = await editor();
    state.input.imageArtifactRef = "image_1";
    state.imageArtifact.value = {
      ref: "image_1",
      manifestDigest: `sha256:${"a".repeat(64)}`,
      provenanceSha256: "b".repeat(64),
      declaredTools: [{ name: "git", version: "2.53" }],
      verifiedToolInventory: unavailableInventoryFixture(),
    } as RoleImageArtifact;
    expect(state.imageInventoryReady.value).toBe(false);
    state.imageArtifact.value.verifiedToolInventory =
      verifiedInventoryFixture();
    expect(state.imageInventoryReady.value).toBe(true);
    state.input.imageArtifactRef = "image_2";
    expect(state.imageInventoryReady.value).toBe(false);
  });
  it("пустой черновик не показывает ложную загрузку восстановленного образа", async () => {
    const state = await editor();
    state.selectedImage.value = { ref: "old_image", title: "Старый образ" };
    state.applyRestoredInput({
      ...state.input,
      imageArtifactRef: "",
    } as RuntimeEnvironmentInput);
    expect(state.selectedImage.value).toBeUndefined();
    expect(state.imageArtifact.value).toBeUndefined();
  });
  it.each(["success", "failure"])(
    "старый %s не заменяет новый образ",
    async (outcome) => {
      const state = await editor();
      const first = pending<{
        artifact: RoleImageArtifact;
        recipeName: string;
      }>();
      const second = pending<{
        artifact: RoleImageArtifact;
        recipeName: string;
      }>();
      runtime.loadPromotedRoleImageArtifact
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise);
      state.input.imageArtifactRef = "image_1";
      const firstRead = state.loadImageArtifact("recipe_1", "image_1");
      const firstSignal = runtime.loadPromotedRoleImageArtifact.mock
        .calls[0]?.[3] as AbortSignal;
      state.input.imageArtifactRef = "image_2";
      const secondRead = state.loadImageArtifact("recipe_2", "image_2");
      expect(firstSignal.aborted).toBe(true);
      if (outcome === "success")
        first.resolve({
          artifact: { ref: "image_1" } as RoleImageArtifact,
          recipeName: "Старый образ",
        });
      else first.reject(new Error("stale image failure"));
      await firstRead;
      expect(state.imageLoading.value).toBe(true);
      expect(state.imageArtifact.value).toBeUndefined();
      expect(state.imageProblem.value).toBeUndefined();
      second.resolve({
        artifact: { ref: "image_2" } as RoleImageArtifact,
        recipeName: "Новый образ",
      });
      await secondRead;
      expect(state.imageArtifact.value?.ref).toBe("image_2");
      expect(state.imageLoading.value).toBe(false);
    },
  );

  it("не принимает image readback другого проекта", async () => {
    const state = await editor();
    const request = pending<{
      artifact: RoleImageArtifact;
      recipeName: string;
    }>();
    runtime.loadPromotedRoleImageArtifact.mockReturnValueOnce(request.promise);
    state.input.imageArtifactRef = "image_1";
    const loading = state.loadImageArtifact("recipe_1", "image_1");
    route.params.projectRef = "project_2";
    request.resolve({
      artifact: { ref: "image_1" } as RoleImageArtifact,
      recipeName: "Другой проект",
    });
    await loading;
    expect(state.imageArtifact.value).toBeUndefined();
  });

  it("показывает ошибку актуальности образа после завершения чтения", async () => {
    const state = await editor();
    runtime.loadPromotedRoleImageArtifact.mockRejectedValueOnce({
      code: "IMAGE_ARTIFACT_NOT_CURRENT",
      status: 409,
      retryable: false,
    });
    state.input.imageArtifactRef = "image_old";
    await state.loadImageArtifact("recipe_1", "image_old");
    expect(state.imageLoading.value).toBe(false);
    expect(state.imageProblem.value).toMatchObject({
      code: "IMAGE_ARTIFACT_NOT_CURRENT",
    });
  });

  it("закрытие редактора отменяет запрос и отбрасывает позднюю ошибку", async () => {
    vi.stubGlobal("window", { removeEventListener: vi.fn() });
    try {
      const state = await editor();
      const request = pending<RoleImageArtifact>();
      runtime.loadPromotedRoleImageArtifact.mockReturnValueOnce(
        request.promise,
      );
      state.input.imageArtifactRef = "image_1";
      const loading = state.loadImageArtifact("recipe_1", "image_1");
      const signal = runtime.loadPromotedRoleImageArtifact.mock
        .calls[0]?.[3] as AbortSignal;
      for (const callback of cleanup) callback();
      expect(signal.aborted).toBe(true);
      request.reject(new Error("closed image failure"));
      await loading;
      expect(state.imageProblem.value).toBeUndefined();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("старый route load не синхронизирует форму нового окружения", async () => {
    const state = await editor();
    const request = pending<undefined>();
    runtime.loadEnvironment.mockReturnValueOnce(request.promise);
    runtime.loadEnvironmentVersions.mockResolvedValueOnce(undefined);
    const loading = state.load();
    Object.assign(runtime.environments, {
      environment_2: { name: "Сохранённое имя другого окружения" },
    });
    route.params.environmentRef = "environment_2";
    state.input.name = "Новый ввод";
    request.resolve(undefined);
    await loading;
    expect(state.input.name).toBe("Новый ввод");
    expect(runtime.loadPromotedRoleImageArtifact).not.toHaveBeenCalled();
  });
});
