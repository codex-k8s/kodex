<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";

import {
  canConfigureCredential,
  definitionRequiresCredential,
  executeConnectionSetup,
  type PendingCredentialSetup,
} from "@/features/integrations/connection-setup";
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
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const definitions = computed(() => Object.values(platform.definitions));
const connections = computed(() => Object.values(platform.connections));
const canCreateConnection = computed(() =>
  platform.integrationDefinitionActions.includes("CREATE_CONNECTION"),
);
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
const selectedTargets = computed(() =>
  grant.targetKind === "AGENT" ? projectAgents.value : projectWorkflows.value,
);

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

function openGrants(connection: IntegrationConnection): void {
  if (!connection.nextActions.includes("MANAGE_GRANTS")) return;
  grantConnectionRef.value = connection.ref;
  grant.capabilityKey = connection.capabilities[0]?.key ?? "";
  grant.projectRef = platform.projectList[0]?.ref ?? "";
  grant.targetKind = "AGENT";
  grant.targetRef = "";
  if (grant.projectRef) void loadGrantTargets();
}

async function loadGrantTargets(): Promise<void> {
  grant.targetRef = "";
  if (!grant.projectRef) return;
  targetsLoading.value = true;
  problem.value = undefined;
  try {
    await platform.loadAgents(grant.projectRef);
    await platform.loadWorkflows(grant.projectRef);
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

async function revokeGrant(
  connection: IntegrationConnection,
  capabilityKey: string,
  agentRef?: string,
  workflowRef?: string,
): Promise<void> {
  if (!connection.nextActions.includes("MANAGE_GRANTS")) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.changeConnectionGrant(connection, {
      capabilityKey,
      ...(agentRef ? { agentRef } : { workflowRef }),
      enabled: false,
    });
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

function capabilityName(
  connection: IntegrationConnection,
  key: string,
): string {
  return connection.capabilities.find((item) => item.key === key)?.name ?? key;
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
    <ProblemNotice v-if="problem && !dialog" :problem="problem" compact />
    <AsyncState
      :loading="platform.loading.integrations"
      :problem="platform.problems.integrations"
      @retry="platform.loadIntegrations()"
    >
      <section aria-labelledby="connections-heading">
        <div class="section-header">
          <div>
            <h2 id="connections-heading">
              {{ $t("integrations.connections") }}
            </h2>
            <p class="muted">{{ $t("integrations.webOnlyReady") }}</p>
          </div>
        </div>
        <div v-if="connections.length" class="connection-list">
          <article
            v-for="connection in connections"
            :key="connection.ref"
            class="card connection-card"
          >
            <header class="card-heading">
              <div>
                <h3>{{ connection.name }}</h3>
                <p>
                  {{
                    platform.definitions[connection.definitionKey]?.name ??
                    connection.definitionKey
                  }}
                </p>
              </div>
              <StatusBadge :state="connection.state" />
            </header>
            <p>{{ connection.credentialsHint }}</p>
            <div class="credential-state">
              <StatusBadge
                :state="
                  connection.credentialsConfigured ? 'READY' : 'NEEDS_ATTENTION'
                "
                :label="
                  connection.credentialsConfigured
                    ? $t('integrations.credentialsConfigured')
                    : $t('integrations.credentialsNotConfigured')
                "
              />
            </div>
            <p v-if="connection.lastTestOutcome" class="test-outcome">
              <strong>{{ $t("integrations.lastTest") }}:</strong>
              {{ connection.lastTestOutcome }}
            </p>
            <div
              class="capability-list"
              :aria-label="$t('integrations.capabilities')"
            >
              <span
                v-for="capability in connection.capabilities"
                :key="capability.key"
                class="capability-chip"
                >{{ capability.name }} ·
                {{ $t(`integrations.risk.${capability.risk}`) }}</span
              >
            </div>
            <div class="entity-row__actions">
              <button
                v-if="
                  canConfigureCredential(
                    platform.definitions[connection.definitionKey],
                    connection,
                  )
                "
                class="button button--primary"
                type="button"
                :disabled="commandRef === connection.ref"
                @click="openCredential(connection)"
              >
                {{ $t("integrations.configureCredential") }}
              </button>
              <button
                v-if="connection.nextActions.includes('TEST')"
                class="button"
                type="button"
                :disabled="commandRef === connection.ref"
                @click="command(connection, 'TEST')"
              >
                {{ $t("common.test") }}</button
              ><button
                v-if="connection.nextActions.includes('MANAGE_GRANTS')"
                class="button"
                type="button"
                @click="openGrants(connection)"
              >
                {{ $t("integrations.manageGrants") }}</button
              ><button
                v-if="connection.nextActions.includes('ENABLE')"
                class="button"
                type="button"
                :disabled="commandRef === connection.ref"
                @click="command(connection, 'ENABLE')"
              >
                {{ $t("common.enable") }}</button
              ><button
                v-if="connection.nextActions.includes('DISABLE')"
                class="button button--danger"
                type="button"
                :disabled="commandRef === connection.ref"
                @click="command(connection, 'DISABLE')"
              >
                {{ $t("common.disable") }}
              </button>
            </div>
          </article>
        </div>
        <div v-else class="card empty-copy">
          <h3>{{ $t("integrations.noConnectionsTitle") }}</h3>
          <p>{{ $t("integrations.noConnections") }}</p>
        </div>
      </section>

      <section
        v-if="grantConnection"
        class="card grant-panel"
        aria-labelledby="grants-heading"
      >
        <div class="section-header">
          <div>
            <h2 id="grants-heading">{{ $t("integrations.grants") }}</h2>
            <p class="muted">
              {{ $t("integrations.grantsFor", { name: grantConnection.name }) }}
            </p>
          </div>
          <button class="button" type="button" @click="grantConnectionRef = ''">
            {{ $t("common.close") }}
          </button>
        </div>
        <form class="form-grid" @submit.prevent="saveGrant">
          <label class="field"
            ><span>{{ $t("integrations.project") }}</span
            ><select
              v-model="grant.projectRef"
              required
              @change="loadGrantTargets"
            >
              <option value="">{{ $t("integrations.chooseProject") }}</option>
              <option
                v-for="project in platform.projectList"
                :key="project.ref"
                :value="project.ref"
              >
                {{ project.name }}
              </option>
            </select></label
          >
          <label class="field"
            ><span>{{ $t("integrations.targetType") }}</span
            ><select v-model="grant.targetKind" @change="grant.targetRef = ''">
              <option value="AGENT">{{ $t("integrations.agent") }}</option>
              <option value="WORKFLOW">
                {{ $t("integrations.workflow") }}
              </option>
            </select></label
          >
          <label class="field"
            ><span>{{ $t("integrations.target") }}</span
            ><select
              v-model="grant.targetRef"
              required
              :disabled="targetsLoading || !grant.projectRef"
            >
              <option value="">
                {{
                  targetsLoading
                    ? $t("common.loading")
                    : $t("integrations.chooseTarget")
                }}
              </option>
              <option
                v-for="target in selectedTargets"
                :key="target.ref"
                :value="target.ref"
              >
                {{ target.name }}
              </option>
            </select></label
          >
          <label class="field"
            ><span>{{ $t("integrations.capability") }}</span
            ><select v-model="grant.capabilityKey" required>
              <option
                v-for="capability in grantConnection.capabilities"
                :key="capability.key"
                :value="capability.key"
              >
                {{ capability.name }} ·
                {{ $t(`integrations.risk.${capability.risk}`) }}
              </option>
            </select></label
          >
          <div class="field field--wide form-actions">
            <p class="muted">{{ $t("integrations.grantBoundary") }}</p>
            <button
              class="button button--primary"
              type="submit"
              :disabled="busy || !grant.targetRef || !grant.capabilityKey"
            >
              {{ $t("integrations.grant") }}
            </button>
          </div>
        </form>
        <div v-if="grantConnection.grants.length" class="grant-list">
          <article
            v-for="item in grantConnection.grants"
            :key="item.ref"
            class="entity-row"
          >
            <div>
              <strong>{{ item.targetName }}</strong>
              <p>{{ capabilityName(grantConnection, item.capabilityKey) }}</p>
            </div>
            <StatusBadge :state="item.enabled ? 'ENABLED' : 'REVOKED'" />
            <button
              v-if="item.enabled"
              class="button button--danger"
              type="button"
              :disabled="busy"
              @click="
                revokeGrant(
                  grantConnection,
                  item.capabilityKey,
                  item.agentRef,
                  item.workflowRef,
                )
              "
            >
              {{ $t("integrations.revoke") }}
            </button>
          </article>
        </div>
        <p v-else class="empty-copy">{{ $t("integrations.noGrants") }}</p>
      </section>

      <section aria-labelledby="catalog-heading">
        <div class="section-header">
          <div>
            <h2 id="catalog-heading">{{ $t("integrations.catalog") }}</h2>
            <p class="muted">{{ $t("integrations.catalogHelp") }}</p>
          </div>
        </div>
        <div class="card-grid">
          <article
            v-for="definition in definitions"
            :key="definition.key"
            class="card catalog-card"
          >
            <div class="card-heading">
              <h3>{{ definition.name }}</h3>
              <StatusBadge
                :state="definition.available ? 'AVAILABLE' : 'UNAVAILABLE'"
              />
            </div>
            <p>{{ definition.description }}</p>
            <ul class="capability-summary">
              <li v-for="item in definition.capabilities" :key="item.key">
                <strong>{{ item.name }}</strong> — {{ item.description }}
              </li>
            </ul>
            <p v-if="!definition.available" class="muted">
              {{ $t("integrations.unavailable") }}
            </p>
            <button
              v-if="canCreateConnection"
              class="button button--primary"
              type="button"
              :disabled="!definition.available"
              @click="openConnection(definition.key)"
            >
              {{
                definition.available
                  ? $t("integrations.connect")
                  : $t("common.unavailable")
              }}
            </button>
          </article>
        </div>
      </section>
    </AsyncState>

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
      ><form id="integration-form" class="form-grid" @submit.prevent="submit">
        <label v-if="dialogMode === 'CREATE'" class="field field--wide"
          ><span>{{ $t("common.name") }}</span
          ><input v-model.trim="form.name" required maxlength="160" autofocus
        /></label>
        <label
          v-for="field in dialogMode === 'CREATE'
            ? selectedDefinition.configurationFields
            : []"
          :key="field.key"
          class="field field--wide"
          ><span>{{ field.label }}</span
          ><input
            v-model="form.configuration[field.key]"
            :type="field.valueType === 'URL' ? 'url' : 'text'"
            :required="field.required"
            :placeholder="field.placeholder"
            :maxlength="field.valueType === 'URL' ? 2048 : 500"
            autocomplete="off"
          /><small>{{ field.help }}</small></label
        >
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
      <template #actions
        ><button
          class="button"
          type="button"
          :disabled="busy"
          @click="closeConnectionDialog()"
        >
          {{ $t("common.cancel") }}</button
        ><button
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
        </button></template
      ></ModalDialog
    >
  </PageFrame>
</template>

<style scoped>
.connection-list {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr));
}
.connection-card,
.catalog-card {
  display: grid;
  gap: 14px;
  align-content: start;
}
.card-heading,
.section-header,
.form-actions {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}
.capability-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.capability-chip {
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 4px 9px;
  font-size: 0.84rem;
}
.capability-summary {
  display: grid;
  gap: 8px;
  margin: 0;
  padding-inline-start: 20px;
}
.grant-panel {
  display: grid;
  gap: 18px;
  scroll-margin-top: 24px;
}
.grant-list {
  display: grid;
  gap: 8px;
}
.test-outcome,
.empty-copy,
.muted {
  color: var(--muted);
}
.credential-boundary {
  margin: 0;
}
.credential-state {
  display: flex;
  align-items: center;
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
@media (max-width: 700px) {
  .section-header,
  .form-actions {
    align-items: stretch;
    flex-direction: column;
  }
  .entity-row__actions {
    width: 100%;
  }
  .entity-row__actions .button {
    flex: 1;
  }
}
</style>
