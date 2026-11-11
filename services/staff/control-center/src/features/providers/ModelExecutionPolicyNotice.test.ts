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
  it("очищает прежние гарантии и отбрасывает поздний ответ после смены маршрута", async () => {
    const responses: ((page: { items: ProviderDefinition[] }) => void)[] = [];
    api.loadProviderDefinitions.mockImplementation(
      () => new Promise((resolve) => responses.push(resolve)),
    );
    const props = reactive({ definitionKey: "opencode-one", model: "one" });
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
      props.model = "two";
      responses[0]!({ items: [{ key: "opencode-one" } as ProviderDefinition] });
      await nextTick();
      expect(state.definition.value).toBeUndefined();
      responses.at(-1)!({ items: [{ key: "foreign" } as ProviderDefinition] });
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
