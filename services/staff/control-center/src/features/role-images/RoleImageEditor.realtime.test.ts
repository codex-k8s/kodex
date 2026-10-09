import {
  createRenderer,
  createSSRApp,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToString } from "@vue/server-renderer";
import type {
  RoleImageArtifact,
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";
import { unavailableInventoryFixture } from "@/test-utils/image-inventory-fixture";
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
vi.mock("@/shared/ui/confirmation", () => ({
  requestConfirmation: vi.fn().mockResolvedValue(true),
}));
import Component from "./RoleImageEditor.vue";

const disposers: Array<() => void> = [];
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
});
async function flush() {
  await nextTick();
  await Promise.resolve();
  await nextTick();
}
async function mount(
  scopeKind: "ORGANIZATION" | "PROJECT",
  supporting?: Promise<void>,
) {
  const source = "FROM ubuntu@sha256:" + "a".repeat(64) + "\n";
  const recipe: RoleImageRecipe = {
    ref: "image_synthetic",
    scopeKind,
    organizationRef: "org_synthetic",
    projectRef: scopeKind === "PROJECT" ? "prj_synthetic" : "",
    roleDefinitionRef: "role_synthetic",
    version: 1,
    generation: 1,
    name: "Образ помощника",
    environment: { environmentKey: "standard", dockerfile: source },
    sourceAvailable: true,
    state: "ACTIVE",
    promotedImageReady: false,
    createdAt: "2026-10-10T00:00:00Z",
    updatedAt: "2026-10-10T00:00:00Z",
    nextActions: ["UPDATE", "REQUEST_BUILD"],
  };
  const platform = reactive({
    bootstrap: { organizationRef: recipe.organizationRef },
    agents: {},
    roleEnvironments: {},
    roleImageRealtimeRevision: 0,
  });
  const store = reactive({
    recipes: { [recipe.ref]: recipe },
    builds: {} as Record<string, RoleImageBuild[]>,
    artifacts: {} as Record<string, RoleImageArtifact>,
    admissionFailures: {},
    promotionReceipts: {},
    revisions: {},
    revisionNextPageToken: {},
    dependencies: {},
    createAllowed: {},
    environments: [{ key: "standard", available: true }],
    environmentByKey: new Map(),
    roleDefinitionByRef: new Map(),
    loadingDetail: false,
    mutating: false,
    applySupportingCatalogSnapshot: vi.fn(),
    loadSupportingCatalogs: vi.fn(() => supporting ?? Promise.resolve()),
    loadDetail: vi.fn().mockResolvedValue(undefined),
    update: vi.fn().mockResolvedValue(recipe),
    command: vi.fn().mockResolvedValue(undefined),
    promote: vi.fn().mockResolvedValue(undefined),
    dispose: vi.fn(),
  });
  state.platform = platform;
  state.store = store;
  const props = reactive({
    projectRef: recipe.projectRef,
    organizationScope:
      scopeKind === "ORGANIZATION"
        ? {
            kind: "ORGANIZATION" as const,
            organizationRef: recipe.organizationRef,
          }
        : undefined,
    recipeRef: recipe.ref,
  });
  let setup!: {
    dockerfile: Ref<string>;
    name: Ref<string>;
    hasLocalChanges: Ref<boolean>;
    canSave: Ref<boolean>;
    loadingForm: Ref<boolean>;
    load: () => Promise<void>;
    save: () => Promise<void>;
    requestBuild: () => Promise<void>;
    cancelCurrentBuild: () => Promise<void>;
    promote: () => Promise<void>;
  };
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
  const app = renderer
    .createApp(
      defineComponent({
        setup(_, context) {
          setup = (
            Component as unknown as {
              setup: (props: object, context: SetupContext) => typeof setup;
            }
          ).setup(props, context);
          return () => null;
        },
      }),
    )
    .use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  const dispose = () => app.unmount();
  disposers.push(dispose);
  await flush();
  return { platform, store, props, setup, recipe, source, dispose };
}

describe.each(["ORGANIZATION", "PROJECT"] as const)(
  "черновик при realtime (%s)",
  (scopeKind) => {
    it("сохраняет локальный FROM и исходную OCC версию после обновления статуса", async () => {
      const { platform, store, setup, recipe, source } = await mount(scopeKind);
      const draft = source.replace(/a{64}/, "b".repeat(64));
      setup.dockerfile.value = draft;
      store.loadDetail.mockImplementationOnce(() => {
        store.recipes[recipe.ref] = { ...recipe, version: 2 };
        return Promise.resolve();
      });
      platform.roleImageRealtimeRevision++;
      await flush();
      expect(setup.dockerfile.value).toBe(draft);
      expect(setup.hasLocalChanges.value).toBe(true);
      await setup.save();
      expect(store.update).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ version: 1 }),
        expect.objectContaining({
          environment: { environmentKey: "standard", dockerfile: draft },
        }),
      );
    });
    it("сохраняет ввод, начатый во время ожидания detail", async () => {
      const { platform, store, setup, source } = await mount(scopeKind);
      let finish!: () => void;
      store.loadDetail.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            finish = resolve;
          }),
      );
      platform.roleImageRealtimeRevision++;
      await nextTick();
      setup.dockerfile.value = source + "RUN echo draft\n";
      finish();
      await flush();
      expect(setup.dockerfile.value).toBe(source + "RUN echo draft\n");
    });
    it("чужая source revision не становится новой базой сохранения", async () => {
      const { platform, store, setup, recipe, source } = await mount(scopeKind);
      store.loadDetail.mockImplementationOnce(() => {
        store.recipes[recipe.ref] = {
          ...recipe,
          version: 2,
          environment: {
            ...recipe.environment,
            dockerfile: source + "RUN echo remote\n",
          },
        };
        return Promise.resolve();
      });
      platform.roleImageRealtimeRevision++;
      await flush();
      setup.dockerfile.value = source + "RUN echo local\n";
      await setup.save();
      expect(store.update).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ version: 1 }),
        expect.anything(),
      );
    });
    it.each(["source", "actions"] as const)(
      "очищает прежний source и запрещает save при утрате %s",
      async (change) => {
        const { store, setup, recipe, source } = await mount(scopeKind);
        setup.dockerfile.value = source + "RUN echo draft\n";
        const current = store.recipes[recipe.ref];
        if (!current) throw new Error("Synthetic recipe is missing");
        if (change === "source") current.sourceAvailable = false;
        else current.nextActions = ["OPEN"];
        expect(setup.dockerfile.value).toBe(change === "source" ? "" : source);
        expect(setup.canSave.value).toBe(false);
        await setup.save();
        expect(store.update).not.toHaveBeenCalled();
      },
    );
    it("явный reload принимает новую source и версию", async () => {
      const { store, setup, recipe, source } = await mount(scopeKind);
      setup.dockerfile.value = source + "RUN echo draft\n";
      store.recipes[recipe.ref] = {
        ...recipe,
        version: 2,
        environment: {
          ...recipe.environment,
          dockerfile: source + "RUN echo remote\n",
        },
      };
      await setup.load();
      expect(setup.dockerfile.value).toBe(source + "RUN echo remote\n");
      await setup.save();
      expect(store.update).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ version: 2 }),
        expect.anything(),
      );
    });
    it("собственное сохранение принимает receipt source и его OCC версию", async () => {
      const { store, setup, recipe, source } = await mount(scopeKind);
      const saved = {
        ...recipe,
        version: 2,
        generation: 2,
        environment: {
          ...recipe.environment,
          dockerfile: source + "RUN echo saved\n",
        },
      };
      setup.dockerfile.value = saved.environment.dockerfile;
      store.update.mockImplementationOnce(() => {
        store.recipes[recipe.ref] = saved;
        return Promise.resolve(saved);
      });
      await setup.save();
      expect(setup.dockerfile.value).toBe(saved.environment.dockerfile);
      expect(setup.hasLocalChanges.value).toBe(false);
      setup.dockerfile.value += "RUN echo next\n";
      await setup.save();
      expect(store.update).toHaveBeenLastCalledWith(
        expect.anything(),
        expect.objectContaining({ version: 2 }),
        expect.anything(),
      );
    });
    it("поздний save receipt не теряет следующий ввод", async () => {
      const { store, setup, recipe, source } = await mount(scopeKind);
      const saved = {
        ...recipe,
        version: 2,
        environment: {
          ...recipe.environment,
          dockerfile: source + "RUN echo saved\n",
        },
      };
      let finish!: (value: RoleImageRecipe) => void;
      setup.dockerfile.value = saved.environment.dockerfile;
      store.update.mockImplementationOnce(
        () =>
          new Promise<RoleImageRecipe>((resolve) => {
            finish = resolve;
          }),
      );
      const pending = setup.save();
      setup.dockerfile.value += "RUN echo next\n";
      store.recipes[recipe.ref] = saved;
      finish(saved);
      await pending;
      expect(setup.dockerfile.value).toBe(
        saved.environment.dockerfile + "RUN echo next\n",
      );
      await setup.save();
      expect(store.update).toHaveBeenLastCalledWith(
        expect.anything(),
        expect.objectContaining({ version: 2 }),
        expect.anything(),
      );
    });
    it("ошибка OCC сохраняет draft и исходную версию без автоматического повтора", async () => {
      const { store, setup, source } = await mount(scopeKind);
      setup.dockerfile.value = source + "RUN echo draft\n";
      store.update.mockRejectedValueOnce(new Error("version mismatch"));
      await setup.save();
      expect(setup.dockerfile.value).toBe(source + "RUN echo draft\n");
      expect(store.update).toHaveBeenCalledTimes(1);
      await setup.save();
      expect(store.update).toHaveBeenLastCalledWith(
        expect.anything(),
        expect.objectContaining({ version: 1 }),
        expect.anything(),
      );
    });
    it("смена scope немедленно очищает draft и baseline до detail нового scope", async () => {
      const { store, setup, props, source } = await mount(scopeKind);
      setup.dockerfile.value = source + "RUN echo draft\n";
      store.loadDetail.mockImplementationOnce(
        () => new Promise<void>(() => {}),
      );
      if (scopeKind === "PROJECT") props.projectRef = "prj_other";
      else
        props.organizationScope = {
          kind: "ORGANIZATION",
          organizationRef: "org_other",
        };
      expect(setup.dockerfile.value).toBe("");
      expect(setup.canSave.value).toBe(false);
      await setup.save();
      expect(store.update).not.toHaveBeenCalled();
    });
    it.each(["route", "dispose", "revoke"] as const)(
      "поздний realtime не восстанавливает draft после %s",
      async (change) => {
        const { platform, store, setup, props, recipe, source, dispose } =
          await mount(scopeKind);
        let finish!: () => void;
        store.loadDetail.mockImplementationOnce(
          () =>
            new Promise<void>((resolve) => {
              finish = resolve;
            }),
        );
        platform.roleImageRealtimeRevision++;
        await nextTick();
        setup.dockerfile.value = source + "RUN echo draft\n";
        if (change === "route") props.recipeRef = "image_other";
        else if (change === "dispose") {
          dispose();
          disposers.pop();
        } else {
          const current = store.recipes[recipe.ref];
          if (!current) throw new Error("Synthetic recipe is missing");
          current.sourceAvailable = false;
        }
        finish();
        await flush();
        expect(setup.dockerfile.value).toBe("");
        await setup.save();
        expect(store.update).not.toHaveBeenCalled();
      },
    );
    it.each(["build", "cancel", "promote"] as const)(
      "%s ACK не заменяет ввод во время await и исходную версию",
      async (action) => {
        const { store, setup, recipe, source } = await mount(scopeKind);
        recipe.nextActions = [
          "UPDATE",
          "REQUEST_BUILD",
          "CANCEL_BUILD",
          "PROMOTE",
        ];
        if (action === "cancel") {
          store.builds[recipe.ref] = [
            {
              ref: "build_synthetic",
              version: 1,
              scopeKind,
              organizationRef: recipe.organizationRef,
              projectRef: recipe.projectRef,
              recipeRef: recipe.ref,
              recipeGeneration: 1,
              sourceAvailable: true,
              attempt: 1,
              stage: "SOLVING",
              progressPercent: 50,
              createdAt: recipe.createdAt,
              updatedAt: recipe.updatedAt,
            },
          ];
        }
        if (action === "promote") {
          store.artifacts[recipe.ref] = {
            ref: "artifact_synthetic",
            version: 1,
            scopeKind,
            organizationRef: recipe.organizationRef,
            projectRef: recipe.projectRef,
            recipeRef: recipe.ref,
            recipeGeneration: 1,
            buildRef: "build_synthetic",
            manifestDigest: "a".repeat(64),
            provenanceSha256: "b".repeat(64),
            admissionVerdict: "ACCEPTED",
            promotionState: "PENDING",
            promotionRequested: false,
            declaredTools: [],
            verifiedToolInventory: unavailableInventoryFixture(),
          };
        }
        let finish!: () => void;
        const effect = action === "promote" ? store.promote : store.command;
        effect.mockImplementationOnce(
          () =>
            new Promise<void>((resolve) => {
              finish = resolve;
            }),
        );
        const pending =
          action === "build"
            ? setup.requestBuild()
            : action === "cancel"
              ? setup.cancelCurrentBuild()
              : setup.promote();
        await nextTick();
        expect(effect).toHaveBeenCalledTimes(1);
        setup.dockerfile.value = source + "RUN echo draft\n";
        setup.name.value = "Локальное имя";
        store.recipes[recipe.ref] = {
          ...recipe,
          version: 2,
          environment: {
            ...recipe.environment,
            dockerfile: source + "RUN echo remote\n",
          },
        };
        finish();
        await pending;
        expect(setup.dockerfile.value).toBe(source + "RUN echo draft\n");
        expect(setup.name.value).toBe("Локальное имя");
        await setup.save();
        expect(store.update).toHaveBeenLastCalledWith(
          expect.anything(),
          expect.objectContaining({ version: 1 }),
          expect.anything(),
        );
      },
    );
    it("initial load не открывает controls до завершения supporting await", async () => {
      let finish!: () => void;
      const supporting = new Promise<void>((resolve) => {
        finish = resolve;
      });
      const { props, setup, source } = await mount(scopeKind, supporting);
      expect(setup.loadingForm.value).toBe(true);
      expect(setup.canSave.value).toBe(false);
      const html = await renderToString(
        createSSRApp({ ...Component, setup: () => setup }, props)
          .use(i18n)
          .component("RouterLink", defineComponent({ render: () => null })),
      );
      expect(html).toContain("editor-loading");
      expect(html).not.toContain("save-boundary");
      finish();
      await flush();
      expect(setup.loadingForm.value).toBe(false);
      expect(setup.dockerfile.value).toBe(source);
    });
    it("explicit reload закрывает controls на весь await и принимает source явно", async () => {
      const { props, store, setup, recipe, source } = await mount(scopeKind);
      setup.dockerfile.value = source + "RUN echo draft\n";
      let finish!: () => void;
      store.loadSupportingCatalogs.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            finish = resolve;
          }),
      );
      const pending = setup.load();
      expect(setup.loadingForm.value).toBe(true);
      const html = await renderToString(
        createSSRApp({ ...Component, setup: () => setup }, props)
          .use(i18n)
          .component("RouterLink", defineComponent({ render: () => null })),
      );
      expect(html).toContain("editor-loading");
      expect(html).not.toContain("save-boundary");
      store.recipes[recipe.ref] = {
        ...recipe,
        version: 2,
        environment: {
          ...recipe.environment,
          dockerfile: source + "RUN echo remote\n",
        },
      };
      finish();
      await pending;
      expect(setup.loadingForm.value).toBe(false);
      expect(setup.dockerfile.value).toBe(source + "RUN echo remote\n");
      await setup.save();
      expect(store.update).toHaveBeenLastCalledWith(
        expect.anything(),
        expect.objectContaining({ version: 2 }),
        expect.anything(),
      );
    });
  },
);
