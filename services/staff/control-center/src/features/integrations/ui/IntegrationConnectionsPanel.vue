<script setup lang="ts">
import {
  FlaskConical,
  KeyRound,
  LoaderCircle,
  Pencil,
  Power,
  PowerOff,
  ShieldCheck,
  Trash2,
} from "@lucide/vue";
import { useI18n } from "vue-i18n";

import { canConfigureCredential } from "@/features/integrations/connection-setup";
import {
  connectionAllows,
  publicIntegrationConfiguration,
} from "@/features/integrations/ui/model";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{
  connections: readonly IntegrationConnection[];
  definitions: Readonly<Record<string, IntegrationDefinition>>;
  coreReady: boolean;
  busyRef: string;
  busyAction?: "TEST" | "ENABLE" | "DISABLE";
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
}>();

const { t } = useI18n();
</script>

<template>
  <section class="connections-panel" aria-labelledby="connections-title">
    <header class="panel-heading">
      <div>
        <h2 id="connections-title">
          {{ t("integrationsRedesign.connectionsTitle") }}
        </h2>
        <p>{{ t("integrationsRedesign.connectionsDescription") }}</p>
      </div>
      <span class="result-count">{{
        t("integrationsRedesign.connectionCount", {
          count: connections.length,
        })
      }}</span>
    </header>
    <div v-if="coreReady" class="core-readiness" role="status">
      <ShieldCheck :size="20" aria-hidden="true" />
      <div>
        <h3>{{ t("integrations.noConnectionsTitle") }}</h3>
        <p>{{ t("integrations.webOnlyReady") }}</p>
      </div>
    </div>
    <div v-if="connections.length" class="connection-grid" role="list">
      <article
        v-for="connection in connections"
        :key="connection.ref"
        class="connection-row connection-card"
        role="listitem"
      >
        <header class="connection-card__heading">
          <div class="connection-main">
            <div class="connection-title">
              <h3>{{ connection.name }}</h3>
              <StatusBadge :state="connection.state" />
            </div>
            <p>
              {{
                definitions[connection.definitionKey]?.name ??
                connection.definitionKey
              }}
            </p>
          </div>
          <div class="connection-version">
            <span class="mono">v{{ connection.definitionVersion }}</span>
            <span class="mono" :title="connection.definitionDigest"
              >{{ connection.definitionDigest.slice(0, 12) }}…</span
            >
          </div>
        </header>

        <section class="credential-state">
          <div>
            <StatusBadge
              :state="
                connection.credentialsConfigured ? 'READY' : 'NEEDS_ATTENTION'
              "
              :label="
                connection.credentialsConfigured
                  ? t('integrations.credentialsConfigured')
                  : t('integrations.credentialsNotConfigured')
              "
            />
            <span>{{ connection.credentialsHint }}</span>
          </div>
          <code
            v-if="definitions[connection.definitionKey]?.credentialSecretKey"
          >
            {{ definitions[connection.definitionKey]?.credentialSecretKey }}
          </code>
        </section>

        <dl
          v-if="
            publicIntegrationConfiguration(
              connection,
              definitions[connection.definitionKey],
            ).length
          "
          class="public-configuration"
        >
          <div
            v-for="entry in publicIntegrationConfiguration(
              connection,
              definitions[connection.definitionKey],
            )"
            :key="entry.key"
          >
            <dt>{{ entry.label }}</dt>
            <dd :title="entry.value">{{ entry.value }}</dd>
          </div>
        </dl>

        <div
          v-if="connection.lastTestOutcome || connection.lastTestedAt"
          class="last-test"
        >
          <strong>{{ t("integrations.lastTest") }}</strong>
          <span v-if="connection.lastTestOutcome">{{
            connection.lastTestOutcome
          }}</span>
          <time
            v-if="connection.lastTestedAt"
            :datetime="connection.lastTestedAt"
          >
            {{ new Date(connection.lastTestedAt).toLocaleString() }}
          </time>
        </div>

        <div class="connection-capabilities">
          <span
            v-for="capability in connection.capabilities"
            :key="capability.key"
            :title="capability.description"
          >
            <strong>{{ capability.name }}</strong>
            {{ t("integrations.risk." + capability.risk) }}
            <code>{{ capability.resourceKind }}</code>
            <ShieldCheck
              v-if="capability.approvalRequired"
              :size="12"
              aria-label="Human Gate"
            />
          </span>
        </div>

        <div class="connection-facts">
          <span>
            <strong>{{
              connection.grants.filter((item) => item.enabled).length
            }}</strong>
            {{ t("integrationsRedesign.activeGrants") }}
          </span>
          <span>
            <strong>{{ connection.capabilities.length }}</strong>
            {{ t("integrationsRedesign.capabilitiesShort") }}
          </span>
        </div>

        <footer class="connection-actions">
          <button
            v-if="
              canConfigureCredential(
                definitions[connection.definitionKey],
                connection,
              )
            "
            class="button button--primary"
            type="button"
            :disabled="busyRef === connection.ref"
            @click="emit('credential', connection)"
          >
            <KeyRound :size="15" aria-hidden="true" />
            {{ t("integrations.configureCredential") }}
          </button>
          <button
            v-if="connectionAllows(connection, 'TEST')"
            class="button"
            type="button"
            :disabled="busyRef === connection.ref"
            :aria-busy="busyRef === connection.ref"
            @click="emit('command', connection, 'TEST')"
          >
            <LoaderCircle
              v-if="busyRef === connection.ref && busyAction === 'TEST'"
              class="spin"
              :size="15"
              aria-hidden="true"
            />
            <FlaskConical v-else :size="15" aria-hidden="true" />
            {{
              busyRef === connection.ref && busyAction === "TEST"
                ? "Проверяем…"
                : t("common.test")
            }}
          </button>
          <button
            v-if="connectionAllows(connection, 'MANAGE_GRANTS')"
            class="button"
            type="button"
            :disabled="busyRef === connection.ref"
            @click="emit('grants', connection)"
          >
            <ShieldCheck :size="15" aria-hidden="true" />
            {{ t("integrations.manageGrants") }}
          </button>
          <button
            v-if="connectionAllows(connection, 'ENABLE')"
            class="button"
            type="button"
            :disabled="busyRef === connection.ref"
            :aria-busy="busyRef === connection.ref"
            @click="emit('command', connection, 'ENABLE')"
          >
            <LoaderCircle
              v-if="busyRef === connection.ref && busyAction === 'ENABLE'"
              class="spin"
              :size="15"
              aria-hidden="true"
            />
            <Power v-else :size="15" aria-hidden="true" />
            {{
              busyRef === connection.ref && busyAction === "ENABLE"
                ? "Включаем…"
                : t("common.enable")
            }}
          </button>
          <button
            v-if="connectionAllows(connection, 'DISABLE')"
            class="button button--danger"
            type="button"
            :disabled="busyRef === connection.ref"
            :aria-busy="busyRef === connection.ref"
            @click="emit('command', connection, 'DISABLE')"
          >
            <LoaderCircle
              v-if="busyRef === connection.ref && busyAction === 'DISABLE'"
              class="spin"
              :size="15"
              aria-hidden="true"
            />
            <PowerOff v-else :size="15" aria-hidden="true" />
            {{
              busyRef === connection.ref && busyAction === "DISABLE"
                ? "Отключаем…"
                : t("common.disable")
            }}
          </button>
          <button
            v-if="connectionAllows(connection, 'UPDATE')"
            class="button"
            type="button"
            :disabled="busyRef === connection.ref"
            @click="emit('edit', connection)"
          >
            <Pencil :size="15" aria-hidden="true" />
            {{ t("common.edit") }}
          </button>
          <button
            v-if="connectionAllows(connection, 'DELETE')"
            class="button button--danger"
            type="button"
            :disabled="busyRef === connection.ref"
            @click="emit('delete', connection)"
          >
            <Trash2 :size="15" aria-hidden="true" />
            {{ t("common.delete") }}
          </button>
        </footer>
      </article>
    </div>
    <div v-else class="connection-empty">
      <PowerOff :size="28" aria-hidden="true" />
      <h3>{{ t("integrationsRedesign.noConnectionsYet") }}</h3>
      <p>{{ t("integrations.noConnections") }}</p>
    </div>
  </section>
</template>

<style scoped>
.connections-panel {
  display: grid;
  gap: 14px;
}
.panel-heading,
.connection-title,
.connection-actions,
.connection-facts,
.connection-capabilities,
.credential-state,
.credential-state > div,
.connection-card__heading {
  display: flex;
  align-items: center;
  gap: 9px;
}
.panel-heading {
  justify-content: space-between;
  align-items: flex-start;
}
.panel-heading h2,
.panel-heading p,
.connection-main h3,
.connection-main p,
.connection-empty h3,
.connection-empty p {
  margin-bottom: 0;
}
.projection-note {
  margin: -6px 1px 0;
  color: var(--muted);
  font-size: 0.76rem;
}
.panel-heading p,
.connection-main p,
.result-count,
.connection-facts,
.connection-empty p {
  color: var(--muted);
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
.core-readiness h3,
.core-readiness p {
  margin: 0;
}
.core-readiness p {
  color: var(--muted);
}
.connection-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 400px), 1fr));
  gap: 12px;
}
.connection-card {
  display: flex;
  flex-direction: column;
  min-height: 360px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.connection-card__heading {
  justify-content: space-between;
  align-items: flex-start;
  gap: 14px;
}
.connection-title {
  flex-wrap: wrap;
}
.connection-main {
  display: grid;
  min-width: 0;
  gap: 4px;
}
.connection-main h3 {
  overflow-wrap: anywhere;
}
.connection-version {
  display: grid;
  justify-items: end;
  gap: 2px;
  color: var(--muted);
  font-size: 0.7rem;
}
.last-test {
  display: grid;
  gap: 2px;
  margin-top: 9px;
  color: var(--muted);
  font-size: 0.8rem;
}
.last-test strong {
  color: var(--text-secondary);
}
.last-test time {
  color: var(--subtle);
  font-size: 0.72rem;
}
.credential-state {
  justify-content: space-between;
  flex-wrap: wrap;
  margin-top: 13px;
  padding: 9px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
  color: var(--muted);
  font-size: 0.8rem;
}
.credential-state > div {
  flex-wrap: wrap;
}
.credential-state code {
  color: var(--text-secondary);
  font-size: 0.72rem;
}
.public-configuration {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 7px;
  margin: 10px 0 0;
}
.public-configuration > div {
  display: grid;
  min-width: 0;
  gap: 2px;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.public-configuration dt {
  color: var(--muted);
  font-size: 0.7rem;
}
.public-configuration dd {
  overflow: hidden;
  margin: 0;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.connection-capabilities {
  flex-wrap: wrap;
  margin-top: 10px;
}
.connection-capabilities span {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 5px 7px;
  border-radius: 5px;
  color: var(--muted);
  background: var(--panel);
  font-size: 0.74rem;
}
.connection-capabilities strong {
  color: var(--text-secondary);
}
.connection-capabilities code {
  font-size: 0.68rem;
}
.connection-facts {
  align-items: stretch;
  margin-top: 12px;
}
.connection-facts span {
  display: grid;
  min-width: 70px;
  gap: 2px;
  padding-left: 10px;
  border-left: 1px solid var(--border);
  font-size: 0.76rem;
}
.connection-facts strong {
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 1rem;
}
.connection-actions {
  justify-content: flex-end;
  flex-wrap: wrap;
  margin-top: auto;
  padding-top: 16px;
  border-top: 1px solid var(--hairline);
}
.connection-actions .button {
  min-width: 112px;
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
  text-align: center;
  background: var(--panel);
}
@media (max-width: 980px) {
  .connection-actions {
    justify-content: flex-start;
  }
}
@media (max-width: 620px) {
  .panel-heading,
  .connection-row {
    align-items: stretch;
  }
  .panel-heading {
    flex-direction: column;
  }
  .connection-card__heading {
    flex-direction: column;
  }
  .connection-version {
    justify-items: start;
  }
  .public-configuration {
    grid-template-columns: 1fr;
  }
  .connection-actions .button {
    flex: 1 1 130px;
  }
}
</style>
