<script setup lang="ts">
import { LockKeyhole, Plus, Search, ShieldCheck, Trash2 } from "@lucide/vue";
import { computed, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  connectionAllows,
  type IntegrationGrantPresentation,
} from "@/features/integrations/ui/model";
import {
  approvalScopeOptions,
  validApprovalScopeSelection,
} from "@/features/integrations/approval-scope-options";
import type {
  IntegrationConnection,
  IntegrationGrantConnectionCandidate,
  IntegrationGrantProjectCandidate,
  IntegrationGrantRecipientCandidate,
  IntegrationGrantCapabilityCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import {
  useCursorInfiniteScroll,
  type AsyncEntityOption,
  type AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import {
  connectionCandidates,
  projectCandidates,
  recipientCandidates,
  capabilityCandidates,
  type IntegrationGrantSelection,
} from "@/features/integrations/grant-candidates";

const props = defineProps<{
  grants: readonly IntegrationGrantPresentation[];
  selectedConnection?: IntegrationConnection;
  projectRef: string;
  targetKind: "AGENT" | "WORKFLOW";
  targetRef: string;
  capabilityKey: string;
  busy: boolean;
  search?: string;
  loading?: boolean;
  hasMore?: boolean;
}>();

const emit = defineEmits<{
  selectConnection: [connectionRef: string];
  "update:projectRef": [value: string];
  "update:targetKind": [value: "AGENT" | "WORKFLOW"];
  "update:targetRef": [value: string];
  "update:capabilityKey": [value: string];
  save: [selection: IntegrationGrantSelection];
  revoke: [grant: IntegrationGrantPresentation];
  "update:search": [value: string];
  more: [];
}>();

const { t } = useI18n();
const fieldPrefix = `integration-grants-${useId()}`;
const chosenProject = ref<AsyncEntityOption>();
const chosenTarget = ref<AsyncEntityOption>();
const chosenCapability = ref<AsyncEntityOption>();
const projectCandidate = ref<IntegrationGrantProjectCandidate>();
const recipientCandidate = ref<IntegrationGrantRecipientCandidate>();
const capabilityCandidate = ref<IntegrationGrantCapabilityCandidate>();
const approvalScopePaths = ref<string[]>([]);
const scrollRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
useCursorInfiniteScroll({
  root: scrollRoot,
  sentinel,
  enabled: () => props.hasMore && !props.loading,
  loadMore: () => emit("more"),
});
const availableApprovalScopePaths = computed(() =>
  approvalScopeOptions(capabilityCandidate.value?.capability.inputSchema),
);
const connectionRows = ref(
  new Map<string, IntegrationGrantConnectionCandidate>(),
);
const connectionCandidate = computed(() => {
  const selected = props.selectedConnection;
  const candidate = selected && connectionRows.value.get(selected.ref);
  return candidate?.pins.connectionVersion === selected?.version
    ? candidate
    : undefined;
});
function scopeLabel(candidate: IntegrationGrantConnectionCandidate): string {
  return Object.entries(candidate.resourceScope)
    .map(([key, value]) => `${key}=${value.slice(0, 160)}`)
    .join(" · ");
}
function approvalPolicyLabel(value: string): string {
  const key = `integrations.approvalPolicies.${value}`;
  return t(key);
}
function resourceKindLabel(value: string): string {
  const key = `integrations.integrationResourceKinds.${value}`;
  return t(key);
}
function approvalPathLabel(path: string): string {
  return path
    .split("/")
    .filter(Boolean)
    .map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"))
    .join(".");
}
const projectRows = new Map<string, IntegrationGrantProjectCandidate>();
const recipientRows = new Map<string, IntegrationGrantRecipientCandidate>();
const capabilityRows = new Map<string, IntegrationGrantCapabilityCandidate>();
let projectGeneration = 0;
let recipientGeneration = 0;
let capabilityGeneration = 0;
const connectionLoader = connectionCandidates({ purpose: "GRANT" });
const projectLoader = computed(() =>
  projectCandidates({ connectionRef: props.selectedConnection?.ref ?? "" }),
);
const recipientLoader = computed(() =>
  recipientCandidates(
    {
      connectionRef: props.selectedConnection?.ref ?? "",
      projectRef: props.projectRef,
      recipientKind: props.targetKind,
    },
    projectCandidate.value?.pins,
  ),
);
const capabilityLoader = computed(() =>
  capabilityCandidates(
    {
      connectionRef: props.selectedConnection?.ref ?? "",
      projectRef: props.projectRef,
      recipientKind: props.targetKind,
      recipientRef: props.targetRef,
    },
    recipientCandidate.value?.pins,
  ),
);
const recipientContextKey = computed(() =>
  JSON.stringify([
    props.selectedConnection?.ref,
    props.selectedConnection?.version,
    props.projectRef,
    props.targetKind,
    projectCandidate.value?.pins,
  ]),
);
const capabilityContextKey = computed(() =>
  JSON.stringify([
    recipientContextKey.value,
    props.targetRef,
    recipientCandidate.value?.pins,
  ]),
);
const selection = computed<IntegrationGrantSelection | undefined>(() => {
  const connection = props.selectedConnection,
    project = projectCandidate.value,
    recipient = recipientCandidate.value,
    capability = capabilityCandidate.value;
  if (
    !connection ||
    !project?.grantable ||
    !recipient?.grantable ||
    !capability?.grantable ||
    project.projectRef !== props.projectRef ||
    recipient.recipientRef !== props.targetRef ||
    recipient.recipientKind !== props.targetKind ||
    capability.capability.key !== props.capabilityKey ||
    capability.pins.connectionVersion !== connection.version
  )
    return undefined;
  if (
    capability.capability.approvalPolicy === "HUMAN_SCOPED" &&
    !validApprovalScopeSelection(
      approvalScopePaths.value,
      availableApprovalScopePaths.value,
    )
  )
    return undefined;
  return {
    connectionRef: connection.ref,
    connectionVersion: connection.version,
    projectRef: project.projectRef,
    recipientKind: recipient.recipientKind,
    recipientRef: recipient.recipientRef,
    capabilityKey: capability.capability.key,
    ...(capability.capability.approvalPolicy === "HUMAN_SCOPED"
      ? { approvalScopePaths: [...approvalScopePaths.value].sort() }
      : {}),
  };
});
function toggleApprovalScopePath(path: string, checked: boolean): void {
  if (!availableApprovalScopePaths.value.includes(path)) return;
  approvalScopePaths.value = checked
    ? [...new Set([...approvalScopePaths.value, path])].sort()
    : approvalScopePaths.value.filter((candidate) => candidate !== path);
}
function submit(): void {
  if (selection.value && !props.busy) emit("save", selection.value);
}
function clearCapability(): void {
  approvalScopePaths.value = [];
  capabilityGeneration += 1;
  capabilityCandidate.value = undefined;
  chosenCapability.value = undefined;
  capabilityRows.clear();
  emit("update:capabilityKey", "");
}
function clearRecipient(): void {
  recipientGeneration += 1;
  recipientCandidate.value = undefined;
  chosenTarget.value = undefined;
  recipientRows.clear();
  clearCapability();
  emit("update:targetRef", "");
}
function clearProject(): void {
  projectGeneration += 1;
  projectCandidate.value = undefined;
  chosenProject.value = undefined;
  projectRows.clear();
  clearRecipient();
  emit("update:projectRef", "");
}
function changeConnection(value: string | readonly string[] | null): void {
  clearProject();
  emit("selectConnection", typeof value === "string" ? value : "");
}
function chooseProject(option: AsyncEntityOption): void {
  const candidate = projectRows.get(option.ref);
  if (!candidate?.grantable) return;
  // Тот же ref может обозначать новую ревизию; props watcher её не замечает.
  clearRecipient();
  projectCandidate.value = candidate;
  chosenProject.value = option;
  emit("update:projectRef", option.ref);
}
function chooseRecipient(option: AsyncEntityOption): void {
  const candidate = recipientRows.get(option.ref);
  if (!candidate?.grantable) return;
  clearCapability();
  recipientCandidate.value = candidate;
  chosenTarget.value = option;
  emit("update:targetRef", option.ref);
}
function chooseCapability(option: AsyncEntityOption): void {
  const candidate = capabilityRows.get(option.ref);
  if (!candidate?.grantable) return;
  approvalScopePaths.value = [];
  capabilityCandidate.value = candidate;
  chosenCapability.value = option;
  emit("update:capabilityKey", option.ref);
}
const projectOption = computed(() =>
  chosenProject.value?.ref === props.projectRef
    ? chosenProject.value
    : undefined,
);
const targetOption = computed(() =>
  chosenTarget.value?.ref === props.targetRef ? chosenTarget.value : undefined,
);
const connectionOption = computed(() =>
  props.selectedConnection
    ? {
        ref: props.selectedConnection.ref,
        title: props.selectedConnection.name,
        description: connectionCandidate.value
          ? [
              connectionCandidate.value.providerName,
              connectionCandidate.value.credentialKind,
            ]
              .filter(Boolean)
              .join(" · ")
          : props.selectedConnection.credentialsHint,
        meta: [
          t(`states.${props.selectedConnection.state}`),
          connectionCandidate.value && scopeLabel(connectionCandidate.value),
        ]
          .filter(Boolean)
          .join(" · "),
      }
    : undefined,
);
async function loadProjects(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
): Promise<AsyncEntityOptionPage> {
  if (!props.selectedConnection) return { items: [] };
  const generation = projectGeneration;
  const page = await projectLoader.value(query, cursor, signal, pageSize);
  if (signal.aborted || generation !== projectGeneration) return { items: [] };
  if (page.pins.connectionVersion !== props.selectedConnection.version)
    throw new Error("Integration connection version changed");
  if (!cursor) projectRows.clear();
  page.items.forEach((item) => projectRows.set(item.projectRef, item));
  return {
    items: page.items.map((item) => ({
      ref: item.projectRef,
      title: item.name,
      meta: t(`integrationCandidates.${item.reason}`),
      disabled: !item.grantable,
      disabledReason: item.grantable
        ? undefined
        : t(`integrationCandidates.${item.reason}`),
    })),
    nextPageToken: page.nextPageToken,
    total: page.total,
  };
}
async function loadRecipients(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
): Promise<AsyncEntityOptionPage> {
  if (!props.projectRef || !props.selectedConnection) return { items: [] };
  const generation = recipientGeneration;
  const page = await recipientLoader.value(query, cursor, signal, pageSize);
  if (signal.aborted || generation !== recipientGeneration)
    return { items: [] };
  if (!cursor) recipientRows.clear();
  page.items.forEach((item) => recipientRows.set(item.recipientRef, item));
  return {
    items: page.items.map((item) => ({
      ref: item.recipientRef,
      title: item.name,
      description: t(`integrationsRedesign.targetKind.${item.recipientKind}`),
      meta: t(`integrationCandidates.${item.reason}`),
      disabled: !item.grantable,
      disabledReason: item.grantable
        ? undefined
        : t(`integrationCandidates.${item.reason}`),
    })),
    nextPageToken: page.nextPageToken,
    total: page.total,
  };
}
async function loadConnections(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
): Promise<AsyncEntityOptionPage> {
  const page = await connectionLoader(query, cursor, signal, pageSize);
  if (signal.aborted) return { items: [] };
  if (!cursor) connectionRows.value.clear();
  page.items.forEach((item) =>
    connectionRows.value.set(item.connectionRef, item),
  );
  return {
    items: page.items.map((item) => ({
      ref: item.connectionRef,
      title: item.name,
      description: [
        item.providerName,
        item.credentialKind,
        item.projectRef,
        scopeLabel(item),
      ]
        .filter(Boolean)
        .join(" · "),
      meta: t(`integrationCandidates.${item.reason}`),
      disabled: !item.grantable,
      disabledReason: item.grantable
        ? undefined
        : t(`integrationCandidates.${item.reason}`),
    })),
    nextPageToken: page.nextPageToken,
    total: page.total,
  };
}
async function loadCapabilities(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
): Promise<AsyncEntityOptionPage> {
  if (!recipientCandidate.value || !props.selectedConnection)
    return { items: [] };
  const generation = capabilityGeneration;
  const page = await capabilityLoader.value(query, cursor, signal, pageSize);
  if (signal.aborted || generation !== capabilityGeneration)
    return { items: [] };
  if (!cursor) capabilityRows.clear();
  page.items.forEach((item) => capabilityRows.set(item.capability.key, item));
  return {
    items: page.items.map((item) => ({
      ref: item.capability.key,
      title: item.capability.name,
      description: item.capability.description,
      meta: [
        t(`integrations.risk.${item.capability.risk}`),
        resourceKindLabel(item.capability.resourceKind),
        approvalPolicyLabel(item.capability.approvalPolicy),
      ].join(" · "),
      disabled: !item.grantable,
      disabledReason: item.grantable
        ? undefined
        : t(`integrationCandidates.${item.reason}`),
    })),
    total: page.total,
    nextPageToken: page.nextPageToken,
  };
}
watch(
  () => [props.selectedConnection?.ref, props.selectedConnection?.version],
  () => {
    clearProject();
  },
  { flush: "sync" },
);
watch(
  () => [props.projectRef, props.targetKind, props.selectedConnection?.ref],
  () => {
    clearRecipient();
  },
  { flush: "sync" },
);
watch(
  () => [
    props.selectedConnection?.version,
    props.projectRef,
    props.targetKind,
    props.targetRef,
  ],
  () => {
    clearCapability();
  },
  { flush: "sync" },
);
const selectedCapability = computed(
  () => capabilityCandidate.value?.capability,
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
      <div ref="scrollRoot" class="grant-list-column">
        <div class="grant-list-toolbar">
          <label class="grant-search">
            <Search :size="16" aria-hidden="true" />
            <span class="sr-only">{{
              t("integrationsRedesign.searchGrantConnections")
            }}</span>
            <input
              type="search"
              :value="search ?? ''"
              :placeholder="t('integrationsRedesign.searchGrantConnections')"
              @input="
                emit('update:search', ($event.target as HTMLInputElement).value)
              "
            />
          </label>
          <label class="connection-picker">
            <span>{{ t("integrationsRedesign.connectionPicker") }}</span>
            <AsyncEntityPicker
              :model-value="selectedConnection?.ref"
              :selected="connectionOption"
              :load-page="loadConnections"
              :trigger-label="t('integrationsRedesign.connectionPicker')"
              :placeholder="t('integrationsRedesign.allConnections')"
              @update:model-value="changeConnection"
            />
          </label>
        </div>

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
              <span>
                {{ t("integrations.risk." + item.grant.risk) }} ·
                {{ approvalPolicyLabel(item.grant.approvalPolicy) }}
              </span>
              <span>{{ resourceKindLabel(item.resourceKind) }}</span>
              <span
                v-for="path in item.grant.approvalScopePaths"
                :key="path"
                class="mono resource-value"
                >{{ approvalPathLabel(path) }}</span
              >
              <span
                v-for="entry in item.resourceValues"
                :key="entry.key"
                class="resource-value"
                :title="`${entry.key}=${entry.value}`"
              >
                {{ entry.value }}
              </span>
              <details class="grant-technical-details">
                <summary>{{ t("integrations.technicalDetails") }}</summary>
                <code>{{ item.capabilityKey }}</code>
                <code>{{ item.resourceKind }}</code>
                <code>{{ item.grant.approvalPolicy }}</code>
              </details>
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
        <p v-if="loading && grants.length" class="grant-loading" role="status">
          {{ t("common.loading") }}
        </p>
        <span ref="sentinel" class="grant-sentinel" aria-hidden="true" />
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

        <form class="grant-form" @submit.prevent="submit">
          <label class="field">
            <span>{{ t("integrations.project") }}</span>
            <AsyncEntityPicker
              :key="`${selectedConnection?.ref}:${selectedConnection?.version}`"
              :model-value="projectRef"
              :selected="projectOption"
              :load-page="loadProjects"
              :disabled="!canManageSelected"
              :trigger-label="t('integrations.project')"
              :placeholder="t('integrations.chooseProject')"
              @select="chooseProject"
              @update:model-value="$event === null && clearProject()"
            />
          </label>
          <label class="field">
            <span>{{ t("integrations.targetType") }}</span>
            <select
              :id="`${fieldPrefix}-target-kind`"
              :name="`${fieldPrefix}-target-kind`"
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
              :key="recipientContextKey"
              :model-value="targetRef"
              :selected="targetOption"
              :load-page="loadRecipients"
              :disabled="!canManageSelected || busy || !projectCandidate"
              :trigger-label="t('integrations.target')"
              :placeholder="t('integrations.chooseTarget')"
              @select="chooseRecipient"
              @update:model-value="$event === null && clearRecipient()"
            />
          </label>
          <label class="field">
            <span>{{ t("integrations.capability") }}</span>
            <AsyncEntityPicker
              :key="capabilityContextKey"
              :model-value="capabilityKey"
              :selected="chosenCapability"
              :load-page="loadCapabilities"
              :disabled="!canManageSelected || !recipientCandidate || busy"
              :trigger-label="t('integrations.capability')"
              :placeholder="t('integrations.capability')"
              @select="chooseCapability"
              @update:model-value="$event === null && clearCapability()"
            />
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
                <dt>{{ t("integrations.operation") }}</dt>
                <dd class="mono">{{ selectedCapability.operation }}</dd>
              </div>
              <div>
                <dt>{{ t("integrations.resourceKind") }}</dt>
                <dd>
                  {{ resourceKindLabel(selectedCapability.resourceKind) }}
                </dd>
              </div>
              <div>
                <dt>{{ t("integrations.approvalPolicy") }}</dt>
                <dd>
                  {{ approvalPolicyLabel(selectedCapability.approvalPolicy) }}
                </dd>
              </div>
            </dl>
          </section>

          <fieldset
            v-if="selectedCapability?.approvalPolicy === 'HUMAN_SCOPED'"
            class="capability-boundary"
          >
            <legend>{{ t("integrations.approvalScopeTitle") }}</legend>
            <p>{{ t("integrations.approvalScopeHelp") }}</p>
            <p v-if="!availableApprovalScopePaths.length">
              {{ t("integrations.approvalScopeUnavailable") }}
            </p>
            <label
              v-for="path in availableApprovalScopePaths"
              :key="path"
              class="approval-scope-option"
            >
              <input
                type="checkbox"
                :name="`${fieldPrefix}-approval-scope`"
                :value="path"
                :checked="approvalScopePaths.includes(path)"
                :disabled="
                  busy ||
                  !canManageSelected ||
                  (!approvalScopePaths.includes(path) &&
                    approvalScopePaths.length >= 16)
                "
                @change="
                  toggleApprovalScopePath(
                    path,
                    ($event.target as HTMLInputElement).checked,
                  )
                "
              />
              <code>{{ approvalPathLabel(path) }}</code>
            </label>
          </fieldset>

          <SafeStructuredData
            v-if="connectionCandidate"
            :value="connectionCandidate.resourceScope"
            literal
            :label="t('integrations.resourceScope')"
          />
          <div v-else class="missing-boundary">
            <LockKeyhole :size="17" aria-hidden="true" />
            <span>{{ t("integrationsRedesign.resourceScopeRefresh") }}</span>
          </div>
          <p class="grant-boundary">{{ t("integrations.grantBoundary") }}</p>
          <button
            class="button button--primary"
            type="submit"
            :disabled="!canManageSelected || busy || !selection"
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
.approval-scope-option {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  min-width: 0;
  font-size: 0.8rem;
}
.approval-scope-option code {
  overflow-wrap: anywhere;
}
.grant-technical-details {
  color: var(--muted);
  font-size: 0.72rem;
}
.grant-technical-details summary {
  cursor: pointer;
}
.grant-technical-details code {
  display: block;
  margin-top: 4px;
  overflow-wrap: anywhere;
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
.grant-list-column {
  max-height: calc(100vh - 250px);
  overflow: auto;
}
.grant-list-toolbar {
  position: sticky;
  z-index: 1;
  top: 0;
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(300px, auto);
  align-items: end;
  gap: 10px;
  padding: 12px 13px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.grant-search {
  position: relative;
  min-width: 0;
}
.grant-search > svg {
  position: absolute;
  top: 50%;
  left: 10px;
  color: var(--subtle);
  transform: translateY(-50%);
}
.grant-search input {
  padding-left: 34px;
}
.connection-picker {
  display: grid;
  grid-template-columns: auto minmax(180px, 320px);
  align-items: center;
  gap: 10px;
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
.grant-loading {
  margin: 0;
  padding: 10px 13px;
  color: var(--muted);
  text-align: center;
}
.grant-sentinel {
  display: block;
  width: 1px;
  height: 1px;
}
@media (max-width: 1060px) {
  .grant-workspace {
    grid-template-columns: 1fr;
  }
  .grant-editor {
    position: static;
  }
  .grant-list-column {
    max-height: none;
  }
}
@media (max-width: 720px) {
  .panel-heading,
  .grant-list-toolbar,
  .connection-picker {
    align-items: stretch;
  }
  .panel-heading {
    flex-direction: column;
  }
  .connection-picker,
  .grant-list-toolbar,
  .grant-row {
    grid-template-columns: 1fr;
  }
  .grant-revoke {
    justify-self: stretch;
  }
}
</style>
