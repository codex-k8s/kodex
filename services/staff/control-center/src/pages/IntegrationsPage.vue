<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";

import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import type {
  IntegrationConnection,
  IntegrationConfigurationField,
} from "@/shared/api/generated/openapi/types.gen";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const definitions = computed(() => Object.values(platform.definitions));
const connections = computed(() => Object.values(platform.connections));
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
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
  if (!definition?.available) return;
  form.definitionKey = definition.key;
  form.name = definition.name;
  form.configuration = Object.fromEntries(
    definition.configurationFields.map((field) => [field.key, ""]),
  );
  problem.value = undefined;
  dialog.value = true;
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
  if (!definition) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const publicConfiguration: Record<string, unknown> = {};
    for (const field of definition.configurationFields) {
      const value = configurationValue(field);
      if (
        (typeof value === "string" && value !== "") ||
        (Array.isArray(value) && value.length > 0)
      ) {
        publicConfiguration[field.key] = value;
      }
    }
    await platform.connectIntegration({
      definitionKey: form.definitionKey,
      name: form.name,
      publicConfiguration,
    });
    dialog.value = false;
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
  if (!connection || !grant.targetRef || !grant.capabilityKey) return;
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
        $t('integrations.connectNamed', { name: selectedDefinition.name })
      "
      :busy="busy"
      @close="dialog = false"
      ><form id="integration-form" class="form-grid" @submit.prevent="submit">
        <label class="field field--wide"
          ><span>{{ $t("common.name") }}</span
          ><input v-model.trim="form.name" required maxlength="160" autofocus
        /></label>
        <label
          v-for="field in selectedDefinition.configurationFields"
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
        <section class="field field--wide card credential-boundary">
          <strong>{{ $t("integrations.credentials") }}</strong>
          <p>{{ $t("integrations.credentialSetup") }}</p>
          <small>{{ $t("integrations.masked") }}</small>
        </section>
        <ProblemNotice
          v-if="problem"
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
          @click="dialog = false"
        >
          {{ $t("common.cancel") }}</button
        ><button
          class="button button--primary"
          form="integration-form"
          type="submit"
          :disabled="busy"
        >
          {{ $t("integrations.connect") }}
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
