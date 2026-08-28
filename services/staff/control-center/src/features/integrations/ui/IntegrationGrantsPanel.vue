<script setup lang="ts">
import { LockKeyhole, Plus, ShieldCheck, Trash2 } from "@lucide/vue";
import { computed } from "vue";
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
          <select
            :value="selectedConnection?.ref ?? ''"
            @change="
              emit(
                'selectConnection',
                ($event.target as HTMLSelectElement).value,
              )
            "
          >
            <option value="">
              {{ t("integrationsRedesign.allConnections") }}
            </option>
            <option
              v-for="connection in connections"
              :key="connection.ref"
              :value="connection.ref"
            >
              {{ connection.name }}
            </option>
          </select>
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
            <select
              :value="projectRef"
              :disabled="!canManageSelected"
              required
              @change="
                emit(
                  'update:projectRef',
                  ($event.target as HTMLSelectElement).value,
                );
                emit('loadTargets');
              "
            >
              <option value="">{{ t("integrations.chooseProject") }}</option>
              <option
                v-for="project in projects"
                :key="project.ref"
                :value="project.ref"
              >
                {{ project.name }}
              </option>
            </select>
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
            <select
              :value="targetRef"
              :disabled="!canManageSelected || targetsLoading || !projectRef"
              required
              @change="
                emit(
                  'update:targetRef',
                  ($event.target as HTMLSelectElement).value,
                )
              "
            >
              <option value="">
                {{
                  targetsLoading
                    ? t("common.loading")
                    : t("integrations.chooseTarget")
                }}
              </option>
              <option
                v-for="target in targets"
                :key="target.ref"
                :value="target.ref"
              >
                {{ target.name }}
              </option>
            </select>
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
