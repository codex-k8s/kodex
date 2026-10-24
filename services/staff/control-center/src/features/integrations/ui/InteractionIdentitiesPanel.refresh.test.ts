import {
  createRenderer,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { expect, it, vi } from "vitest";
import type { IntegrationConnection } from "@/shared/api/generated/openapi/types.gen";

const read = vi.hoisted(() =>
  vi.fn(() => Promise.resolve({ items: [], nextPageToken: "" })),
);
vi.mock("../interaction-identities", () => ({
  readInteractionIdentities: read,
  validIdentityInput: () => false,
  createInteractionIdentity: vi.fn(),
  removeInteractionIdentity: vi.fn(),
}));
import Panel from "./InteractionIdentitiesPanel.vue";

it("обновление того же подключения сохраняет форму, смена версии закрывает её", async () => {
  const connection: IntegrationConnection = {
    ref: "connection_identity",
    version: 3,
    definitionKey: "mattermost",
    definitionVersion: "1",
    definitionDigest: "a".repeat(64),
    name: "Подключение",
    state: "CONNECTED",
    credentialsConfigured: true,
    credentialsHint: "",
    capabilities: [],
    grants: [],
    nextActions: [],
    publicConfiguration: {},
  };
  const props = reactive({ connection });
  type Props = typeof props;
  let state!: {
    openCreate(): void;
    createOpen: Ref<boolean>;
    input: { subjectRef: string };
  };
  const setup = (
    Panel as unknown as {
      setup: (props: Props, context: SetupContext) => typeof state;
    }
  ).setup;
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
  const app = renderer.createApp(
    defineComponent({
      setup(_props, context) {
        state = setup(props, context);
        return () => null;
      },
    }),
  );
  app.use(createI18n({ legacy: false, locale: "ru", messages: { ru: {} } }));
  app.provide(ssrContextKey, {});
  app.mount({});
  try {
    await nextTick();
    state.openCreate();
    state.input.subjectRef = "user_selected";
    props.connection = { ...connection };
    await nextTick();
    expect(state.createOpen.value).toBe(true);
    expect(state.input.subjectRef).toBe("user_selected");
    expect(read).toHaveBeenCalledTimes(1);
    props.connection = { ...connection, version: 4 };
    await nextTick();
    expect(state.createOpen.value).toBe(false);
    expect(read).toHaveBeenCalledTimes(2);
  } finally {
    app.unmount();
  }
});
