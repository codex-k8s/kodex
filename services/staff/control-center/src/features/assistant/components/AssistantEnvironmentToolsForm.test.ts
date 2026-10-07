import {
  createSSRApp,
  createRenderer,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import type { EditablePlanOperation } from "@/features/assistant/model";
import type {
  RuntimeImageCatalog,
  RuntimeImageOption,
} from "@/features/runtime/image-tools-selection";
import type { RoleImageArtifact } from "@/shared/api/generated/openapi/types.gen";
import { verifiedImageTools } from "@/shared/lib/verified-image-tools";
import { verifiedInventoryFixture } from "@/test-utils/image-inventory-fixture";

vi.mock("@/features/runtime/store", () => ({ useRuntimeStore: () => ({}) }));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import Component from "./AssistantEnvironmentToolsForm.vue";

const selectedTools = [
  {
    name: "Git",
    command: "git",
    description: "Проверка дерева",
    usageHint: "status",
  },
];

const scope = {
  kind: "ORGANIZATION",
  organizationRef: "org_synthetic",
} as const;
const artifact = {
  ref: "imgart_exact",
  recipeRef: "recipe_exact",
  recipeGeneration: 7,
  manifestDigest: `sha256:${"a".repeat(64)}`,
  provenanceSha256: "b".repeat(64),
  promotedReference: `example.invalid/assistant@sha256:${"a".repeat(64)}`,
  verifiedToolInventory: verifiedInventoryFixture(undefined, undefined, [
    "git",
    "npm",
  ]),
} as RoleImageArtifact;
const npm = artifact.verifiedToolInventory.platforms[0]?.tools.find(
  (tool) => tool.name === "npm",
);
if (!npm) throw new Error("Synthetic npm observation is missing");
npm.status = "PROBE_FAILED";
npm.version = "";

function catalog() {
  const option: RuntimeImageOption = {
    ref: artifact.ref,
    title: "Собственный образ",
    recipeRef: artifact.recipeRef,
    generation: 7,
  };
  return {
    loadPage: vi.fn<RuntimeImageCatalog["loadPage"]>().mockResolvedValue({
      items: [option],
    }),
    loadArtifact: vi
      .fn<RuntimeImageCatalog["loadArtifact"]>()
      .mockResolvedValue({ artifact, recipeName: "Собственный образ" }),
  };
}
function mount(
  imageCatalog?: RuntimeImageCatalog,
  tools = selectedTools.slice(0, 0),
) {
  const props = reactive({
    operation: {
      parametersText: JSON.stringify({
        imageArtifactRef: artifact.ref,
        tools,
      }),
      value: { before: {} },
    } as EditablePlanOperation,
    projectRef: "",
    resourceScope: scope,
    selectedImage: { ref: artifact.ref, title: artifact.ref },
    disabled: false,
    imageCatalog,
  });
  const events: Array<[string, unknown]> = [];
  let state!: {
    artifact: Ref<RoleImageArtifact | undefined>;
    loadFailed: Ref<boolean>;
    loading: Ref<boolean>;
    problems: Ref<string[]>;
    visibleProblems: Ref<string[]>;
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
  const app = renderer.createApp(
    defineComponent({
      setup(_, context) {
        const setup = (
          Component as unknown as {
            setup: (props: object, context: SetupContext) => unknown;
          }
        ).setup;
        state = setup(props, {
          ...context,
          emit: (name: string, value: unknown) => events.push([name, value]),
        }) as typeof state;
        return () => null;
      },
    }),
  );
  app.provide(ssrContextKey, { modules: new Set() });
  app.mount({});
  return { props, state, events, dispose: () => app.unmount() };
}

describe("Нативный образ плана окружения", () => {
  it("держит valid=false при загрузке exact artifact, затем разрешает проверенный inventory без изменения tools", async () => {
    const reader = catalog();
    let resolveArtifact:
      | ((
          value: Awaited<ReturnType<RuntimeImageCatalog["loadArtifact"]>>,
        ) => void)
      | undefined;
    reader.loadArtifact.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveArtifact = resolve;
        }),
    );
    const before = structuredClone(selectedTools);
    const view = mount(reader, selectedTools);
    try {
      await vi.waitFor(() =>
        expect(reader.loadArtifact).toHaveBeenCalledOnce(),
      );
      expect(reader.loadArtifact).toHaveBeenCalledWith(
        scope,
        artifact.recipeRef,
        artifact.ref,
        expect.any(AbortSignal),
      );
      expect(view.state.loading.value).toBe(true);
      expect(view.state.loadFailed.value).toBe(false);
      expect(view.state.problems.value).toContain(
        "assistant.planEditor.environmentToolsUnverified",
      );
      expect(view.state.visibleProblems.value).toEqual([]);
      expect(view.events.filter(([name]) => name === "valid").at(-1)).toEqual([
        "valid",
        false,
      ]);
      resolveArtifact?.({ artifact, recipeName: "Собственный образ" });
      await vi.waitFor(() => expect(view.state.loading.value).toBe(false));
      expect(view.state.visibleProblems.value).toEqual([]);
      expect(view.state.problems.value).toEqual([]);
      expect(view.events.filter(([name]) => name === "valid").at(-1)).toEqual([
        "valid",
        true,
      ]);
      expect(
        view.events.some(([name]) => name === "parameter" || name === "dirty"),
      ).toBe(false);
      expect(selectedTools).toEqual(before);
    } finally {
      view.dispose();
    }
  });

  it("сохраняет настоящий отказ scoped inventory после завершения запроса", async () => {
    const reader = catalog();
    reader.loadArtifact.mockRejectedValue(
      new Error("Synthetic scoped image read failed"),
    );
    const view = mount(reader, selectedTools);
    try {
      await vi.waitFor(() => expect(view.state.loading.value).toBe(false));
      expect(view.state.loadFailed.value).toBe(true);
      expect(view.state.artifact.value).toBeUndefined();
      expect(view.state.visibleProblems.value).toContain(
        "assistant.planEditor.environmentToolsUnverified",
      );
      expect(view.events.filter(([name]) => name === "valid").at(-1)).toEqual([
        "valid",
        false,
      ]);
    } finally {
      view.dispose();
    }
  });

  it("не скрывает неподтверждённый инструмент даже после успешного HTTP readback", async () => {
    const view = mount(catalog(), [
      { ...selectedTools[0], name: "npm", command: "npm" },
    ] as typeof selectedTools);
    try {
      await vi.waitFor(() => expect(view.state.loading.value).toBe(false));
      expect(view.state.loadFailed.value).toBe(false);
      expect(view.state.visibleProblems.value).toContain(
        "assistant.planEditor.environmentToolsUnverified",
      );
      expect(view.events.filter(([name]) => name === "valid").at(-1)).toEqual([
        "valid",
        false,
      ]);
    } finally {
      view.dispose();
    }
  });

  it("не маскирует ошибку полей инструмента состоянием загрузки", () => {
    const reader = catalog();
    reader.loadPage.mockImplementationOnce(() => new Promise(() => {}));
    const view = mount(reader, [
      { name: "Git", command: "bad command", description: "", usageHint: "" },
    ]);
    try {
      expect(view.state.loading.value).toBe(true);
      expect(view.state.visibleProblems.value.length).toBeGreaterThan(0);
      expect(view.state.visibleProblems.value).not.toContain(
        "assistant.planEditor.environmentToolsUnverified",
      );
      expect(view.events.filter(([name]) => name === "valid").at(-1)).toEqual([
        "valid",
        false,
      ]);
    } finally {
      view.dispose();
    }
  });
  it("не показывает ложный красный alert до завершения загрузки inventory выбранного образа", async () => {
    const reader = catalog();
    let resolvePage:
      | ((value: Awaited<ReturnType<RuntimeImageCatalog["loadPage"]>>) => void)
      | undefined;
    reader.loadPage.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolvePage = resolve;
        }),
    );
    const operation = {
      parametersText: JSON.stringify({
        imageArtifactRef: artifact.ref,
        tools: selectedTools,
      }),
      value: { before: {} },
    } as EditablePlanOperation;
    const app = createSSRApp(Component, {
      operation,
      projectRef: "",
      resourceScope: scope,
      selectedImage: { ref: artifact.ref, title: artifact.ref },
      disabled: false,
      imageCatalog: reader,
    });
    app.use(i18n);
    try {
      const html = await renderToString(app);
      expect(reader.loadPage).toHaveBeenCalledOnce();
      expect(html).toContain('role="status"');
      expect(html).toContain(i18n.global.t("common.loading"));
      expect(html).not.toContain('role="alert"');
      expect(html).not.toContain(
        i18n.global.t("assistant.planEditor.environmentToolsUnverified"),
      );
      expect(html).not.toContain(
        i18n.global.t("assistant.planEditor.environmentToolsPending"),
      );
    } finally {
      resolvePage?.({ items: [] });
    }
  });
  it("новый объект той же области не запускает цикл metadata reload", async () => {
    const reader = catalog();
    const view = mount(reader);
    try {
      await vi.waitFor(() => expect(view.state.loading.value).toBe(false));
      view.props.resourceScope = { ...scope };
      await nextTick();
      expect(reader.loadArtifact).toHaveBeenCalledTimes(1);
      expect(
        view.events.filter(([name]) => name === "resolved-image"),
      ).toHaveLength(1);
    } finally {
      view.dispose();
    }
  });
  it("восстанавливает название из точного каталога без изменения tools[] и не выбирает PROBE_FAILED", async () => {
    const reader = catalog();
    const view = mount(reader);
    try {
      await vi.waitFor(() => expect(view.state.loading.value).toBe(false));
      expect(view.state.loadFailed.value).toBe(false);
      expect(view.events).toContainEqual([
        "resolved-image",
        {
          ref: artifact.ref,
          title: "Собственный образ",
          description: artifact.promotedReference,
          recipeRef: artifact.recipeRef,
          generation: 7,
        },
      ]);
      expect(
        view.events.some(([name]) => name === "parameter" || name === "dirty"),
      ).toBe(false);
      expect(verifiedImageTools(view.state.artifact.value)).toEqual([
        { name: "git", version: "1.2.3" },
      ]);
    } finally {
      view.dispose();
    }
  });
  it("повторяет закрытое initial чтение, когда scoped catalog становится доступен", async () => {
    const view = mount();
    try {
      await vi.waitFor(() => expect(view.state.loadFailed.value).toBe(true));
      view.props.imageCatalog = catalog();
      await nextTick();
      await vi.waitFor(() =>
        expect(view.state.artifact.value?.ref).toBe(artifact.ref),
      );
      expect(view.state.loadFailed.value).toBe(false);
    } finally {
      view.dispose();
    }
  });
  it("не возвращает старое название или inventory после смены ref", async () => {
    const reader = catalog();
    let resolve!: (value: {
      artifact: RoleImageArtifact;
      recipeName: string;
    }) => void;
    reader.loadArtifact.mockImplementationOnce(
      () =>
        new Promise((accept) => {
          resolve = accept;
        }),
    );
    const view = mount(reader);
    try {
      await vi.waitFor(() =>
        expect(reader.loadArtifact).toHaveBeenCalledTimes(1),
      );
      view.props.operation.parametersText = JSON.stringify({
        imageArtifactRef: "imgart_other",
        tools: [],
      });
      reader.loadPage.mockResolvedValue({ items: [] });
      await nextTick();
      resolve({ artifact, recipeName: "Старый образ" });
      await vi.waitFor(() => expect(view.state.loading.value).toBe(false));
      expect(view.state.artifact.value).toBeUndefined();
      expect(view.events.some(([name]) => name === "resolved-image")).toBe(
        false,
      );
    } finally {
      view.dispose();
    }
  });
});
