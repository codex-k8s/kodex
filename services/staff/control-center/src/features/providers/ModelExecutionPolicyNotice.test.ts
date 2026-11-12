import {
  reactive,
  nextTick,
  defineComponent,
  ssrContextKey,
  type SetupContext,
  type Ref,
} from "vue";
import { describe, expect, it, vi } from "vitest";
import { createNoopRenderer } from "@/test-utils/create-noop-renderer";
import type { ProviderDefinition } from "./model";

const api = vi.hoisted(() => ({ loadProviderDefinitions: vi.fn() }));
vi.mock("./api", () => api);
vi.mock("vue-i18n", () => ({ useI18n: () => ({ t: (key: string) => key }) }));
import Notice from "./ModelExecutionPolicyNotice.vue";

describe("политика исполнения модели", () => {
  it("при одинаковом alias очищает гарантии прежнего маршрута и отбрасывает его поздний ответ", async () => {
    const responses: ((page: { items: ProviderDefinition[] }) => void)[] = [];
    api.loadProviderDefinitions.mockImplementation(
      () => new Promise((resolve) => responses.push(resolve)),
    );
    const props = reactive({ definitionKey: "opencode-one", model: "shared-model" });
    type State = {
      definition: Ref<ProviderDefinition | undefined>;
      unavailable: Ref<boolean>;
      loading: Ref<boolean>;
    };
    let state!: State;
    const source = Notice as unknown as {
      setup(props: object, context: SetupContext): State;
    };
    const app = createNoopRenderer().createApp(
      defineComponent({
        setup(_props, context) {
          state = source.setup(props, context);
          return () => null;
        },
      }),
    );
    app.provide(ssrContextKey, {});
    app.mount({});
    try {
      expect(state.loading.value).toBe(true);
      props.definitionKey = "opencode-two";
      const first = responses[0];
      if (!first) throw new Error("Initial provider request is missing");
      first({ items: [{ key: "opencode-one" } as ProviderDefinition] });
      await nextTick();
      expect(state.definition.value).toBeUndefined();
      const last = responses.at(-1);
      if (!last) throw new Error("Current provider request is missing");
      last({ items: [{ key: "foreign" } as ProviderDefinition] });
      await nextTick();
      expect(state.definition.value).toBeUndefined();
      expect(state.unavailable.value).toBe(true);
      props.model = "";
      expect(state.loading.value).toBe(false);
    } finally {
      app.unmount();
    }
  });
});
