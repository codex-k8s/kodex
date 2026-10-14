import { readFile } from "node:fs/promises";

import { createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createRenderer,
  defineComponent,
  nextTick,
  ssrContextKey,
  watch,
  type App,
  type SetupContext,
} from "vue";

import { usePlatformStore } from "@/features/platform/store";
import IntegrationsPage from "@/pages/IntegrationsPage.vue";
import EmailMailboxConfigurationPanel from "@/features/integrations/ui/EmailMailboxConfigurationPanel.vue";
import * as mailboxApi from "@/features/integrations/email-mailbox-api";
import {
  confirmationState,
  resolveConfirmation,
} from "@/shared/ui/confirmation";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import { captureSetupState } from "@/test-utils/setup-harness";

const definition: IntegrationDefinition = {
  key: "synthetic",
  name: "Synthetic HTTP",
  description: "Локальная проверка lifecycle",
  category: "testing",
  builtIn: true,
  version: 1,
  nextActions: [],
  connectionCount: 0,
  healthyConnectionCount: 0,
  available: true,
  capabilities: [],
  configurationFields: [
    {
      key: "journal",
      label: "Журнал",
      help: "Exact journal",
      valueType: "TEXT",
      required: true,
    },
  ],
  schemaVersion: "integrations.kodex.io/v1",
  definitionVersion: "3.0.0",
  origin: "SHIPPED",
  digest: "a".repeat(64),
  adapter: "SYNTHETIC_HTTP",
  adapterOwner: "integration-gateway",
  executionRoute: "MANAGED_MCP",
  adapterReadiness: "READY",
};

function connection(
  ref: string,
  nextActions: IntegrationConnection["nextActions"],
): IntegrationConnection {
  return {
    ref,
    version: 3,
    definitionKey: definition.key,
    name: ref,
    state: "CONNECTED",
    credentialsConfigured: true,
    credentialsHint: "ab***yz",
    capabilities: [],
    grants: [],
    nextActions,
    definitionVersion: definition.definitionVersion,
    definitionDigest: definition.digest,
    publicConfiguration: {
      journal: "ui-lifecycle",
      credential: "must-never-leak",
    },
  };
}

interface IntegrationsSetup {
  command: (
    connection: IntegrationConnection,
    action: "TEST" | "ENABLE" | "DISABLE",
  ) => Promise<void>;
  confirmDelete: () => Promise<void>;
  credentialValue: { value: string };
  deleteCandidate: { value?: IntegrationConnection };
  dialog: { value: boolean };
  dialogMode: { value: string };
  openDelete: (connection: IntegrationConnection) => Promise<void>;
  openEdit: (connection: IntegrationConnection) => Promise<void>;
  operationSuccess: { value: string };
  refreshTestFeedback: (connection?: IntegrationConnection) => void;
  submit: () => Promise<void>;
  detailsConnection: { value?: IntegrationConnection };
  openConnectionDetails: (connection: IntegrationConnection) => void;
}

const unmounts: Array<() => void> = [];
afterEach(() => {
  for (const unmount of unmounts.splice(0)) unmount();
});
function mountedPage(configure: (app: App) => void): IntegrationsSetup {
  let state: IntegrationsSetup | undefined;
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
      setup(_props, context) {
        state = (
          IntegrationsPage as unknown as {
            setup(props: object, context: SetupContext): IntegrationsSetup;
          }
        ).setup({}, context);
        return () => null;
      },
    }),
  );
  configure(app);
  app.provide(ssrContextKey, {});
  app.mount({});
  unmounts.push(() => app.unmount());
  if (!state) throw new Error("Integrations page setup is missing");
  return state;
}

describe("IntegrationsPage lifecycle", () => {
  beforeEach(() => {
    resolveConfirmation(false);
    vi.clearAllMocks();
  });

  it("сначала завершает query открытия, затем монтирует busy-панель без leave confirmation", async () => {
    const pinia = createPinia();
    const platform = usePlatformStore(pinia);
    const selected = {
      ...connection("connection_email", ["UPDATE"]),
      definitionKey: "email",
    };
    vi.spyOn(platform, "loadIntegrations").mockResolvedValue();
    vi.spyOn(platform, "loadProjects").mockResolvedValue();
    const read = vi
      .spyOn(platform, "readConnection")
      .mockResolvedValue(selected);
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/integrations", component: IntegrationsPage },
        {
          path: "/elsewhere",
          component: defineComponent({ render: () => null }),
        },
      ],
    });
    await router.push("/integrations");
    const i18n = createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      messages: { ru: {} },
    });
    const configure = (app: import("vue").App) => {
      app.use(pinia);
      app.use(i18n);
      app.use(router);
    };
    const setup = mountedPage(configure);
    let panel:
      | {
          editor: {
            busy: boolean;
            dirty: boolean;
            name: string;
            catalog(): Promise<void>;
          };
        }
      | undefined;
    const stop = watch(
      () => setup.detailsConnection.value,
      (current) => {
        if (!current) return;
        const component = defineComponent({
          setup(_props, context) {
            const state = (
              EmailMailboxConfigurationPanel as unknown as {
                setup(
                  props: object,
                  context: import("vue").SetupContext,
                ): typeof panel;
              }
            ).setup({ connection: current, realtimeRevision: 0 }, context);
            if (!state) throw new Error("Mailbox panel setup is missing");
            state.editor.busy = true;
            panel = state;
            return state;
          },
        });
        void captureSetupState(component, configure);
      },
    );
    setup.openConnectionDetails(selected);
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query.connectionRef).toBe(selected.ref),
    );
    expect(confirmationState.open).toBe(false);
    await vi.waitFor(() => expect(panel?.editor.busy).toBe(true));
    expect(read).toHaveBeenCalledOnce();
    expect(setup.detailsConnection.value?.ref).toBe(selected.ref);

    // Начальная загрузка по-прежнему защищает настоящий уход из редактора.
    const leave = router.push("/elsewhere");
    await vi.waitFor(() => expect(confirmationState.open).toBe(true));
    resolveConfirmation(false);
    await leave;
    expect(router.currentRoute.value.path).toBe("/integrations");
    expect(setup.detailsConnection.value?.ref).toBe(selected.ref);
    if (!panel) throw new Error("Mailbox panel setup is missing");
    panel.editor.busy = false;
    const listMailboxes = vi
      .spyOn(mailboxApi, "listMailboxes")
      .mockResolvedValue({
        items: [],
        nextPageToken: "",
        total: 0,
        nextActions: [
          { action: "CREATE_DRAFT", enabled: true, reason: "NONE" },
        ],
      });
    await panel.editor.catalog();
    listMailboxes.mockRestore();
    panel.editor.name = "Несохранённая конфигурация";
    expect(panel.editor.dirty).toBe(true);
    const changeConnection = router.replace({
      query: { connectionRef: "connection_other" },
    });
    await vi.waitFor(() => expect(confirmationState.open).toBe(true));
    resolveConfirmation(false);
    await changeConnection;
    expect(router.currentRoute.value.query.connectionRef).toBe(selected.ref);
    expect(panel.editor.name).toBe("Несохранённая конфигурация");
    expect(read).toHaveBeenCalledOnce();
    stop();
  });

  it("отмена dirty-перехода к другому подключению не заменяет открытый редактор", async () => {
    const pinia = createPinia();
    const platform = usePlatformStore(pinia);
    const current = connection("connection_current", ["UPDATE"]);
    const other = connection("connection_other", ["UPDATE"]);
    vi.spyOn(platform, "loadIntegrations").mockResolvedValue();
    vi.spyOn(platform, "loadProjects").mockResolvedValue();
    const read = vi
      .spyOn(platform, "readConnection")
      .mockResolvedValue(current);
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/integrations", component: IntegrationsPage }],
    });
    await router.push(`/integrations?connectionRef=${current.ref}`);
    const setup = (await captureSetupState(IntegrationsPage, (app) => {
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
    })) as unknown as IntegrationsSetup;
    await vi.waitFor(() =>
      expect(setup.detailsConnection.value?.ref).toBe(current.ref),
    );
    const guard = vi.fn(() => false);
    const remove = router.beforeEach(guard);
    setup.openConnectionDetails(other);
    await vi.waitFor(() => expect(guard).toHaveBeenCalledOnce());
    await nextTick();
    expect(router.currentRoute.value.query.connectionRef).toBe(current.ref);
    expect(setup.detailsConnection.value?.ref).toBe(current.ref);
    expect(read).toHaveBeenCalledOnce();
    remove();
  });

  it("обновляет только публичную конфигурацию и удаляет лишь после подтверждения", async () => {
    const pinia = createPinia();
    const platform = usePlatformStore(pinia);
    const allowed = connection("connection_allowed", ["UPDATE", "DELETE"]);
    const forbidden = connection("connection_forbidden", []);
    platform.definitions[definition.key] = definition;
    platform.connections[allowed.ref] = allowed;
    platform.connections[forbidden.ref] = forbidden;
    vi.spyOn(platform, "loadIntegrations").mockResolvedValue();
    vi.spyOn(platform, "loadProjects").mockResolvedValue();
    const read = vi
      .spyOn(platform, "readConnection")
      .mockImplementation((ref) => {
        const current = platform.connections[ref];
        return current
          ? Promise.resolve(current)
          : Promise.reject(new Error(`Unknown connection ${ref}`));
      });
    const update = vi
      .spyOn(platform, "updateConnection")
      .mockResolvedValue({ ...allowed, name: "Изменено", version: 4 });
    const remove = vi
      .spyOn(platform, "deleteConnection")
      .mockResolvedValue({ ...allowed, state: "DELETED", version: 4 });
    const i18n = createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      messages: { ru: {} },
    });
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/integrations", component: IntegrationsPage }],
    });
    await router.push("/integrations");
    await router.isReady();
    const setup = (await captureSetupState(IntegrationsPage, (app) => {
      app.use(pinia);
      app.use(i18n);
      app.use(router);
    })) as unknown as IntegrationsSetup;

    await setup.openEdit(forbidden);
    await setup.openDelete(forbidden);
    expect(read).not.toHaveBeenCalled();
    expect(setup.dialog.value).toBe(false);
    expect(setup.deleteCandidate.value).toBeUndefined();

    await setup.openEdit(allowed);
    expect(read).toHaveBeenCalledWith(allowed.ref);
    expect(setup.dialog.value).toBe(true);
    expect(setup.dialogMode.value).toBe("EDIT");
    expect(setup.credentialValue.value).toBe("");
    await setup.submit();
    expect(update).toHaveBeenCalledWith(allowed, {
      name: allowed.name,
      publicConfiguration: { journal: "ui-lifecycle" },
    });
    expect(JSON.stringify(update.mock.calls[0]?.[1])).not.toContain(
      "must-never-leak",
    );

    await setup.openDelete(allowed);
    expect(setup.deleteCandidate.value).toEqual(allowed);
    expect(remove).not.toHaveBeenCalled();
    await setup.confirmDelete();
    expect(remove).toHaveBeenCalledOnce();
    expect(remove).toHaveBeenCalledWith(allowed);
    expect(setup.deleteCandidate.value).toBeUndefined();
  });

  it("связывает delete lifecycle с ModalDialog без window.confirm", async () => {
    const source = await readFile(
      new URL("./IntegrationsPage.vue", import.meta.url),
      "utf8",
    );

    expect(source).toContain('v-if="deleteCandidate"');
    expect(source).toContain("<ModalDialog");
    expect(source).toContain('@click="confirmDelete"');
    expect(source).not.toContain("window.confirm");
  });

  it("открывает прямую ссылку на подключение любого типа", async () => {
    const pinia = createPinia();
    const platform = usePlatformStore(pinia);
    const selected = connection("connection_source", ["UPDATE"]);
    vi.spyOn(platform, "loadIntegrations").mockResolvedValue();
    vi.spyOn(platform, "loadProjects").mockResolvedValue();
    const read = vi
      .spyOn(platform, "readConnection")
      .mockResolvedValue(selected);
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/integrations", component: IntegrationsPage }],
    });
    await router.push(`/integrations?connectionRef=${selected.ref}`);
    await router.isReady();
    const setup = (await captureSetupState(IntegrationsPage, (app) => {
      app.use(pinia);
      app.use(
        createI18n({ legacy: false, locale: "ru", messages: { ru: {} } }),
      );
      app.use(router);
    })) as unknown as IntegrationsSetup;
    await nextTick();
    expect(read).toHaveBeenCalledWith(selected.ref);
    expect(setup.detailsConnection.value?.ref).toBe(selected.ref);
  });

  it("заменяет сообщение о начатой проверке после авторитетного readback", async () => {
    const pinia = createPinia();
    const platform = usePlatformStore(pinia);
    const selected = connection("connection_test", ["TEST"]);
    const testing = { ...selected, state: "TESTING" as const, version: 4 };
    platform.connections[selected.ref] = selected;
    vi.spyOn(platform, "loadIntegrations").mockResolvedValue();
    vi.spyOn(platform, "changeConnection").mockImplementation(() => {
      platform.connections[selected.ref] = testing;
      return Promise.resolve(testing);
    });
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/integrations", component: IntegrationsPage }],
    });
    await router.push("/integrations");
    await router.isReady();
    const setup = (await captureSetupState(IntegrationsPage, (app) => {
      app.use(pinia);
      app.use(
        createI18n({
          legacy: false,
          locale: "ru",
          messages: {
            ru: {
              integrationsRedesign: {
                testStarted: "Проверка выполняется",
                testFinished: "Проверка завершена: {outcome}",
              },
            },
          },
        }),
      );
      app.use(router);
    })) as unknown as IntegrationsSetup;

    setup.detailsConnection.value = selected;
    await setup.command(selected, "TEST");
    expect(setup.operationSuccess.value).toBe("Проверка выполняется");
    expect(setup.detailsConnection.value.state).toBe("TESTING");
    platform.connections[selected.ref] = {
      ...testing,
      state: "CONNECTED",
      lastTestOutcome: "CONNECTED",
    };
    setup.refreshTestFeedback(platform.connections[selected.ref]);
    expect(setup.operationSuccess.value).toBe("Проверка выполняется");
    platform.connections[selected.ref] = {
      ...testing,
      state: "CONNECTED",
      version: 5,
      lastTestOutcome: "CONNECTED",
    };
    setup.refreshTestFeedback(platform.connections[selected.ref]);
    expect(setup.operationSuccess.value).toContain("Проверка завершена");
    expect(setup.detailsConnection.value.state).toBe("CONNECTED");
    expect(setup.detailsConnection.value.version).toBe(5);
  });
});
