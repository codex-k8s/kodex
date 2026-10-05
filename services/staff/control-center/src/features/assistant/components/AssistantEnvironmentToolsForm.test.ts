import {
  createRenderer,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { describe, expect, it, vi } from "vitest";
import type { EditablePlanOperation } from "@/features/assistant/model";
import type {
  RuntimeImageCatalog,
  RuntimeImageOption,
} from "@/features/runtime/image-tools-selection";
import type { RoleImageArtifact } from "@/shared/api/generated/openapi/types.gen";
import { verifiedImageTools } from "@/shared/lib/verified-image-tools";
import { verifiedInventoryFixture } from "@/test-utils/image-inventory-fixture";

vi.mock("@/features/runtime/store", () => ({ useRuntimeStore: () => ({}) }));
import Component from "./AssistantEnvironmentToolsForm.vue";

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
function mount(imageCatalog?: RuntimeImageCatalog) {
  const props = reactive({
    operation: {
      parametersText: JSON.stringify({
        imageArtifactRef: artifact.ref,
        tools: [],
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
