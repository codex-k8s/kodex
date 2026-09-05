<script setup lang="ts">
import { LockKeyhole, Plus, ShieldCheck, Trash2 } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  connectionAllows,
  type IntegrationGrantPresentation,
} from "@/features/integrations/ui/model";
import type {
  Agent,
  IntegrationConnection,
  Project,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import { searchProjects } from "@/features/projects/api";
import { loadCatalog } from "@/features/catalogs/api";
import { listIntegrationConnections } from "@/shared/api/generated/openapi/sdk.gen";
import { unwrap } from "@/shared/api/problem";
import { requestSignal } from "@/shared/api/client";

const props = defineProps<{
  connections: readonly IntegrationConnection[];
  grants: readonly IntegrationGrantPresentation[];
  selectedConnection?: IntegrationConnection;
  projects: readonly Project[];
  agents: readonly Agent[];
  workflows: readonly Workflow[];
  projectRef: string;
  targetKind: "AGENT" | "WORKFLOW";
  targetRef: string;
  capabilityKey: string;
  targetsLoading: boolean;
  busy: boolean;
}>();

const emit = defineEmits<{
  selectConnection: [connectionRef: string];
  "update:projectRef": [value: string];
  "update:targetKind": [value: "AGENT" | "WORKFLOW"];
  "update:targetRef": [value: string];
  "update:capabilityKey": [value: string];
  loadTargets: [];
  save: [];
  revoke: [grant: IntegrationGrantPresentation];
}>();

const { t } = useI18n();
const targets = computed(() =>
  props.targetKind === "AGENT" ? props.agents : props.workflows,
);
const chosenProject = ref<AsyncEntityOption>();
const chosenTarget = ref<AsyncEntityOption>();
const projectOption = computed(() =>
  chosenProject.value?.ref === props.projectRef
    ? chosenProject.value
    : props.projects.find((item) => item.ref === props.projectRef)
      ? {
          ref: props.projectRef,
          title:
            props.projects.find((item) => item.ref === props.projectRef)
              ?.name ?? "",
        }
      : undefined,
);
const targetOption = computed(() =>
  chosenTarget.value?.ref === props.targetRef
    ? chosenTarget.value
    : targets.value.find((item) => item.ref === props.targetRef)
      ? {
          ref: props.targetRef,
          title:
            targets.value.find((item) => item.ref === props.targetRef)?.name ??
            "",
        }
      : undefined,
);
const connectionOption = computed(() =>
  props.selectedConnection
    ? {
        ref: props.selectedConnection.ref,
        title: props.selectedConnection.name,
        description: props.selectedConnection.credentialsHint,
        meta: t(`states.${props.selectedConnection.state}`),
      }
    : undefined,
);
async function loadProjects(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = await searchProjects(query, cursor, signal);
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.purpose,
      meta: t(`states.${item.lifecycle}`),
    })),
    nextPageToken: page.nextPageToken,
  };
}
async function loadRecipients(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  if (!props.projectRef || !props.selectedConnection) return { items: [] };
  const project = props.projectRef;
  const page = await loadCatalog(
    props.targetKind === "AGENT" ? "agents" : "workflows",
    query,
    signal,
    cursor,
    project,
  );
  if (page.items.some((item) => item.projectRef !== project))
    throw new Error("Invalid integration recipient project");
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.title,
      description: item.description,
      meta: [t(`states.${item.state}`), ...item.meta].join(" · "),
    })),
    nextPageToken: page.nextPageToken,
  };
}
async function loadConnections(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      listIntegrationConnections({
        query: { query, pageToken: cursor, pageSize: 30 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.credentialsHint,
      meta: t(`states.${item.state}`),
    })),
    nextPageToken: page.nextPageToken,
  };
}
watch(
  () => [props.projectRef, props.targetKind, props.selectedConnection?.ref],
  () => {
    chosenTarget.value = undefined;
    emit("update:targetRef", "");
  },
);
const selectedCapability = computed(() =>
  props.selectedConnection?.capabilities.find(
    (item) => item.key === props.capabilityKey,
  ),
);
const canManageSelected = computed(
  () =>
    !!props.selectedConnection &&
    connectionAllows(props.selectedConnection, "MANAGE_GRANTS"),
);
</script>

<template>
  <section class="grants-panel grant-panel" aria-labelledby="grants-title">
    <header class="panel-heading">
      <div>
        <h2 id="grants-title">{{ t("integrationsRedesign.grantsTitle") }}</h2>
        <p>{{ t("integrationsRedesign.grantsDescription") }}</p>
      </div>
      <span class="result-count">{{
        t("integrationsRedesign.grantCount", { count: grants.length })
      }}</span>
    </header>

    <div class="grant-workspace">
      <div class="grant-list-column">
        <label class="connection-picker">
          <span>{{ t("integrationsRedesign.connectionPicker") }}</span>
          <AsyncEntityPicker
            :model-value="selectedConnection?.ref"
            :selected="connectionOption"
            :load-page="loadConnections"
            :trigger-label="t('integrationsRedesign.connectionPicker')"
            :placeholder="t('integrationsRedesign.allConnections')"
            @select="emit('selectConnection', $event.ref)"
          />
        </label>

        <div v-if="grants.length" class="grant-list" role="list">
          <article
            v-for="item in grants"
            :key="item.ref"
            class="grant-row entity-row"
            role="listitem"
          >
            <div class="grant-target">
              <span class="grant-icon" aria-hidden="true">
                <ShieldCheck :size="16" />
              </span>
              <div>
                <h3>{{ item.targetName }}</h3>
                <p>
                  {{ t(`integrationsRedesign.targetKind.${item.targetKind}`) }}
                  · {{ item.connectionName }}
                </p>
              </div>
            </div>
            <div class="grant-capability">
              <strong>{{ item.capabilityName }}</strong>
              <span class="mono">{{ item.capabilityKey }}</span>
              <span>
                {{ t("integrations.risk." + item.grant.risk) }} ·
                {{ item.grant.approvalPolicy }}
              </span>
              <span class="mono">{{ item.resourceKind }}</span>
              <span
                v-for="entry in item.resourceValues"
                :key="entry.key"
                class="mono resource-value"
                :title="entry.value"
              >
                {{ entry.key }}={{ entry.value }}
              </span>
            </div>
            <StatusBadge :state="item.enabled ? 'ENABLED' : 'REVOKED'" />
            <button
              v-if="
                item.enabled &&
                connectionAllows(item.connection, 'MANAGE_GRANTS')
              "
              class="button button--danger grant-revoke"
              type="button"
              :disabled="busy"
              @click="emit('revoke', item)"
            >
              <Trash2 :size="15" aria-hidden="true" />
              {{ t("integrations.revoke") }}
            </button>
          </article>
        </div>
        <div v-else class="grant-empty">
          <ShieldCheck :size="26" aria-hidden="true" />
          <h3>{{ t("integrations.noGrants") }}</h3>
          <p>{{ t("integrationsRedesign.noGrantsHint") }}</p>
        </div>
      </div>

      <aside class="grant-editor" aria-labelledby="grant-editor-title">
        <header>
          <div>
            <h3 id="grant-editor-title">
              {{ t("integrationsRedesign.grantEditorTitle") }}
            </h3>
            <p v-if="selectedConnection">{{ selectedConnection.name }}</p>
            <p v-else>{{ t("integrationsRedesign.chooseConnectionHint") }}</p>
          </div>
          <StatusBadge
            v-if="selectedConnection"
            :state="selectedConnection.state"
          />
        </header>

        <form class="grant-form" @submit.prevent="emit('save')">
          <label class="field">
            <span>{{ t("integrations.project") }}</span>
            <AsyncEntityPicker
              :model-value="projectRef"
              :selected="projectOption"
              :load-page="loadProjects"
              :disabled="!canManageSelected"
              :trigger-label="t('integrations.project')"
              :placeholder="t('integrations.chooseProject')"
              @select="
                chosenProject = $event;
                emit('update:projectRef', $event.ref);
              "
            />
          </label>
          <label class="field">
            <span>{{ t("integrations.targetType") }}</span>
            <select
              :value="targetKind"
              :disabled="!canManageSelected"
              @change="
                emit(
                  'update:targetKind',
                  ($event.target as HTMLSelectElement).value as
                    | 'AGENT'
                    | 'WORKFLOW',
                );
                emit('update:targetRef', '');
              "
            >
              <option value="AGENT">{{ t("integrations.agent") }}</option>
              <option value="WORKFLOW">{{ t("integrations.workflow") }}</option>
            </select>
          </label>
          <label class="field">
            <span>{{ t("integrations.target") }}</span>
            <AsyncEntityPicker
              :key="
                [
                  'recipient',
                  projectRef,
                  targetKind,
                  selectedConnection?.ref,
                ].join(':')
              "
              :model-value="targetRef"
              :selected="targetOption"
              :load-page="loadRecipients"
              :disabled="!canManageSelected || targetsLoading || !projectRef"
              :trigger-label="t('integrations.target')"
              :placeholder="t('integrations.chooseTarget')"
              @select="
                chosenTarget = $event;
                emit('update:targetRef', $event.ref);
              "
            />
          </label>
          <label class="field">
            <span>{{ t("integrations.capability") }}</span>
            <select
              :value="capabilityKey"
              :disabled="!canManageSelected"
              required
              @change="
                emit(
                  'update:capabilityKey',
                  ($event.target as HTMLSelectElement).value,
                )
              "
            >
              <option
                v-for="capability in selectedConnection?.capabilities ?? []"
                :key="capability.key"
                :value="capability.key"
              >
                {{ capability.name }} ·
                {{ t(`integrations.risk.${capability.risk}`) }}
              </option>
            </select>
          </label>

          <section
            v-if="selectedCapability"
            class="capability-boundary"
            aria-live="polite"
          >
            <header>
              <strong>{{ selectedCapability.name }}</strong>
              <span>{{
                t("integrations.risk." + selectedCapability.risk)
              }}</span>
            </header>
            <p>{{ selectedCapability.description }}</p>
            <dl>
              <div>
                <dt>Operation</dt>
                <dd class="mono">{{ selectedCapability.operation }}</dd>
              </div>
              <div>
                <dt>Resource scope</dt>
                <dd class="mono">{{ selectedCapability.resourceKind }}</dd>
              </div>
              <div>
                <dt>Approval policy</dt>
                <dd class="mono">
                  {{ selectedCapability.approvalPolicy }}
                </dd>
              </div>
            </dl>
          </section>

          <div class="missing-boundary">
            <LockKeyhole :size="17" aria-hidden="true" />
            <span>{{
              t("integrationsRedesign.resourceScopeUnavailable")
            }}</span>
          </div>
          <p class="grant-boundary">{{ t("integrations.grantBoundary") }}</p>
          <button
            class="button button--primary"
            type="submit"
            :disabled="
              !canManageSelected || busy || !targetRef || !capabilityKey
            "
          >
            <Plus :size="15" aria-hidden="true" />
            {{ t("integrations.grant") }}
          </button>
        </form>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.grants-panel {
  display: grid;
  gap: 14px;
}
.panel-heading,
.grant-target,
.grant-editor > header,
.missing-boundary {
  display: flex;
  align-items: center;
  gap: 10px;
}
.capability-boundary {
  display: grid;
  gap: 8px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
}
.capability-boundary > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.capability-boundary > header span {
  color: var(--muted);
  font-size: 0.76rem;
}
.capability-boundary p {
  margin: 0;
  color: var(--muted);
  font-size: 0.8rem;
}
.capability-boundary dl {
  display: grid;
  gap: 5px;
  margin: 0;
}
.capability-boundary dl > div {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 8px;
}
.capability-boundary dt {
  color: var(--muted);
  font-size: 0.72rem;
}
.capability-boundary dd {
  overflow-wrap: anywhere;
  margin: 0;
  font-size: 0.72rem;
}
.panel-heading,
.grant-editor > header {
  justify-content: space-between;
  align-items: flex-start;
}
.panel-heading h2,
.panel-heading p,
.grant-row h3,
.grant-row p,
.grant-editor h3,
.grant-editor p,
.grant-empty h3,
.grant-empty p,
.grant-boundary {
  margin-bottom: 0;
}
.panel-heading p,
.grant-row p,
.grant-editor p,
.grant-empty p,
.result-count,
.grant-boundary {
  color: var(--muted);
}
.grant-workspace {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 350px;
  gap: 14px;
  align-items: start;
}
.grant-list-column,
.grant-editor {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.connection-picker {
  display: grid;
  grid-template-columns: auto minmax(180px, 320px);
  align-items: center;
  gap: 10px;
  padding: 12px 13px;
  border-bottom: 1px solid var(--border);
}
.connection-picker > span {
  color: var(--muted);
  font-size: 0.8rem;
}
.grant-row {
  display: grid;
  grid-template-columns: minmax(190px, 1fr) minmax(170px, 0.8fr) auto auto;
  align-items: center;
  gap: 12px;
  min-height: 76px;
  padding: 11px 13px;
  border-top: 1px solid var(--hairline);
}
.grant-row:first-child {
  border-top: 0;
}
.grant-target {
  min-width: 0;
}
.grant-target > div,
.grant-capability {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.grant-icon {
  display: inline-grid;
  place-items: center;
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  border-radius: 7px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.grant-capability span {
  overflow: hidden;
  color: var(--muted);
  font-size: 0.72rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.grant-capability .resource-value {
  max-width: 260px;
}
.grant-editor {
  position: sticky;
  top: 74px;
  overflow: hidden;
}
.grant-editor > header {
  padding: 13px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.grant-form {
  display: grid;
  gap: 13px;
  padding: 13px;
}
.missing-boundary {
  align-items: flex-start;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 7px;
  color: var(--muted);
  background: var(--panel);
  font-size: 0.8rem;
}
.missing-boundary svg {
  flex: 0 0 auto;
}
.grant-empty {
  display: grid;
  justify-items: center;
  gap: 7px;
  padding: 42px 18px;
  text-align: center;
}
@media (max-width: 1060px) {
  .grant-workspace {
    grid-template-columns: 1fr;
  }
  .grant-editor {
    position: static;
  }
}
@media (max-width: 720px) {
  .panel-heading,
  .connection-picker {
    align-items: stretch;
  }
  .panel-heading {
    flex-direction: column;
  }
  .connection-picker,
  .grant-row {
    grid-template-columns: 1fr;
  }
  .grant-revoke {
    justify-self: stretch;
  }
}
</style>
