import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  h,
  reactive,
  ssrContextKey,
  type App,
  type ComputedRef,
  type Ref,
  type SetupContext,
} from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import type { IntegrationConnectionBindingPlan } from "./integration-binding";
import {
  bindIntegrationConnection,
  prepareIntegrationConnectionBinding,
} from "./integration-binding";
import * as api from "./api";

vi.mock("./integration-binding", () => ({
  bindIntegrationConnection: vi.fn(),
  prepareIntegrationConnectionBinding: vi.fn(),
}));
vi.mock("./api", () => ({
  impact: vi.fn(),
  rebind: vi.fn(),
  configurationRequiresProject: () => false,
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({ managedConfigurations: {} }),
}));
vi.mock("@/shared/ui/unsaved-changes", () => ({
  useUnsavedChanges: () => undefined,
}));
vi.mock("@/shared/ui/cursor-list", async () => {
  const { ref } = await import("vue");
  return { useAdaptiveCursorPageSize: () => ref(40) };
});
vi.mock("@/shared/ui/async-entity-picker", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/ui/async-entity-picker")>()),
  useCursorInfiniteScroll: () => undefined,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));

import Editor from "./ConfigurationEditor.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";

const configuration = {
  ref: "configuration_target",
  kind: "INTEGRATION_DEFINITION",
  managedBy: "UI",
  name: "Целевая",
  version: 7,
  archived: false,
} as ManagedConfiguration;
const revision = {
  ref: "revision_target",
  state: "PUBLISHED",
  content: '{"metadata":{"key":"synthetic"}}',
  contentFormat: "JSON",
} as ManagedConfigurationRevision;
const projection = {
  configurationRef: configuration.ref,
  targetRevisionRef: revision.ref,
  digest: "a".repeat(64),
  total: 0,
  consumers: [],
};
function plan(ref: string): IntegrationConnectionBindingPlan {
  return {
    configuration,
    revision,
    connectionRef: ref,
    definitionKey: "synthetic",
    signal: new AbortController().signal,
    input: {
      impactDigest: projection.digest,
      consumers: [
        {
          kind: "INTEGRATION_CONNECTION",
          ref,
          revisionRef: "revision_source",
          version: 19,
          expectedAbsent: false,
        },
      ],
    },
  };
}
interface State {
  configuration: Ref<ManagedConfiguration>;
  revision: Ref<ManagedConfigurationRevision>;
  name: Ref<string>;
  content: Ref<string>;
  format: Ref<string>;
  newConnection: Ref<AsyncEntityOption | undefined>;
  connectionBindingPlan: Ref<IntegrationConnectionBindingPlan | undefined>;
  connectionBindingLoading: Ref<boolean>;
  connectionBindingProblem: Ref<unknown>;
  showImpact(): Promise<void>;
  closeImpact(): void;
  bindNewConnection(): Promise<void>;
}
const apps: App[] = [];
afterEach(() => apps.splice(0).forEach((app) => app.unmount()));
async function setup(): Promise<State> {
  let state: State | undefined;
  const renderer = createRenderer<object, object>({
    patchProp() {},
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
  const original = (
    Editor as unknown as {
      setup: (props: object, context: SetupContext) => State;
    }
  ).setup;
  const app = renderer.createApp(
    defineComponent({
      setup(_, context) {
        state = original({ kind: "INTEGRATION_DEFINITION" }, context);
        return () => null;
      },
    }),
  );
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      fallbackWarn: false,
    }),
  );
  app.provide(ssrContextKey, { modules: new Set<string>() });
  app.mount({});
  apps.push(app);
  if (!state) throw new Error("Missing editor fixture");
  const current = state;
  current.configuration.value = configuration;
  current.revision.value = revision;
  current.name.value = configuration.name;
  current.content.value = revision.content;
  current.format.value = revision.contentFormat;
  await current.showImpact();
  return current;
}
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.impact).mockResolvedValue(projection);
  vi.mocked(bindIntegrationConnection).mockResolvedValue({
    configuration,
    revision,
  } as never);
  vi.mocked(prepareIntegrationConnectionBinding).mockImplementation(
    (_config, _revision, ref) => Promise.resolve(plan(ref)),
  );
});

describe("picker точной привязки подключения", () => {
  it("сохраняет имя из native select после закрытия picker и очистки каталога", async () => {
    const editor = await setup();
    const option: AsyncEntityOption = {
      ref: "connection_selected",
      title: "GitHub подключение",
      description: "CONNECTED",
    };
    const props = reactive({
      get modelValue() {
        return editor.newConnection.value?.ref;
      },
      get selected() {
        return editor.newConnection.value;
      },
      loadPage: () => Promise.resolve({ items: [option] }),
      contextKey: "configuration_target:revision_target:synthetic",
      triggerLabel: "Подключение",
    });
    interface Entry {
      id: string;
      label: string;
      source: AsyncEntityOption;
    }
    interface PickerState {
      items: Ref<Entry[]>;
      open: Ref<boolean>;
      selectedOption: ComputedRef<AsyncEntityOption | undefined>;
      handlePopoverOpen(value: boolean): void;
      chooseDropdown(item: Entry): void;
    }
    let picker: PickerState | undefined;
    const renderer = createRenderer<object, object>({
      patchProp() {},
      insert() {},
      remove() {},
      createElement: () => ({}),
      createText: () => ({}),
      createComment: () => ({}),
      setText() {},
      setElementText() {},
      parentNode: () => null,
      nextSibling: () => null,
    });
    const original = (
      AsyncEntityPicker as unknown as {
        setup: (props: object, context: SetupContext) => PickerState;
      }
    ).setup;
    const app = renderer.createApp(
      defineComponent({
        setup(_, context) {
          picker = original(props, {
            ...context,
            emit(event: string, value: unknown) {
              if (event === "select")
                editor.newConnection.value = value as AsyncEntityOption;
              if (event === "update:modelValue" && !value)
                editor.newConnection.value = undefined;
            },
          });
          return () => null;
        },
      }),
    );
    app.provide(ssrContextKey, { modules: new Set<string>() });
    app.mount({});
    apps.push(app);
    if (!picker) throw new Error("Missing picker fixture");
    const current = picker;
    current.handlePopoverOpen(true);
    await vi.waitFor(() => expect(current.items.value).toHaveLength(1));
    const entry = current.items.value[0];
    if (!entry) throw new Error("Missing picker candidate");
    current.chooseDropdown(entry);
    expect(current.open.value).toBe(false);
    expect(editor.newConnection.value).toEqual(option);
    // Имя выбранного подключения не зависит от временного каталога popover.
    current.items.value = [];
    expect(current.selectedOption.value?.title).toBe(option.title);
    await vi.waitFor(() =>
      expect(editor.connectionBindingPlan.value?.connectionRef).toBe(
        option.ref,
      ),
    );
    const display = createSSRApp({
      render: () => h(AsyncEntityPicker, { ...props }),
    });
    display.use(
      createI18n({ legacy: false, locale: "ru", missingWarn: false }),
    );
    const html = await renderToString(display);
    expect(html).toMatch(/<strong[^>]*>GitHub подключение<\/strong>/);
    expect(html).not.toContain('class="async-picker__placeholder"');
    expect(bindIntegrationConnection).not.toHaveBeenCalled();
  });

  it("готовит authoritative план при выборе и передаёт его штатной перепривязке", async () => {
    const state = await setup();
    state.newConnection.value = {
      ref: "connection_selected",
      title: "Подключение",
    };
    await vi.waitFor(() =>
      expect(state.connectionBindingLoading.value).toBe(false),
    );
    expect(state.connectionBindingPlan.value?.input.consumers[0]).toMatchObject(
      { expectedAbsent: false, revisionRef: "revision_source", version: 19 },
    );
    await state.bindNewConnection();
    expect(bindIntegrationConnection).toHaveBeenCalledOnce();
    expect(api.rebind).not.toHaveBeenCalled();
    expect(state.newConnection.value).toBeUndefined();
  });

  it("поздний результат прежнего выбора не подменяет новое подключение", async () => {
    const state = await setup();
    let resolve:
      | ((value: IntegrationConnectionBindingPlan) => void)
      | undefined;
    vi.mocked(prepareIntegrationConnectionBinding).mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    state.newConnection.value = { ref: "connection_old", title: "Прежнее" };
    state.newConnection.value = { ref: "connection_new", title: "Новое" };
    await vi.waitFor(() =>
      expect(state.connectionBindingPlan.value?.connectionRef).toBe(
        "connection_new",
      ),
    );
    resolve?.(plan("connection_old"));
    await Promise.resolve();
    expect(state.connectionBindingPlan.value?.connectionRef).toBe(
      "connection_new",
    );
  });

  it("закрытие окна не принимает поздний план и не отправляет binding", async () => {
    const state = await setup();
    let resolve:
      | ((value: IntegrationConnectionBindingPlan) => void)
      | undefined;
    vi.mocked(prepareIntegrationConnectionBinding).mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    state.newConnection.value = { ref: "connection_old", title: "Прежнее" };
    state.closeImpact();
    resolve?.(plan("connection_old"));
    await Promise.resolve();
    await state.bindNewConnection();
    expect(state.connectionBindingPlan.value).toBeUndefined();
    expect(bindIntegrationConnection).not.toHaveBeenCalled();
  });
});
