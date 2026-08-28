<script setup lang="ts">
import { PackageOpen } from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";

import {
  canConfigureCredential,
  definitionRequiresCredential,
  executeConnectionSetup,
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
const dialogMode = ref<"CREATE" | "CREDENTIAL">("CREATE");
const busy = ref(false);
const problem = ref<AppProblem>();
const credentialStepFailed = ref(false);
const credentialRequired = ref(false);
const credentialValue = ref("");
const pendingCredential = ref<PendingCredentialSetup>();
const commandRef = ref("");
const grantConnectionRef = ref("");
const targetsLoading = ref(false);

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
  problem.value = undefined;
  dialog.value = true;
}

function closeConnectionDialog(force = false): void {
  if (busy.value && !force) return;
  dialog.value = false;
  dialogMode.value = "CREATE";
  credentialValue.value = "";
  pendingCredential.value = undefined;
  credentialStepFailed.value = false;
  credentialRequired.value = false;
  problem.value = undefined;
  form.definitionKey = "";
  form.name = "";
  form.configuration = {};
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
    credentialRequired.value = false;
    dialog.value = true;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    commandRef.value = "";
  }
}

function configurationValue(field: IntegrationConfigurationField): unknown {
  const raw = form.configuration[field.key]?.trim() ?? "";
  if (field.valueType !== "STRING_LIST") return raw;
  return raw
    .split(",")
    .map((item) => item.trim())
    .filter((item, index, values) => item && values.indexOf(item) === index);
}

async function submit(): Promise<void> {
  const definition = selectedDefinition.value;
  if (
    !definition?.available ||
    (dialogMode.value === "CREATE" && !canCreateConnection.value)
  )
    return;
  if (requiresCredential.value && !credentialValue.value.trim()) {
    credentialRequired.value = true;
    return;
  }
  busy.value = true;
  problem.value = undefined;
  credentialStepFailed.value = false;
  credentialRequired.value = false;
  try {
    const publicConfiguration: Record<string, unknown> = {};
    if (dialogMode.value === "CREATE") {
      for (const field of definition.configurationFields) {
        const value = configurationValue(field);
        if (
          (typeof value === "string" && value !== "") ||
          (Array.isArray(value) && value.length > 0)
        ) {
          publicConfiguration[field.key] = value;
        }
      }
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
        credentialValue: credentialValue.value,
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
      credentialStepFailed.value = true;
      problem.value = asProblem(outcome.error);
      return;
    }
    activeSection.value = "CONNECTIONS";
    closeConnectionDialog(true);
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
  problem.value = undefined;
  try {
    await platform.changeConnection(connection, action);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    commandRef.value = "";
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

      <ProblemNotice v-if="problem && !dialog" :problem="problem" compact />

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
          :busy-ref="commandRef"
          @command="command"
          @credential="openCredential"
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
        $t(
          dialogMode === 'CREATE'
            ? 'integrations.connectNamed'
            : 'integrations.configureCredentialNamed',
          { name: form.name },
        )
      "
      :busy="busy"
      @close="closeConnectionDialog"
    >
      <form id="integration-form" class="form-grid" @submit.prevent="submit">
        <label v-if="dialogMode === 'CREATE'" class="field field--wide">
          <span>{{ $t("common.name") }}</span>
          <input v-model.trim="form.name" required maxlength="160" autofocus />
        </label>
        <label
          v-for="field in dialogMode === 'CREATE'
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
            autocomplete="off"
          />
          <small>{{ field.help }}</small>
        </label>
        <section
          v-if="dialogMode === 'CREDENTIAL'"
          class="field field--wide card credential-summary"
        >
          <strong>{{ form.name }}</strong>
          <p>{{ $t("integrations.metadataAlreadyCreated") }}</p>
        </section>
        <label
          v-if="requiresCredential"
          class="field field--wide card credential-boundary"
        >
          <strong>{{ $t("integrations.credentials") }}</strong>
          <span>{{ $t("integrations.credentialValue") }}</span>
          <input
            v-model="credentialValue"
            type="password"
            required
            maxlength="16384"
            autocomplete="off"
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
            pendingCredential
              ? $t("integrations.retryCredential")
              : $t("integrations.connect")
          }}
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
.credential-boundary {
  display: grid;
  gap: 6px;
  margin: 0;
  border-radius: 8px;
  background: var(--panel);
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
</style>
