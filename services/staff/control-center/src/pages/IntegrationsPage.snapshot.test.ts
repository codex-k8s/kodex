import { createPinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  createRenderer,
  defineComponent,
  nextTick,
  ssrContextKey,
  type ComputedRef,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import IntegrationsPage from "./IntegrationsPage.vue";
import type { IntegrationConnection } from "@/shared/api/generated/openapi/types.gen";
import { selectProjectRef } from "@/shared/project-context";

const connection: IntegrationConnection = {
  ref: "int_fixture_connected",
  version: 6,
  name: "Подключение",
  definitionKey: "context7",
  definitionVersion: "1.0.0",
  definitionDigest: "a".repeat(64),
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "configured",
  capabilities: [],
  grants: [],
  nextActions: [],
  publicConfiguration: {},
};

type Platform = ReturnType<typeof usePlatformStore>;
interface State {
  connections: ComputedRef<IntegrationConnection[]>;
  connectionCursor: Ref<string>;
  integrationsLoaded: Ref<boolean>;
  applyConnectionSnapshot(): boolean;
}
const disposers: (() => void)[] = [];
beforeEach(() => selectProjectRef(undefined));
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
  selectProjectRef(undefined);
});

function snapshot(
  platform: Platform,
  items: IntegrationConnection[],
  scope?: string,
  nextPageToken = "",
) {
  platform.applyPlatformSnapshot("INTEGRATION_CONNECTION", scope, {
    definitions: {
      definitions: [],
      coreReady: true,
      nextActions: [],
      page: {},
    },
    connections: { connections: items, page: { nextPageToken } },
  });
}

async function mount(seed?: (platform: Platform) => void) {
  const pinia = createPinia();
  const platform = usePlatformStore(pinia);
  seed?.(platform);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/integrations", component: IntegrationsPage }],
  });
  await router.push("/integrations");
  await router.isReady();
  let state!: State;
  const source = IntegrationsPage as unknown as {
    setup(props: Record<string, never>, context: SetupContext): State;
  };
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
        state = source.setup({}, context);
        return () => null;
      },
    }),
  );
  app.use(pinia);
  app.use(router);
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      messages: { ru: {} },
    }),
  );
  app.provide(ssrContextKey, {});
  app.mount({});
  disposers.push(() => app.unmount());
  await nextTick();
  return { platform, state };
}

describe("Список организационных подключений из realtime", () => {
  it("показывает уже полученный глобальный снимок при открытии страницы", async () => {
    const { state } = await mount((platform) =>
      snapshot(platform, [connection]),
    );
    expect(state.connections.value).toEqual([connection]);
    expect(state.integrationsLoaded.value).toBe(true);
  });

  it("использует текущую точную subscription scope, не только all-projects marker", async () => {
    selectProjectRef("prj_current_fixture");
    const { state } = await mount((platform) =>
      snapshot(platform, [connection], "prj_current_fixture"),
    );
    expect(state.connections.value).toEqual([connection]);
  });

  it("получает поздний снимок, даже когда store уже содержал тот же ref/version", async () => {
    const { platform, state } = await mount((value) => {
      value.connections[connection.ref] = connection;
    });
    expect(state.connections.value).toEqual([]);
    snapshot(platform, [connection]);
    await nextTick();
    expect(state.connections.value).toEqual([connection]);
  });

  it("различает отсутствующий и авторитетный пустой снимок и обновляет cursor при rejoin", async () => {
    const { platform, state } = await mount();
    expect(state.integrationsLoaded.value).toBe(false);
    snapshot(platform, []);
    await nextTick();
    expect(state.integrationsLoaded.value).toBe(true);
    snapshot(platform, [connection], undefined, "cursor_first");
    await nextTick();
    expect(state.connectionCursor.value).toBe("cursor_first");
    snapshot(platform, [connection], undefined, "cursor_rejoined");
    await nextTick();
    expect(state.connectionCursor.value).toBe("cursor_rejoined");
  });

  it("удаляет показанные строки после authoritative availability revoke без polling", async () => {
    const { platform, state } = await mount((value) =>
      snapshot(value, [connection]),
    );
    platform.applyRealtimeAvailability([], undefined);
    await nextTick();
    expect(state.connections.value).toEqual([]);
    expect(state.connectionCursor.value).toBe("");
    expect(state.integrationsLoaded.value).toBe(false);
  });

  it("не принимает снимок чужого project scope", async () => {
    const { platform, state } = await mount((value) =>
      snapshot(value, [connection]),
    );
    expect(() =>
      snapshot(
        platform,
        [{ ...connection, ref: "int_foreign_fixture" }],
        "prj_foreign_fixture",
      ),
    ).toThrow();
    await nextTick();
    expect(state.connections.value).toEqual([connection]);
  });

  it("не переносит подключения прежнего owner lifetime после reset и нового snapshot", async () => {
    const { platform, state } = await mount((value) =>
      snapshot(value, [connection]),
    );
    platform.clearOwnerState();
    await nextTick();
    expect(state.connections.value).toEqual([]);
    expect(state.integrationsLoaded.value).toBe(false);
    const next = { ...connection, ref: "int_next_owner_fixture" };
    snapshot(platform, [next]);
    await nextTick();
    expect(state.connections.value).toEqual([next]);
    expect(
      state.connections.value.some((item) => item.ref === connection.ref),
    ).toBe(false);
  });
});
