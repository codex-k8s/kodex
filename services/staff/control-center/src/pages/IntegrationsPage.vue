<script setup lang="ts">
import { PackageOpen } from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";

import {
  canConfigureCredential,
  definitionRequiresCredential,
  executeConnectionSetup,
  prepareConnectionConfiguration,
  type PendingCredentialSetup,
} from "@/features/integrations/connection-setup";
import IntegrationApprovalPanel from "@/features/integrations/ui/IntegrationApprovalPanel.vue";
import IntegrationCatalogPanel from "@/features/integrations/ui/IntegrationCatalogPanel.vue";
import IntegrationConnectionsPanel from "@/features/integrations/ui/IntegrationConnectionsPanel.vue";
import IntegrationGrantsPanel from "@/features/integrations/ui/IntegrationGrantsPanel.vue";
import IntegrationSectionTabs from "@/features/integrations/ui/IntegrationSectionTabs.vue";
import {
  buildIntegrationPackages,
  filterIntegrationPackages,
  flattenIntegrationGrants,
  integrationCategories,
  type IntegrationGrantPresentation,
  type IntegrationsSection,
} from "@/features/integrations/ui/model";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import type {
  IntegrationConnection,
  IntegrationConfigurationField,
} from "@/shared/api/generated/openapi/types.gen";
import { idempotencyKey } from "@/shared/api/mutation";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const platform = usePlatformStore();
const activeSection = ref<IntegrationsSection>("CONNECTIONS");
const catalogSearch = ref("");
const catalogCategory = ref("");
const dialog = ref(false);
const dialogMode = ref<"CREATE" | "CREDENTIAL" | "EDIT">("CREATE");
const editingConnection = ref<IntegrationConnection>();
const deleteCandidate = ref<IntegrationConnection>();
const busy = ref(false);
const problem = ref<AppProblem>();
const credentialStepFailed = ref(false);
const credentialRequired = ref(false);
const credentialValue = ref("");
const pendingCredential = ref<PendingCredentialSetup>();
const commandRef = ref("");
const commandAction = ref<"TEST" | "ENABLE" | "DISABLE">();
const operationSuccess = ref("");
const grantConnectionRef = ref("");
const targetsLoading = ref(false);
const formSubmitted = ref(false);

const form = reactive({
  definitionKey: "",
  name: "",
  configuration: {} as Record<string, string>,
});
const grant = reactive({
  projectRef: "",
  targetKind: "AGENT" as "AGENT" | "WORKFLOW",
  targetRef: "",
  capabilityKey: "",
});

const definitions = computed(() => Object.values(platform.definitions));
const connections = computed(() => Object.values(platform.connections));
const canCreateConnection = computed(() =>
  platform.integrationDefinitionActions.includes("CREATE_CONNECTION"),
);
const packages = computed(() =>
  buildIntegrationPackages(
    definitions.value,
    connections.value,
    canCreateConnection.value,
  ),
);
const categories = computed(() => integrationCategories(packages.value));
const visiblePackages = computed(() =>
  filterIntegrationPackages(
    packages.value,
    catalogSearch.value,
    catalogCategory.value || undefined,
  ),
);
const allGrants = computed(() => flattenIntegrationGrants(connections.value));
const visibleGrants = computed(() =>
  grantConnectionRef.value
    ? allGrants.value.filter(
        (item) => item.connectionRef === grantConnectionRef.value,
      )
    : allGrants.value,
);
const selectedDefinition = computed(
  () => platform.definitions[form.definitionKey],
);
const requiresCredential = computed(() =>
  definitionRequiresCredential(selectedDefinition.value),
);
const showsCredentialInput = computed(
  () => dialogMode.value !== "EDIT" && requiresCredential.value,
);
const preparedConfiguration = computed(() =>
  prepareConnectionConfiguration(
    selectedDefinition.value?.configurationFields ?? [],
    form.configuration,
  ),
);
const credentialProblemKey = computed(() => {
  switch (problem.value?.kind) {
    case "unauthorized":
      return "integrations.credentialErrors.unauthorized";
    case "forbidden":
      return "integrations.credentialErrors.forbidden";
    case "not-found":
      return "integrations.credentialErrors.notFound";
    case "conflict":
      return "integrations.credentialErrors.conflict";
    case "unavailable":
      return "integrations.credentialErrors.unavailable";
    default:
      return "integrations.credentialErrors.default";
  }
});
const grantConnection = computed(() =>
  grantConnectionRef.value
    ? platform.connections[grantConnectionRef.value]
    : undefined,
);
const projectAgents = computed(() =>
  Object.values(platform.agents).filter(
    (item) => item.projectRef === grant.projectRef && !item.system,
  ),
);
const projectWorkflows = computed(() =>
  Object.values(platform.workflows).filter(
    (item) => item.projectRef === grant.projectRef,
  ),
);

function selectSection(section: IntegrationsSection): void {
  activeSection.value = section;
}

function openConnection(definitionKey: string): void {
  const definition = platform.definitions[definitionKey];
  if (!canCreateConnection.value || !definition?.available) return;
  dialogMode.value = "CREATE";
  form.definitionKey = definition.key;
  form.name = definition.name;
  form.configuration = Object.fromEntries(
    definition.configurationFields.map((field) => [field.key, ""]),
  );
  credentialValue.value = "";
  pendingCredential.value = undefined;
  credentialStepFailed.value = false;
  credentialRequired.value = false;
  formSubmitted.value = false;
  problem.value = undefined;
  operationSuccess.value = "";
  editingConnection.value = undefined;
  dialog.value = true;
}

function closeConnectionDialog(force = false): void {
  if (busy.value && !force) return;
  dialog.value = false;
  dialogMode.value = "CREATE";
  credentialValue.value = "";
  pendingCredential.value = undefined;
  editingConnection.value = undefined;
  credentialStepFailed.value = false;
  credentialRequired.value = false;
  formSubmitted.value = false;
  problem.value = undefined;
  form.definitionKey = "";
  form.name = "";
  form.configuration = {};
}

function editableConfigurationValue(
  connection: IntegrationConnection,
  field: IntegrationConfigurationField,
): string {
  const value = connection.publicConfiguration[field.key];
  if (Array.isArray(value)) return value.map(String).join(", ");
  if (typeof value === "string" || typeof value === "number")
    return String(value);
  if (typeof value === "boolean") return value ? "true" : "false";
  return "";
}

async function openEdit(connection: IntegrationConnection): Promise<void> {
  if (!connection.nextActions.includes("UPDATE")) return;
  commandRef.value = connection.ref;
  problem.value = undefined;
  operationSuccess.value = "";
  try {
    const current = await platform.readConnection(connection.ref);
    const definition = platform.definitions[current.definitionKey];
    if (!definition || !current.nextActions.includes("UPDATE")) return;
    dialogMode.value = "EDIT";
    editingConnection.value = current;
    form.definitionKey = current.definitionKey;
    form.name = current.name;
    form.configuration = Object.fromEntries(
      definition.configurationFields.map((field) => [
        field.key,
        editableConfigurationValue(current, field),
      ]),
    );
    credentialValue.value = "";
    pendingCredential.value = undefined;
    credentialStepFailed.value = false;
    credentialRequired.value = false;
    formSubmitted.value = false;
    dialog.value = true;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    commandRef.value = "";
  }
}

function credentialChanged(): void {
  credentialRequired.value = false;
  if (!credentialStepFailed.value || !pendingCredential.value) return;
  pendingCredential.value = {
    ...pendingCredential.value,
    idempotencyKey: idempotencyKey(),
  };
  credentialStepFailed.value = false;
  problem.value = undefined;
}

async function openCredential(
  connection: IntegrationConnection,
): Promise<void> {
  const definition = platform.definitions[connection.definitionKey];
  if (!canConfigureCredential(definition, connection)) return;
  commandRef.value = connection.ref;
  problem.value = undefined;
  try {
    const current = await platform.readConnection(connection.ref);
    if (!canConfigureCredential(definition, current)) return;
    dialogMode.value = "CREDENTIAL";
    form.definitionKey = current.definitionKey;
    form.name = current.name;
    form.configuration = {};
    credentialValue.value = "";
    pendingCredential.value = {
      connectionRef: current.ref,
      version: current.version,
      idempotencyKey: idempotencyKey(),
    };
    credentialStepFailed.value = false;
    editingConnection.value = undefined;
    credentialRequired.value = false;
    formSubmitted.value = false;
    dialog.value = true;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    commandRef.value = "";
  }
}

function configurationProblem(field: IntegrationConfigurationField): string {
  const code = preparedConfiguration.value.problems[field.key];
  if (code === "REQUIRED") return "Заполните обязательное поле.";
  if (code === "INVALID_HTTPS_URL")
    return "Укажите полный URL с протоколом https://.";
  return "";
}

async function submit(): Promise<void> {
  const definition = selectedDefinition.value;
  if (
    !definition ||
    (dialogMode.value === "CREATE" &&
      (!definition.available || !canCreateConnection.value)) ||
    (dialogMode.value === "EDIT" &&
      !editingConnection.value?.nextActions.includes("UPDATE"))
  )
    return;
  formSubmitted.value = true;
  if (
    dialogMode.value !== "CREDENTIAL" &&
    Object.keys(preparedConfiguration.value.problems).length
  )
    return;
  if (showsCredentialInput.value && !credentialValue.value.trim()) {
    credentialRequired.value = true;
    return;
  }
  busy.value = true;
  problem.value = undefined;
  credentialStepFailed.value = false;
  credentialRequired.value = false;
  const oneTimeCredential = credentialValue.value;
  credentialValue.value = "";
  try {
    const publicConfiguration = preparedConfiguration.value.value;
    if (dialogMode.value === "EDIT" && editingConnection.value) {
      const updated = await platform.updateConnection(editingConnection.value, {
        name: form.name,
        publicConfiguration,
      });
      activeSection.value = "CONNECTIONS";
      operationSuccess.value = `Подключение «${updated.name}» изменено.`;
      closeConnectionDialog(true);
      return;
    }
    const outcome = await executeConnectionSetup(
      {
        connection: {
          definitionKey: form.definitionKey,
          name: form.name,
          ...(Object.keys(publicConfiguration).length
            ? { publicConfiguration }
            : {}),
        },
        credentialValue: oneTimeCredential,
        requiresCredential: requiresCredential.value,
        ...(pendingCredential.value
          ? { pending: pendingCredential.value }
          : {}),
      },
      {
        create: (input) => platform.connectIntegration(input),
        configure: (target, value, requestKey) =>
          platform.configureConnectionCredential(
            { ref: target.connectionRef, version: target.version },
            value,
            requestKey,
          ),
        createIdempotencyKey: idempotencyKey,
      },
    );
    if (outcome.status === "CREDENTIAL_FAILED") {
      dialogMode.value = "CREDENTIAL";
      pendingCredential.value = outcome.pending;
      credentialValue.value = "";
      credentialStepFailed.value = true;
      problem.value = asProblem(outcome.error);
      return;
    }
    activeSection.value = "CONNECTIONS";
    operationSuccess.value = `Подключение «${outcome.connection.name}» сохранено.`;
    closeConnectionDialog(true);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    credentialValue.value = "";
    busy.value = false;
  }
}

async function openDelete(connection: IntegrationConnection): Promise<void> {
  if (!connection.nextActions.includes("DELETE")) return;
  commandRef.value = connection.ref;
  problem.value = undefined;
  operationSuccess.value = "";
  try {
    const current = await platform.readConnection(connection.ref);
    if (!current.nextActions.includes("DELETE")) return;
    deleteCandidate.value = current;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    commandRef.value = "";
  }
}

function closeDeleteDialog(force = false): void {
  if (busy.value && !force) return;
  deleteCandidate.value = undefined;
  problem.value = undefined;
}

async function confirmDelete(): Promise<void> {
  const current = deleteCandidate.value;
  if (!current?.nextActions.includes("DELETE")) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const deleted = await platform.deleteConnection(current);
    operationSuccess.value = `Подключение «${deleted.name}» удалено.`;
    closeDeleteDialog(true);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function command(
  connection: IntegrationConnection,
  action: "TEST" | "ENABLE" | "DISABLE",
): Promise<void> {
  if (!connection.nextActions.includes(action)) return;
  commandRef.value = connection.ref;
  commandAction.value = action;
  problem.value = undefined;
  operationSuccess.value = "";
  try {
    const updated = await platform.changeConnection(connection, action);
    operationSuccess.value =
      action === "TEST"
        ? `Проверка «${updated.name}» завершена: ${updated.lastTestOutcome ?? updated.state}.`
        : action === "ENABLE"
          ? `Подключение «${updated.name}» включено.`
          : `Подключение «${updated.name}» отключено.`;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    commandRef.value = "";
    commandAction.value = undefined;
  }
}

function selectGrantConnection(connectionRef: string): void {
  grantConnectionRef.value = connectionRef;
  const connection = connectionRef
    ? platform.connections[connectionRef]
    : undefined;
  grant.capabilityKey = connection?.capabilities[0]?.key ?? "";
  grant.projectRef = platform.projectList[0]?.ref ?? "";
  grant.targetKind = "AGENT";
  grant.targetRef = "";
  if (connection && grant.projectRef) void loadGrantTargets();
}

function openGrants(connection: IntegrationConnection): void {
  if (!connection.nextActions.includes("MANAGE_GRANTS")) return;
  activeSection.value = "GRANTS";
  selectGrantConnection(connection.ref);
}

async function loadGrantTargets(): Promise<void> {
  grant.targetRef = "";
  if (!grant.projectRef) return;
  targetsLoading.value = true;
  problem.value = undefined;
  try {
    await Promise.all([
      platform.loadAgents(grant.projectRef),
      platform.loadWorkflows(grant.projectRef),
    ]);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    targetsLoading.value = false;
  }
}

async function saveGrant(): Promise<void> {
  const connection = grantConnection.value;
  if (
    !connection?.nextActions.includes("MANAGE_GRANTS") ||
    !grant.targetRef ||
    !grant.capabilityKey
  )
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.changeConnectionGrant(connection, {
      capabilityKey: grant.capabilityKey,
      ...(grant.targetKind === "AGENT"
        ? { agentRef: grant.targetRef }
        : { workflowRef: grant.targetRef }),
      enabled: true,
    });
    grant.targetRef = "";
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function revokeGrant(item: IntegrationGrantPresentation): Promise<void> {
  if (!item.connection.nextActions.includes("MANAGE_GRANTS")) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.changeConnectionGrant(item.connection, {
      capabilityKey: item.capabilityKey,
      ...(item.grant.agentRef
        ? { agentRef: item.grant.agentRef }
        : { workflowRef: item.grant.workflowRef }),
      enabled: false,
    });
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

onMounted(() => {
  void platform.loadIntegrations();
  void platform.loadProjects();
});

onBeforeUnmount(() => {
  credentialValue.value = "";
  editingConnection.value = undefined;
  deleteCandidate.value = undefined;
});
</script>

<template>
  <PageFrame
    :title="$t('integrations.title')"
    :subtitle="$t('integrations.subtitle')"
  >
    <template #actions>
      <button
        v-if="activeSection === 'CONNECTIONS'"
        class="button"
        type="button"
        @click="activeSection = 'CATALOG'"
      >
        <PackageOpen :size="16" aria-hidden="true" />
        {{ $t("integrationsRedesign.tabs.CATALOG") }}
      </button>
    </template>

    <div class="integration-page">
      <IntegrationSectionTabs
        :active="activeSection"
        :connection-count="connections.length"
        :package-count="packages.length"
        :grant-count="allGrants.length"
        @select="selectSection"
      />

      <ProblemNotice
        v-if="problem && !dialog && !deleteCandidate"
        :problem="problem"
        compact
      />
      <div
        v-if="operationSuccess && !dialog"
        class="operation-success"
        role="status"
      >
        {{ operationSuccess }}
      </div>

      <AsyncState
        v-if="activeSection !== 'APPROVALS'"
        :loading="platform.loading.integrations"
        :problem="platform.problems.integrations"
        @retry="platform.loadIntegrations()"
      >
        <IntegrationConnectionsPanel
          v-if="activeSection === 'CONNECTIONS'"
          :connections="connections"
          :definitions="platform.definitions"
          :core-ready="platform.integrationCoreReady === true"
          :busy-ref="commandRef"
          :busy-action="commandAction"
          @command="command"
          @credential="openCredential"
          @edit="openEdit"
          @delete="openDelete"
          @grants="openGrants"
        />

        <IntegrationCatalogPanel
          v-else-if="activeSection === 'CATALOG'"
          :packages="visiblePackages"
          :categories="categories"
          :search="catalogSearch"
          :category="catalogCategory"
          @update:search="catalogSearch = $event"
          @update:category="catalogCategory = $event"
          @connect="openConnection"
        />

        <IntegrationGrantsPanel
          v-else
          :connections="connections"
          :grants="visibleGrants"
          :selected-connection="grantConnection"
          :projects="platform.projectList"
          :agents="projectAgents"
          :workflows="projectWorkflows"
          :project-ref="grant.projectRef"
          :target-kind="grant.targetKind"
          :target-ref="grant.targetRef"
          :capability-key="grant.capabilityKey"
          :targets-loading="targetsLoading"
          :busy="busy"
          @select-connection="selectGrantConnection"
          @update:project-ref="grant.projectRef = $event"
          @update:target-kind="grant.targetKind = $event"
          @update:target-ref="grant.targetRef = $event"
          @update:capability-key="grant.capabilityKey = $event"
          @load-targets="loadGrantTargets"
          @save="saveGrant"
          @revoke="revokeGrant"
        />
      </AsyncState>

      <IntegrationApprovalPanel v-else />
    </div>

    <ModalDialog
      v-if="dialog && selectedDefinition"
      :title="
        dialogMode === 'EDIT'
          ? `Изменить подключение «${form.name}»`
          : $t(
              dialogMode === 'CREATE'
                ? 'integrations.connectNamed'
                : 'integrations.configureCredentialNamed',
              { name: form.name },
            )
      "
      :busy="busy"
      size="lg"
      @close="closeConnectionDialog"
    >
      <form id="integration-form" class="form-grid" @submit.prevent="submit">
        <section class="field field--wide manifest-summary">
          <div>
            <strong>{{ selectedDefinition.name }}</strong>
            <span class="mono">
              {{ selectedDefinition.schemaVersion }} · v{{
                selectedDefinition.definitionVersion
              }}
            </span>
          </div>
          <p>{{ selectedDefinition.description }}</p>
          <div class="manifest-summary__facts">
            <span class="mono">{{ selectedDefinition.adapter }}</span>
            <span
              v-for="capability in selectedDefinition.capabilities"
              :key="capability.key"
            >
              {{ capability.name }} ·
              {{ $t("integrations.risk." + capability.risk) }}
              · <code>{{ capability.resourceKind }}</code>
              <strong v-if="capability.approvalRequired">Human Gate</strong>
            </span>
          </div>
        </section>
        <label v-if="dialogMode !== 'CREDENTIAL'" class="field field--wide">
          <span>{{ $t("common.name") }}</span>
          <input v-model.trim="form.name" required maxlength="160" autofocus />
        </label>
        <label
          v-for="field in dialogMode !== 'CREDENTIAL'
            ? selectedDefinition.configurationFields
            : []"
          :key="field.key"
          class="field field--wide"
        >
          <span>{{ field.label }}</span>
          <input
            v-model="form.configuration[field.key]"
            :type="field.valueType === 'URL' ? 'url' : 'text'"
            :required="field.required"
            :placeholder="field.placeholder"
            :maxlength="field.valueType === 'URL' ? 2048 : 500"
            :aria-invalid="
              formSubmitted &&
              Boolean(preparedConfiguration.problems[field.key])
            "
            autocomplete="off"
          />
          <small>
            {{ field.help }}
            <template v-if="field.valueType === 'STRING_LIST'">
              Значения разделяются запятыми.
            </template>
          </small>
          <small
            v-if="formSubmitted && configurationProblem(field)"
            class="field-error"
          >
            {{ configurationProblem(field) }}
          </small>
        </label>
        <section
          v-if="dialogMode === 'CREDENTIAL'"
          class="field field--wide card credential-summary"
        >
          <strong>{{ form.name }}</strong>
          <p>{{ $t("integrations.metadataAlreadyCreated") }}</p>
        </section>
        <label
          v-if="showsCredentialInput"
          class="field field--wide card credential-boundary"
        >
          <strong>{{ $t("integrations.credentials") }}</strong>
          <code v-if="selectedDefinition.credentialSecretKey">
            {{ selectedDefinition.credentialSecretKey }}
          </code>
          <span>{{ $t("integrations.credentialValue") }}</span>
          <input
            v-model="credentialValue"
            type="password"
            required
            maxlength="16384"
            autocomplete="new-password"
            autocapitalize="none"
            spellcheck="false"
            :aria-invalid="credentialRequired"
            aria-describedby="credential-help"
            @input="credentialChanged"
          />
          <small id="credential-help">
            {{ $t("integrations.credentialValueHelp") }}
          </small>
          <small v-if="credentialRequired" class="field-error">
            {{ $t("integrations.credentialRequired") }}
          </small>
        </label>
        <section
          v-else-if="dialogMode === 'EDIT'"
          class="field field--wide card credential-boundary"
        >
          <strong>{{ $t("integrations.credentials") }}</strong>
          <p>
            Учётные данные не изменяются вместе с публичной конфигурацией. Для
            их ротации используйте отдельное действие подключения.
          </p>
        </section>
        <section v-else class="field field--wide card credential-boundary">
          <strong>{{ $t("integrations.credentials") }}</strong>
          <p>{{ $t("integrations.credentialsNotRequired") }}</p>
        </section>
        <section
          v-if="credentialStepFailed && problem"
          class="field field--wide credential-failure"
          role="alert"
        >
          <strong>{{ $t("integrations.credentialFailedTitle") }}</strong>
          <p>{{ $t(credentialProblemKey) }}</p>
          <p>{{ $t("integrations.metadataPreserved") }}</p>
          <small v-if="problem.correlationId">{{
            problem.correlationId
          }}</small>
        </section>
        <ProblemNotice
          v-if="problem && !credentialStepFailed"
          class="field--wide"
          :problem="problem"
          compact
        />
      </form>
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="busy"
          @click="closeConnectionDialog()"
        >
          {{ $t("common.cancel") }}
        </button>
        <button
          class="button button--primary"
          form="integration-form"
          type="submit"
          :disabled="busy"
        >
          {{
            busy
              ? "Сохраняем…"
              : pendingCredential
                ? $t("integrations.retryCredential")
                : dialogMode === "EDIT"
                  ? $t("common.save")
                  : $t("integrations.connect")
          }}
        </button>
      </template>
    </ModalDialog>

    <ModalDialog
      v-if="deleteCandidate"
      title="Удалить подключение"
      :busy="busy"
      size="md"
      @close="closeDeleteDialog"
    >
      <div class="delete-confirmation">
        <p>
          Подключение <strong>«{{ deleteCandidate.name }}»</strong> будет
          отключено и переведено в терминальное состояние.
        </p>
        <p>
          Все разрешения подключения будут отозваны. Это действие не удаляет
          обязательный аудит.
        </p>
        <ProblemNotice v-if="problem" :problem="problem" compact />
      </div>
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="busy"
          @click="closeDeleteDialog()"
        >
          {{ $t("common.cancel") }}
        </button>
        <button
          class="button button--danger"
          type="button"
          :disabled="busy"
          @click="confirmDelete"
        >
          {{ busy ? "Удаляем…" : $t("common.delete") }}
        </button>
      </template>
    </ModalDialog>
  </PageFrame>
</template>

<style scoped>
.integration-page {
  min-width: 0;
}
.integration-page > :deep(.problem-notice) {
  margin-bottom: 14px;
}
.operation-success {
  margin-bottom: 14px;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--success) 32%, var(--border));
  border-radius: 8px;
  color: var(--text-secondary);
  background: var(--success-soft);
}
.credential-boundary {
  display: grid;
  gap: 6px;
  margin: 0;
  border-radius: 8px;
  background: var(--panel);
}
.manifest-summary {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.manifest-summary > div:first-child,
.manifest-summary__facts {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.manifest-summary > div:first-child {
  justify-content: space-between;
}
.manifest-summary p {
  margin: 0;
  color: var(--muted);
}
.manifest-summary__facts span {
  padding: 4px 7px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--muted);
  background: var(--surface);
  font-size: 0.76rem;
}
.manifest-summary__facts strong {
  color: var(--warning);
}
.credential-boundary p {
  margin-bottom: 0;
}
.credential-summary,
.credential-failure {
  margin: 0;
}
.credential-failure {
  display: grid;
  gap: 6px;
  padding: 12px;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  background: var(--warning-soft);
}
.credential-failure p {
  margin: 0;
}
.delete-confirmation {
  display: grid;
  gap: 10px;
}
.delete-confirmation p {
  margin: 0;
}
</style>
