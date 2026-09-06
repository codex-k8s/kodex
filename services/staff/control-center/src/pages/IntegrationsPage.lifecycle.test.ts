import { readFile } from "node:fs/promises";

import { createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { usePlatformStore } from "@/features/platform/store";
import IntegrationsPage from "@/pages/IntegrationsPage.vue";
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
  confirmDelete: () => Promise<void>;
  credentialValue: { value: string };
  deleteCandidate: { value?: IntegrationConnection };
  dialog: { value: boolean };
  dialogMode: { value: string };
  openDelete: (connection: IntegrationConnection) => Promise<void>;
  openEdit: (connection: IntegrationConnection) => Promise<void>;
  submit: () => Promise<void>;
}

describe("IntegrationsPage lifecycle", () => {
  beforeEach(() => vi.clearAllMocks());

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
});
