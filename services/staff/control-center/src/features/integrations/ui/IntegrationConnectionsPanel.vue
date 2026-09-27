<script setup lang="ts">
import {
  FlaskConical,
  Info,
  KeyRound,
  LoaderCircle,
  Pencil,
  Power,
  PowerOff,
  Search,
  ShieldCheck,
  Trash2,
} from "@lucide/vue";
import { ref, useId } from "vue";
import { useI18n } from "vue-i18n";

import { canConfigureCredential } from "@/features/integrations/connection-setup";
import {
  connectionAllows,
  isUnboundOpenAPITemplate,
} from "@/features/integrations/ui/model";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import EntityIcon from "@/shared/ui/EntityIcon.vue";
import { useServerMessage } from "@/shared/ui/server-message";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  connections: readonly IntegrationConnection[];
  definitions: Readonly<Record<string, IntegrationDefinition>>;
  coreReady: boolean;
  busyRef: string;
  busyAction?: "TEST" | "ENABLE" | "DISABLE";
  search?: string;
  loading?: boolean;
  hasMore?: boolean;
}>();

const emit = defineEmits<{
  command: [
    connection: IntegrationConnection,
    action: "TEST" | "ENABLE" | "DISABLE",
  ];
  credential: [connection: IntegrationConnection];
  edit: [connection: IntegrationConnection];
  delete: [connection: IntegrationConnection];
  grants: [connection: IntegrationConnection];
  details: [connection: IntegrationConnection];
  "update:search": [query: string];
  more: [];
}>();

const { t } = useI18n();
const serverMessage = useServerMessage();
const searchId = useId();
const sentinel = ref<HTMLElement>();
useCursorInfiniteScroll({
  sentinel,
  enabled: () => props.hasMore && !props.loading,
  loadMore: () => emit("more"),
});

function definition(
  connection: IntegrationConnection,
): IntegrationDefinition | undefined {
  return props.definitions[connection.definitionKey];
}
function needsBinding(connection: IntegrationConnection): boolean {
  return isUnboundOpenAPITemplate(connection, definition(connection));
}
function credentialLabel(connection: IntegrationConnection): string {
  if (needsBinding(connection))
    return t("integrations.openapiTemplateNeedsBinding");
  return (
    connection.credentialsHint ||
    t(
      connection.credentialsConfigured
        ? "integrations.credentialsConfigured"
        : "integrations.credentialsNotConfigured",
    )
  );
}
function activeGrantCount(connection: IntegrationConnection): number {
  return connection.grants.filter((grant) => grant.enabled).length;
}
function capabilityPreview(connection: IntegrationConnection): string {
  return connection.capabilities
    .slice(0, 3)
    .map((capability) => capability.name)
    .join(" · ");
}
</script>

<template>
  <section class="connections-panel" aria-labelledby="connections-title">
    <header class="panel-heading">
      <h2 id="connections-title">
        {{ t("integrationsRedesign.connectionsTitle") }}
      </h2>
      <span class="result-count">{{
        t(
          hasMore
            ? "integrationsRedesign.connectionsLoadedCount"
            : "integrationsRedesign.connectionCount",
          { count: connections.length },
        )
      }}</span>
    </header>
    <label class="connection-search" :for="searchId">
      <Search :size="18" aria-hidden="true" />
      <input
        :id="searchId"
        name="integration-connection-search"
        type="search"
        :value="search"
        :aria-label="t('common.search')"
        :placeholder="t('integrationsRedesign.searchConnections')"
        maxlength="500"
        @input="
          emit('update:search', ($event.target as HTMLInputElement).value)
        "
      />
    </label>
    <div
      v-if="coreReady && !connections.length && !search?.trim()"
      class="core-readiness"
      role="status"
    >
      <ShieldCheck :size="20" aria-hidden="true" />
      <div>
        <h3>{{ t("integrations.noConnectionsTitle") }}</h3>
        <p>{{ t("integrations.webOnlyReady") }}</p>
      </div>
    </div>
    <div
      v-if="connections.length"
      class="connection-table-wrap"
      :aria-busy="loading"
    >
      <table class="connection-table">
        <thead>
          <tr>
            <th scope="col">{{ t("integrationsRedesign.table.name") }}</th>
            <th scope="col">{{ t("integrationsRedesign.table.package") }}</th>
            <th scope="col">{{ t("integrationsRedesign.table.state") }}</th>
            <th scope="col">
              {{ t("integrationsRedesign.table.credentials") }}
            </th>
            <th scope="col">{{ t("integrationsRedesign.table.access") }}</th>
            <th scope="col">{{ t("integrationsRedesign.table.lastTest") }}</th>
            <th scope="col" class="connection-table__actions-heading">
              {{ t("common.actions") }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="connection in connections"
            :key="connection.ref"
            class="connection-row"
          >
            <td>
              <button
                class="connection-name"
                type="button"
                :title="connection.name"
                @click="emit('details', connection)"
              >
                <EntityIcon kind="INTEGRATION" />
                <span>{{ connection.name }}</span>
              </button>
            </td>
            <td>
              <strong
                class="connection-cell-text"
                :title="
                  definition(connection)?.name ?? connection.definitionKey
                "
                >{{
                  definition(connection)?.name ?? connection.definitionKey
                }}</strong
              >
              <small :title="connection.definitionDigest"
                >v{{ connection.definitionVersion }}</small
              >
            </td>
            <td><StatusBadge :state="connection.state" /></td>
            <td>
              <StatusBadge
                :state="
                  connection.credentialsConfigured && !needsBinding(connection)
                    ? 'READY'
                    : 'NEEDS_ATTENTION'
                "
                :label="credentialLabel(connection)"
              />
              <small
                v-if="needsBinding(connection)"
                :title="t('integrations.openapiTemplateNextStep')"
                >{{ t("integrations.openapiTemplateNextStep") }}</small
              >
            </td>
            <td>
              <span>{{
                t("integrationsRedesign.table.grants", {
                  count: activeGrantCount(connection),
                })
              }}</span>
              <small :title="capabilityPreview(connection)">{{
                t("integrationsRedesign.capabilityCount", {
                  count: connection.capabilities.length,
                })
              }}</small>
            </td>
            <td>
              <span
                v-if="connection.lastTestOutcome"
                class="connection-cell-text"
                :title="serverMessage(connection.lastTestOutcome)"
                >{{ serverMessage(connection.lastTestOutcome) }}</span
              >
              <span v-else>{{ t("common.noData") }}</span>
              <time
                v-if="connection.lastTestedAt"
                :datetime="connection.lastTestedAt"
                >{{ new Date(connection.lastTestedAt).toLocaleString() }}</time
              >
            </td>
            <td>
              <div
                class="connection-actions"
                role="group"
                :aria-label="connection.name"
              >
                <button
                  class="icon-button"
                  type="button"
                  :title="t('identity.details')"
                  :aria-label="t('identity.details')"
                  @click="emit('details', connection)"
                >
                  <Info :size="17" aria-hidden="true" />
                </button>
                <button
                  v-if="
                    canConfigureCredential(definition(connection), connection)
                  "
                  class="icon-button"
                  type="button"
                  :disabled="busyRef === connection.ref"
                  :title="t('integrations.configureCredential')"
                  :aria-label="t('integrations.configureCredential')"
                  @click="emit('credential', connection)"
                >
                  <KeyRound :size="17" aria-hidden="true" />
                </button>
                <button
                  v-if="connectionAllows(connection, 'TEST')"
                  class="icon-button"
                  type="button"
                  :disabled="busyRef === connection.ref"
                  :aria-busy="
                    busyRef === connection.ref && busyAction === 'TEST'
                  "
                  :title="
                    busyRef === connection.ref && busyAction === 'TEST'
                      ? t('integrationsRedesign.testingConnection')
                      : t('common.test')
                  "
                  :aria-label="t('common.test')"
                  @click="emit('command', connection, 'TEST')"
                >
                  <LoaderCircle
                    v-if="busyRef === connection.ref && busyAction === 'TEST'"
                    class="spin"
                    :size="17"
                    aria-hidden="true"
                  /><FlaskConical v-else :size="17" aria-hidden="true" />
                </button>
                <button
                  v-if="connectionAllows(connection, 'MANAGE_GRANTS')"
                  class="icon-button"
                  type="button"
                  :disabled="busyRef === connection.ref"
                  :title="t('integrations.manageGrants')"
                  :aria-label="t('integrations.manageGrants')"
                  @click="emit('grants', connection)"
                >
                  <ShieldCheck :size="17" aria-hidden="true" />
                </button>
                <button
                  v-if="connectionAllows(connection, 'ENABLE')"
                  class="icon-button"
                  type="button"
                  :disabled="busyRef === connection.ref"
                  :aria-busy="
                    busyRef === connection.ref && busyAction === 'ENABLE'
                  "
                  :title="
                    busyRef === connection.ref && busyAction === 'ENABLE'
                      ? t('integrationsRedesign.enablingConnection')
                      : t('common.enable')
                  "
                  :aria-label="t('common.enable')"
                  @click="emit('command', connection, 'ENABLE')"
                >
                  <LoaderCircle
                    v-if="busyRef === connection.ref && busyAction === 'ENABLE'"
                    class="spin"
                    :size="17"
                    aria-hidden="true"
                  /><Power v-else :size="17" aria-hidden="true" />
                </button>
                <button
                  v-if="connectionAllows(connection, 'DISABLE')"
                  class="icon-button icon-button--danger"
                  type="button"
                  :disabled="busyRef === connection.ref"
                  :aria-busy="
                    busyRef === connection.ref && busyAction === 'DISABLE'
                  "
                  :title="
                    busyRef === connection.ref && busyAction === 'DISABLE'
                      ? t('integrationsRedesign.disablingConnection')
                      : t('common.disable')
                  "
                  :aria-label="t('common.disable')"
                  @click="emit('command', connection, 'DISABLE')"
                >
                  <LoaderCircle
                    v-if="
                      busyRef === connection.ref && busyAction === 'DISABLE'
                    "
                    class="spin"
                    :size="17"
                    aria-hidden="true"
                  /><PowerOff v-else :size="17" aria-hidden="true" />
                </button>
                <button
                  v-if="connectionAllows(connection, 'UPDATE')"
                  class="icon-button"
                  type="button"
                  :disabled="busyRef === connection.ref"
                  :title="t('common.edit')"
                  :aria-label="t('common.edit')"
                  @click="emit('edit', connection)"
                >
                  <Pencil :size="17" aria-hidden="true" />
                </button>
                <button
                  v-if="connectionAllows(connection, 'DELETE')"
                  class="icon-button icon-button--danger"
                  type="button"
                  :disabled="busyRef === connection.ref"
                  :title="t('common.delete')"
                  :aria-label="t('common.delete')"
                  @click="emit('delete', connection)"
                >
                  <Trash2 :size="17" aria-hidden="true" />
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <span ref="sentinel" class="connection-sentinel" aria-hidden="true" />
    </div>
    <p v-else-if="loading" role="status">{{ t("common.loading") }}</p>
    <div v-else class="connection-empty">
      <PowerOff :size="28" aria-hidden="true" />
      <h3>
        {{
          t(
            search?.trim()
              ? "integrationsRedesign.noConnectionMatches"
              : "integrationsRedesign.noConnectionsYet",
          )
        }}
      </h3>
      <p>
        {{
          t(
            search?.trim()
              ? "integrationsRedesign.tryAnotherSearch"
              : "integrations.noConnections",
          )
        }}
      </p>
    </div>
  </section>
</template>

<style scoped>
.connections-panel {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.panel-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.panel-heading h2,
.core-readiness h3,
.core-readiness p,
.connection-empty h3,
.connection-empty p {
  margin: 0;
}
.result-count,
.core-readiness p,
.connection-empty p {
  color: var(--muted);
}
.connection-search {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 640px;
}
.connection-search input {
  min-width: 0;
  width: 100%;
}
.core-readiness {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 11px 12px;
  border: 1px solid color-mix(in srgb, var(--success) 30%, var(--border));
  border-radius: 8px;
  background: color-mix(in srgb, var(--success) 6%, var(--surface));
}
.core-readiness > svg {
  flex: 0 0 auto;
  margin-top: 1px;
  color: var(--success);
}
.core-readiness > div {
  display: grid;
  gap: 3px;
}
.connection-table-wrap {
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.connection-table {
  width: 100%;
  min-width: 1350px;
  table-layout: fixed;
  border-collapse: collapse;
}
.connection-table th,
.connection-table td {
  padding: 9px 10px;
  text-align: left;
  vertical-align: middle;
}
.connection-table th {
  color: var(--muted);
  font-size: 0.72rem;
  font-weight: 600;
}
.connection-table th:nth-child(1) {
  width: 20%;
}
.connection-table th:nth-child(2) {
  width: 13%;
}
.connection-table th:nth-child(3) {
  width: 9%;
}
.connection-table th:nth-child(4) {
  width: 17%;
}
.connection-table th:nth-child(5) {
  width: 10%;
}
.connection-table th:nth-child(6) {
  width: 11%;
}
.connection-table th:nth-child(7) {
  width: 20%;
}
.connection-row {
  height: 64px;
  border-top: 1px solid var(--border);
}
.connection-row:hover {
  background: var(--panel);
}
.connection-name {
  display: flex;
  align-items: center;
  gap: 10px;
  max-width: 100%;
  min-width: 0;
  padding: 0;
  border: 0;
  color: var(--text);
  background: transparent;
  font: inherit;
  font-weight: 600;
  text-align: left;
  cursor: pointer;
}
.connection-name:hover {
  color: var(--accent-strong);
  text-decoration: underline;
}
.connection-name span:last-child,
.connection-cell-text,
.connection-row small,
.connection-row time {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.connection-row small,
.connection-row time {
  margin-top: 3px;
  color: var(--muted);
  font-size: 0.75rem;
}
.connection-actions {
  display: flex;
  justify-content: flex-end;
  gap: 2px;
  white-space: nowrap;
}
.connection-table__actions-heading {
  text-align: right !important;
}
.connection-actions .icon-button--danger {
  color: var(--danger);
}
.connection-sentinel {
  display: block;
  height: 1px;
}
.spin {
  animation: connection-spin 0.8s linear infinite;
}
@keyframes connection-spin {
  to {
    transform: rotate(360deg);
  }
}
.connection-empty {
  display: grid;
  justify-items: center;
  gap: 7px;
  padding: 50px 20px;
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
  background: var(--panel);
  text-align: center;
}
</style>
