import {
  createRenderer,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type {
  Agent,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";
import { i18n } from "@/app/i18n";

const state = vi.hoisted(() => ({
  platform: {} as Record<string, unknown>,
  store: {} as Record<string, unknown>,
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => state.platform,
}));
vi.mock("./store", () => ({ useRoleImagesStore: () => state.store }));
vi.mock("vue-router", () => ({
  useRoute: () => ({ query: {}, hash: "" }),
  useRouter: () => ({ push: vi.fn() }),
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import Component from "./RoleImageEditor.vue";

const disposers: Array<() => void> = [];
beforeEach(() => {
  i18n.global.locale.value = "ru";
});
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
});

function mount(organization = false) {
  const helper: Agent = {
    ref: "agt_helper",
    projectRef: "prj_synthetic",
    roleDefinitionRef: "role_helper",
    roleDefinitionName: "Помощник проекта",
    name: "Помощник",
    version: 1,
    purpose: "Помогать с проектом",
    roleDescription: "Помощник проекта",
    state: "READY",
    enabled: true,
    system: false,
    runtimeRef: "runtime_synthetic",
    runtimeName: "Базовая среда",
    runtimeReady: true,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    updatedAt: "2026-10-06T00:00:00Z",
    nextActions: [],
  };
  const recipe: RoleImageRecipe = {
    ref: "imgrec_synthetic",
    scopeKind: organization ? "ORGANIZATION" : "PROJECT",
    organizationRef: "org_synthetic",
    projectRef: organization ? "" : helper.projectRef,
    roleDefinitionRef: "role_helper",
    version: 1,
    generation: 1,
    name: "Образ помощника",
    environment: { environmentKey: "standard" },
    sourceAvailable: false,
    state: "ACTIVE",
    promotedImageReady: false,
    createdAt: "2026-10-06T00:00:00Z",
    updatedAt: "2026-10-06T00:00:00Z",
    nextActions: [],
  };
  const platform = reactive({
    bootstrap: { organizationRef: recipe.organizationRef },
    agents: {} as Record<string, Agent>,
    roleEnvironments: {},
    roleImageRealtimeRevision: 0,
  });
  const store = reactive({
    recipes: { [recipe.ref]: recipe },
    builds: {},
    artifacts: {},
    admissionFailures: {},
    promotionReceipts: {},
    revisions: {},
    revisionNextPageToken: {},
    dependencies: {},
    createAllowed: {},
    environments: [],
    environmentByKey: new Map(),
    roleDefinitionByRef: new Map<string, { label: string }>(),
    loadingDetail: false,
    mutating: false,
    applySupportingCatalogSnapshot: vi.fn((agents: Agent[]) => {
      store.roleDefinitionByRef = new Map(
        agents.flatMap((agent) =>
          agent.roleDefinitionRef
            ? [
                [
                  agent.roleDefinitionRef,
                  {
                    label:
                      agent.roleDefinitionName ||
                      agent.roleDescription ||
                      agent.name,
                  },
                ] as const,
              ]
            : [],
        ),
      );
    }),
    loadSupportingCatalogs: vi.fn(
      (_scope: unknown, snapshot?: { agents: Agent[] }) => {
        if (snapshot) store.applySupportingCatalogSnapshot(snapshot.agents);
        return Promise.resolve();
      },
    ),
    loadDetail: vi.fn().mockResolvedValue(undefined),
    dispose: vi.fn(),
  });
  state.platform = platform;
  state.store = store;
  let setupState!: { roleLabel: Ref<string> };
  const renderer = createRenderer<object, object>({
    insert() {},
    remove() {},
    patchProp() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
  const props = reactive(
    organization
      ? {
          organizationScope: {
            kind: "ORGANIZATION",
            organizationRef: recipe.organizationRef,
          },
          recipeRef: recipe.ref,
        }
      : { projectRef: helper.projectRef, recipeRef: recipe.ref },
  );
  const app = renderer
    .createApp(
      defineComponent({
        setup(_, context) {
          setupState = (
            Component as unknown as {
              setup: (
                props: object,
                context: SetupContext,
              ) => typeof setupState;
            }
          ).setup(props, context);
          return () => null;
        },
      }),
    )
    .use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  disposers.push(() => app.unmount());
  return { platform, store, helper, setupState };
}

it("поздний авторитетный snapshot показывает имя роли помощника без нового HTTP чтения", async () => {
  const { platform, store, helper, setupState } = mount();
  await nextTick();
  expect(setupState.roleLabel.value).toBe("Название роли недоступно");
  platform.agents[helper.ref] = helper;
  await nextTick();
  expect(setupState.roleLabel.value).toBe(helper.roleDefinitionName);
  expect(store.applySupportingCatalogSnapshot).toHaveBeenLastCalledWith(
    [helper],
    [],
  );
  expect(store.loadSupportingCatalogs).toHaveBeenCalledTimes(1);
  expect(store.loadDetail).toHaveBeenCalledTimes(1);
  const current = platform.agents[helper.ref];
  if (!current) throw new Error("Synthetic helper is missing");
  current.roleDefinitionName = "Обновлённое название";
  await nextTick();
  expect(setupState.roleLabel.value).toBe("Обновлённое название");
});

it("не показывает имя роли из чужого проекта и очищает исчезнувший snapshot", async () => {
  const { platform, store, helper, setupState } = mount();
  await nextTick();
  platform.agents.agt_foreign = {
    ...helper,
    ref: "agt_foreign",
    projectRef: "prj_other",
    roleDefinitionName: "Чужая роль",
  };
  await nextTick();
  expect(setupState.roleLabel.value).toBe("Название роли недоступно");
  platform.agents[helper.ref] = helper;
  await nextTick();
  expect(setupState.roleLabel.value).toBe(helper.roleDefinitionName);
  Reflect.deleteProperty(platform.agents, helper.ref);
  await nextTick();
  expect(setupState.roleLabel.value).toBe("Название роли недоступно");
  expect(store.applySupportingCatalogSnapshot).toHaveBeenLastCalledWith([], []);
});

it("проектные агенты не заменяют каталог системного образа", async () => {
  const { platform, store, helper, setupState } = mount(true);
  await nextTick();
  platform.agents[helper.ref] = helper;
  await nextTick();
  expect(setupState.roleLabel.value).toBe(
    i18n.global.t("assistant.settings.systemScope"),
  );
  expect(store.applySupportingCatalogSnapshot).not.toHaveBeenCalled();
});
