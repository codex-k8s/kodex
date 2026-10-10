import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
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
import { verifiedImageTools } from "@/shared/lib/verified-image-tools";
import {
  verifiedInventoryFixture,
  unavailableInventoryFixture,
} from "@/test-utils/image-inventory-fixture";
import type {
  RoleImageArtifact,
  RuntimeEnvironmentInput,
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentSet,
  RevisionImpactPlan,
} from "@/shared/api/generated/openapi/types.gen";

const runtime = vi.hoisted(() => ({
  environments: {},
  environmentVersions: {},
  environmentVersionCursors: {},
  environmentReadiness: {},
  environmentAgents: {},
  environmentAgentCursors: {},
  loading: {},
  loadPromotedRoleImageArtifact: vi.fn(),
  loadEnvironment: vi.fn(),
  loadEnvironmentVersions: vi.fn(),
  searchPromotedRoleImagePage: vi.fn(),
}));
const cleanup = vi.hoisted(() => [] as Array<() => void>);
const session = vi.hoisted(() => ({
  beginRuntimeEnvironmentPolicyReauth: vi.fn(),
}));
const draftApi = vi.hoisted(() => ({
  createEnvironmentDraft: vi.fn(),
  saveEnvironmentDraft: vi.fn(),
  publishEnvironmentDraft: vi.fn(),
  prepareEnvironmentPublication: vi.fn(),
  readEnvironmentDraft: vi.fn(),
  transitionEnvironmentDraft: vi.fn(),
}));
const router = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn() }));
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
  useRouter: () => router,
  onBeforeRouteLeave: vi.fn(),
  onBeforeRouteUpdate: vi.fn(),
}));
vi.mock("@/features/runtime/store", () => ({ useRuntimeStore: () => runtime }));
vi.mock("@/features/session/store", () => ({ useSessionStore: () => session }));
vi.mock("@/features/runtime/environment-drafts", async (original) => ({
  ...(await original<typeof import("@/features/runtime/environment-drafts")>()),
  ...draftApi,
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import RuntimeEnvironmentEditorPage from "./RuntimeEnvironmentEditorPage.vue";
import { i18n } from "@/app/i18n";
import { AppProblem } from "@/shared/api/problem";
import {
  environmentDraftReauthKey,
  rememberEnvironmentDraft,
} from "@/features/runtime/environment-draft-reauth";
import {
  createRuntimeEnvironmentPolicyIntent,
  recordRuntimeEnvironmentPolicyReauthCompletion,
} from "@/features/session/reauth";
const mountedEditors: Array<() => void> = [];
afterEach(() => {
  for (const dispose of mountedEditors.splice(0)) dispose();
  vi.unstubAllGlobals();
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
    draftDirty: Ref<boolean>;
    problem: Ref<AppProblem | undefined>;
    reauthRestored: Ref<boolean>;
    specification: Ref<{ name: string; description: string }>;
    imageArtifact: Ref<RoleImageArtifact | undefined>;
    imageInventoryReady: Ref<boolean>;
    imageLoading: Ref<boolean>;
    imageProblem: Ref<unknown>;
    imageBadgeLabel: Ref<string>;
    publicationPlan: Ref<RevisionImpactPlan | undefined>;
    selectedImage: Ref<{ ref: string; title: string } | undefined>;
    applyRestoredInput(input: RuntimeEnvironmentInput): void;
    applyServerDraft(draft: RuntimeEnvironmentDraft): void;
    loadImageArtifact(recipe: string, artifact: string): Promise<void>;
    selectImage(option: typeof ownImageOption): Promise<void>;
    loadImagePage(query: string): Promise<AsyncEntityOptionPage>;
    load(): Promise<void>;
    validateDraft(): Promise<void>;
    publish(selected: string[]): Promise<void>;
    preparePublication(): Promise<void>;
    restoreAfterFreshAuthentication(): Promise<void>;
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
  route.query = {};
  runtime.environments = {};
});

function browserStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    key: (index) => [...values.keys()][index] ?? null,
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
    clear: () => values.clear(),
  };
}
function savedDraft(): RuntimeEnvironmentDraft {
  return {
    scopeKind: "PROJECT",
    organizationRef: "org_synthetic",
    projectRef: "project_1",
    environmentRef: "environment_1",
    ref: "draft_synthetic",
    version: 1,
    state: "DRAFT",
    expectedEnvironmentVersion: 1,
    validationDigest: "",
    diagnostics: [],
    specification: {
      name: "Окружение",
      description: "",
      imageArtifactRef: "",
      tools: [],
      values: [],
      secretBindings: [],
    },
  };
}
const freshAuthenticationProblem = new AppProblem({
  status: 403,
  code: "FRESH_AUTHENTICATION_REQUIRED",
  retryable: false,
  kind: "forbidden",
});

describe("проверка черновика после свежего SSO", () => {
  it("PROJECT список использует такую же компактную подпись без нового чтения", async () => {
    const image = {
      ref: "artifact_fixture",
      title: "Базовый системный образ",
      recipeRef: "imgrecipe_exact",
      generation: 3,
      description: "example.invalid/image@sha256:" + "a".repeat(64),
    };
    runtime.searchPromotedRoleImagePage.mockResolvedValue({ items: [image] });
    const state = await editor(true);
    const page = await state.loadImagePage(image.recipeRef);
    expect(page.items[0]).toMatchObject({
      ref: image.ref,
      title: image.title,
      description: "Поколение 3 · …pe_exact",
    });
    expect(page.items[0]?.tooltip).toContain(image.description);
    expect(runtime.searchPromotedRoleImagePage).toHaveBeenCalledTimes(1);
    expect(runtime.loadPromotedRoleImageArtifact).not.toHaveBeenCalled();
  });
  beforeEach(() => {
    runtime.environments = {
      environment_1: { ref: "environment_1", version: 1 },
    };
  });
  it("на fresh 403 сохраняет только точный draft ref/version и начинает SSO без повторной mutation", async () => {
    const storage = browserStorage();
    vi.stubGlobal("window", { sessionStorage: storage });
    route.query = { assistantForm: "1" };
    const state = await editor(true);
    const draft = savedDraft();
    state.applyServerDraft(draft);
    expect(state.draftDirty.value).toBe(false);
    draftApi.transitionEnvironmentDraft.mockRejectedValueOnce(
      freshAuthenticationProblem,
    );
    await state.validateDraft();
    expect(
      session.beginRuntimeEnvironmentPolicyReauth,
    ).toHaveBeenCalledExactlyOnceWith({
      environmentRef: "environment_1",
      operation: "PUBLISH",
      projectRef: "project_1",
      surface: "assistant",
    });
    expect(
      JSON.parse(storage.getItem(environmentDraftReauthKey) ?? "{}") as unknown,
    ).toMatchObject({ ref: draft.ref, version: draft.version });
    expect(storage.getItem(environmentDraftReauthKey)).not.toContain(
      "specification",
    );
    expect(draftApi.transitionEnvironmentDraft).toHaveBeenCalledTimes(1);
    expect(state.serverDraft.value).toEqual(draft);
  });
  it("после возврата читает точный server draft, а Validate запускает только следующий явный клик", async () => {
    const storage = browserStorage();
    vi.stubGlobal("window", { sessionStorage: storage });
    const draft = savedDraft();
    rememberEnvironmentDraft(draft, storage);
    recordRuntimeEnvironmentPolicyReauthCompletion(
      createRuntimeEnvironmentPolicyIntent(
        "project_1",
        "PUBLISH",
        "environment_1",
      ),
      storage,
    );
    draftApi.readEnvironmentDraft.mockResolvedValueOnce(draft);
    const state = await editor(true);
    await state.restoreAfterFreshAuthentication();
    expect(draftApi.readEnvironmentDraft.mock.calls[0]?.slice(0, 2)).toEqual([
      "project_1",
      draft.ref,
    ]);
    expect(state.serverDraft.value).toEqual(draft);
    expect(state.reauthRestored.value).toBe(true);
    expect(router.replace).toHaveBeenCalledExactlyOnceWith({
      query: { draftRef: draft.ref },
    });
    expect(storage.getItem(environmentDraftReauthKey)).toBeNull();
    expect(draftApi.transitionEnvironmentDraft).not.toHaveBeenCalled();
    expect(draftApi.createEnvironmentDraft).not.toHaveBeenCalled();
    expect(draftApi.saveEnvironmentDraft).not.toHaveBeenCalled();
    expect(draftApi.publishEnvironmentDraft).not.toHaveBeenCalled();
    draftApi.transitionEnvironmentDraft.mockResolvedValueOnce({
      ...draft,
      state: "VALID",
      version: 2,
    });
    await state.validateDraft();
    expect(draftApi.transitionEnvironmentDraft).toHaveBeenCalledTimes(1);
    expect(state.serverDraft.value?.state).toBe("VALID");
  });
  it("при изменившейся версии после SSO не принимает новую ревизию и не выполняет mutation", async () => {
    const storage = browserStorage();
    vi.stubGlobal("window", { sessionStorage: storage });
    const draft = savedDraft();
    rememberEnvironmentDraft(draft, storage);
    recordRuntimeEnvironmentPolicyReauthCompletion(
      createRuntimeEnvironmentPolicyIntent(
        "project_1",
        "PUBLISH",
        "environment_1",
      ),
      storage,
    );
    draftApi.readEnvironmentDraft.mockResolvedValueOnce({
      ...draft,
      version: 2,
    });
    const state = await editor(true);
    await expect(state.restoreAfterFreshAuthentication()).rejects.toThrow(
      "version",
    );
    expect(state.serverDraft.value).toBeUndefined();
    expect(state.reauthRestored.value).toBe(false);
    expect(draftApi.transitionEnvironmentDraft).not.toHaveBeenCalled();
    expect(router.replace).not.toHaveBeenCalled();
  });
  it("несохранённый или изменённый локально draft не запускает проверку/SSO", async () => {
    const state = await editor(true);
    await state.validateDraft();
    state.applyServerDraft(savedDraft());
    state.input.description = "Несохранённое изменение";
    await state.validateDraft();
    expect(draftApi.transitionEnvironmentDraft).not.toHaveBeenCalled();
    expect(session.beginRuntimeEnvironmentPolicyReauth).not.toHaveBeenCalled();
    expect(draftApi.createEnvironmentDraft).not.toHaveBeenCalled();
    expect(draftApi.saveEnvironmentDraft).not.toHaveBeenCalled();
    expect(draftApi.publishEnvironmentDraft).not.toHaveBeenCalled();
  });
  it("сохранённый draft нового окружения возвращается по CREATE без повторного создания", async () => {
    const storage = browserStorage();
    vi.stubGlobal("window", { sessionStorage: storage });
    Reflect.deleteProperty(route.params, "environmentRef");
    const draft = {
      ...savedDraft(),
      environmentRef: undefined,
      expectedEnvironmentVersion: 0,
    };
    const state = await editor(true);
    state.applyServerDraft(draft);
    draftApi.transitionEnvironmentDraft.mockRejectedValueOnce(
      freshAuthenticationProblem,
    );
    await state.validateDraft();
    expect(
      session.beginRuntimeEnvironmentPolicyReauth,
    ).toHaveBeenCalledExactlyOnceWith({
      operation: "CREATE",
      projectRef: "project_1",
    });
    recordRuntimeEnvironmentPolicyReauthCompletion(
      createRuntimeEnvironmentPolicyIntent("project_1", "CREATE"),
      storage,
    );
    draftApi.readEnvironmentDraft.mockResolvedValueOnce(draft);
    await state.restoreAfterFreshAuthentication();
    expect(state.serverDraft.value).toEqual(draft);
    expect(draftApi.transitionEnvironmentDraft).toHaveBeenCalledTimes(1);
  });
  it("поздний fresh 403 и readback не переходят в другой project scope", async () => {
    const storage = browserStorage();
    vi.stubGlobal("window", { sessionStorage: storage });
    const draft = savedDraft();
    const state = await editor(true);
    state.applyServerDraft(draft);
    const validation = pending<RuntimeEnvironmentDraft>();
    draftApi.transitionEnvironmentDraft.mockReturnValueOnce(validation.promise);
    const checking = state.validateDraft();
    route.params.projectRef = "project_other";
    validation.reject(freshAuthenticationProblem);
    await checking;
    expect(session.beginRuntimeEnvironmentPolicyReauth).not.toHaveBeenCalled();
    expect(storage.getItem(environmentDraftReauthKey)).toBeNull();
    route.params.projectRef = "project_1";
    rememberEnvironmentDraft(draft, storage);
    recordRuntimeEnvironmentPolicyReauthCompletion(
      createRuntimeEnvironmentPolicyIntent(
        "project_1",
        "PUBLISH",
        "environment_1",
      ),
      storage,
    );
    const readback = pending<RuntimeEnvironmentDraft>();
    draftApi.readEnvironmentDraft.mockReturnValueOnce(readback.promise);
    const restoring = state.restoreAfterFreshAuthentication();
    route.params.projectRef = "project_other";
    readback.resolve(draft);
    await restoring;
    expect(state.reauthRestored.value).toBe(false);
    expect(router.replace).not.toHaveBeenCalled();
    expect(draftApi.transitionEnvironmentDraft).toHaveBeenCalledTimes(1);
  });
  it("обычный 403 не переходит в SSO, а ошибка redirect очищает metadata и остаётся видимой", async () => {
    const storage = browserStorage();
    vi.stubGlobal("window", { sessionStorage: storage });
    const state = await editor(true);
    state.applyServerDraft(savedDraft());
    const forbidden = new AppProblem({
      status: 403,
      code: "ACCESS_DENIED",
      retryable: false,
      kind: "forbidden",
    });
    draftApi.transitionEnvironmentDraft.mockRejectedValueOnce(forbidden);
    await state.validateDraft();
    expect(state.problem.value).toBe(forbidden);
    expect(session.beginRuntimeEnvironmentPolicyReauth).not.toHaveBeenCalled();
    session.beginRuntimeEnvironmentPolicyReauth.mockRejectedValueOnce(
      forbidden,
    );
    draftApi.transitionEnvironmentDraft.mockRejectedValueOnce(
      freshAuthenticationProblem,
    );
    await state.validateDraft();
    expect(state.problem.value).toBe(forbidden);
    expect(storage.getItem(environmentDraftReauthKey)).toBeNull();
    expect(draftApi.transitionEnvironmentDraft).toHaveBeenCalledTimes(2);
  });
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
  it("история не сжимает дату кнопкой восстановления и сохраняет gates/дозагрузку", () => {
    const source = readFileSync(
      new URL("./RuntimeEnvironmentEditorPage.vue", import.meta.url),
      "utf8",
    );
    const history = source.slice(
      source.indexOf('<aside class="panel revision-panel">'),
      source.indexOf(
        "</aside>",
        source.indexOf('<aside class="panel revision-panel">'),
      ),
    );
    expect(history.includes('class="revision-meta"')).toBe(true);
    expect(history.includes('class="revision-actions"')).toBe(true);
    expect(history.includes('class="icon-button"')).toBe(true);
    expect(history.includes(":title=\"$t('runtime.rollback')\"")).toBe(true);
    expect(history.includes(":aria-label=\"$t('runtime.rollback')\"")).toBe(
      true,
    );
    expect(
      history.includes("version.ref !== current?.currentVersion.ref"),
    ).toBe(true);
    expect(history.includes("hasEnvironmentAction(current, 'ROLLBACK')")).toBe(
      true,
    );
    expect(history.includes(':disabled="busy || localChanges"')).toBe(true);
    expect(history.includes('@click="rollback(version.ref)"')).toBe(true);
    expect(history.includes('ref="versionSentinel"')).toBe(true);
    expect(
      history.includes("runtime.environmentVersionCursors[environmentRef]"),
    ).toBe(true);
    expect(
      /\.revision-scroll > article\s*{[^}]*grid-template-columns: minmax\(0, 1fr\);/.test(
        source,
      ),
    ).toBe(true);
    expect(/\.revision-summary\s*{[^}]*flex-wrap: wrap;/.test(source)).toBe(
      true,
    );
    expect(source.includes("max-height: min(560px, calc(100vh - 270px))")).toBe(
      true,
    );
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

describe("смена образа сохраняет выбранные инструменты", () => {
  function selectedTools() {
    return verifiedImageTools(promotedArtifact("image_own"))
      .slice(0, 38)
      .map((tool) => ({
        name: `Мой ${tool.name}`,
        command: tool.name,
        description: `Описание ${tool.name}`,
        usageHint: `Подсказка ${tool.name}`,
      }));
  }

  it("не стирает 38 metadata без OLD inventory и пересекает только после exact verified NEW read", async () => {
    const state = mountedEditor();
    const tools = selectedTools();
    state.input.imageArtifactRef = "image_old";
    state.input.tools = tools;
    const request = pending<{
      artifact: RoleImageArtifact;
      recipeName: string;
    }>();
    runtime.loadPromotedRoleImageArtifact.mockReturnValueOnce(request.promise);
    const selecting = state.selectImage(ownImageOption);
    expect(state.input.tools).toEqual(tools);
    expect(state.imageInventoryReady.value).toBe(false);
    request.resolve({
      artifact: promotedArtifact("image_own"),
      recipeName: "Новый",
    });
    await selecting;
    expect(state.input.tools).toEqual(tools);
    expect(state.input.tools).toHaveLength(38);
    expect(state.imageInventoryReady.value).toBe(true);
  });

  it("не выбирает дополнительные команды и удаляет только отсутствующую verified command", async () => {
    const state = mountedEditor();
    const tools = selectedTools();
    state.input.tools = [
      ...tools,
      {
        name: "Недоступный",
        command: "not-verified",
        description: "Своё",
        usageHint: "Не терять",
      },
    ];
    runtime.loadPromotedRoleImageArtifact.mockResolvedValueOnce({
      artifact: promotedArtifact("image_own"),
      recipeName: "Новый",
    });
    await state.selectImage(ownImageOption);
    expect(state.input.tools).toEqual(tools);
  });

  it.each(["failure", "unavailable", "scope", "generation"] as const)(
    "не стирает selection при %s нового inventory",
    async (caseName) => {
      const state = mountedEditor();
      const tools = selectedTools();
      state.input.tools = tools;
      const request = pending<{
        artifact: RoleImageArtifact;
        recipeName: string;
      }>();
      runtime.loadPromotedRoleImageArtifact.mockReturnValueOnce(
        request.promise,
      );
      const selecting = state.selectImage(ownImageOption);
      if (caseName === "scope") route.params.projectRef = "project_other";
      if (caseName === "failure")
        request.reject(new Error("Synthetic image failure"));
      else {
        const artifact = promotedArtifact("image_own");
        if (caseName === "unavailable")
          artifact.verifiedToolInventory = unavailableInventoryFixture();
        if (caseName === "generation") artifact.recipeGeneration += 1;
        request.resolve({ artifact, recipeName: "Новый" });
      }
      await selecting;
      expect(state.input.tools).toEqual(tools);
      expect(state.imageInventoryReady.value).toBe(false);
    },
  );
});

describe("восстановление точного образа серверного draft", () => {
  it("после публикации читает точный образ даже без повторной инициализации маршрута", async () => {
    vi.stubGlobal("window", { sessionStorage: browserStorage() });
    Reflect.deleteProperty(route.params, "environmentRef");
    const state = await editor(true);
    state.input.imageArtifactRef = "image_own";
    state.imageArtifact.value = promotedArtifact("image_own");
    state.selectedImage.value = { ref: "image_own", title: "Свой образ" };
    const tools = [
      { name: "Git", command: "git", description: "", usageHint: "" },
    ];
    state.input.tools = tools;
    const draft = {
      ...savedDraft(),
      environmentRef: undefined,
      state: "VALID" as const,
      validationDigest: "c".repeat(64),
      specification: { ...state.specification.value },
    } as RuntimeEnvironmentDraft;
    state.applyServerDraft(draft);
    expect(state.imageInventoryReady.value).toBe(true);
    expect(state.draftDirty.value).toBe(false);
    draftApi.prepareEnvironmentPublication.mockResolvedValueOnce({
      ref: "plan_synthetic",
      version: 1,
      kind: "RUNTIME_ENVIRONMENT",
      sourceVersion: 0,
      draftRef: draft.ref,
      draftVersion: draft.version,
      targetDigest: "d".repeat(64),
      digest: "e".repeat(64),
      total: 0,
      state: "PREPARED",
      createdAt: new Date().toISOString(),
      expiresAt: new Date(Date.now() + 60_000).toISOString(),
    });
    await state.preparePublication();
    expect(state.problem.value).toBeUndefined();
    expect(state.publicationPlan.value).toBeDefined();
    const saved: RuntimeEnvironmentSet = {
      scopeKind: "PROJECT",
      organizationRef: "org_synthetic",
      ref: "environment_published",
      version: 1,
      projectRef: "project_1",
      name: state.input.name,
      description: state.input.description,
      state: "ACTIVE",
      updatedAt: new Date().toISOString(),
      ready: true,
      readinessBlockers: [],
      nextActions: ["UPDATE"],
      currentVersion: {
        ref: "environment_version_published",
        version: 1,
        revision: 1,
        digest: "f".repeat(64),
        createdAt: new Date().toISOString(),
        image: {
          artifactRef: "image_own",
          recipeRef: "recipe_own",
          recipeGeneration: 3,
          reference: `registry.example.test/own@sha256:${"a".repeat(64)}`,
          digest: "a".repeat(64),
        },
        tools,
        values: [],
        secretDescriptors: [],
        policy: {
          resources: state.input.policy.resources,
          volumes: [],
          network: {
            denyByDefault: true,
            egress: [
              { destination: "DNS", protocol: "TCP", port: 53 },
              { destination: "DNS", protocol: "UDP", port: 53 },
              { destination: "PROVIDER_PROXY", protocol: "TCP", port: 8084 },
              { destination: "RUNTIME_CALLBACK", protocol: "TCP", port: 8444 },
            ],
            webAccess: state.input.policy.webAccess,
          },
          kubernetesAccess: { kind: "NONE", namespace: "kodex-runtime" },
          resourcesDigest: "1".repeat(64),
          volumesDigest: "2".repeat(64),
          networkDigest: "3".repeat(64),
          rbacDigest: "4".repeat(64),
        },
      },
    };
    Object.assign(runtime.environments, { environment_published: saved });
    draftApi.publishEnvironmentDraft.mockResolvedValueOnce({
      draft: {
        ...draft,
        state: "PUBLISHED",
        publishedEnvironmentRef: saved.ref,
      },
    });
    router.replace.mockImplementationOnce(() => {
      route.params.environmentRef = saved.ref;
      return Promise.resolve();
    });
    runtime.loadPromotedRoleImageArtifact.mockResolvedValueOnce({
      artifact: promotedArtifact("image_own"),
      recipeName: "Свой образ",
    });
    await state.publish([]);
    expect(draftApi.publishEnvironmentDraft).toHaveBeenCalledOnce();
    expect(state.problem.value).toBeUndefined();
    expect(runtime.loadPromotedRoleImageArtifact).toHaveBeenCalledOnce();
    expect(
      runtime.loadPromotedRoleImageArtifact.mock.calls[0]?.slice(0, 3),
    ).toEqual(["project_1", "recipe_own", "image_own"]);
    expect(state.imageInventoryReady.value).toBe(true);
    expect(state.selectedImage.value.title).toBe("Свой образ");
    expect(state.input.tools).toEqual(tools);
  });
  it("пустое состояние образа не объявляет загрузкой", async () => {
    const state = await editor(true);
    state.input.imageArtifactRef = "image_own";
    expect(state.imageLoading.value).toBe(false);
    expect(state.imageBadgeLabel.value).toBe(
      i18n.global.t("runtime.imageInventoryUnavailable"),
    );
  });
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
