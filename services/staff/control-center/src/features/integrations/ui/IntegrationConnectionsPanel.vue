<script setup lang="ts">
import {
  FlaskConical,
  KeyRound,
  Power,
  PowerOff,
  ShieldCheck,
} from "@lucide/vue";
import { useI18n } from "vue-i18n";

import { canConfigureCredential } from "@/features/integrations/connection-setup";
import { connectionAllows } from "@/features/integrations/ui/model";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{
  connections: readonly IntegrationConnection[];
  definitions: Readonly<Record<string, IntegrationDefinition>>;
  busyRef: string;
}>();

const emit = defineEmits<{
  command: [
    connection: IntegrationConnection,
    action: "TEST" | "ENABLE" | "DISABLE",
  ];
  credential: [connection: IntegrationConnection];
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

    <div v-if="connections.length" class="connection-table" role="list">
      <article
        v-for="connection in connections"
        :key="connection.ref"
        class="connection-row connection-card"
        role="listitem"
      >
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
          <div class="credential-state">
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
          <p v-if="connection.lastTestOutcome" class="last-test">
            {{ t("integrations.lastTest") }}: {{ connection.lastTestOutcome }}
          </p>
          <div class="connection-capabilities">
            <span
              v-for="capability in connection.capabilities.slice(0, 4)"
              :key="capability.key"
              >{{ capability.name }}</span
            >
            <span v-if="connection.capabilities.length > 4"
              >+{{ connection.capabilities.length - 4 }}</span
            >
          </div>
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

        <div class="connection-actions">
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
            @click="emit('command', connection, 'TEST')"
          >
            <FlaskConical :size="15" aria-hidden="true" />
            {{ t("common.test") }}
          </button>
          <button
            v-if="connectionAllows(connection, 'MANAGE_GRANTS')"
            class="button"
            type="button"
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
            @click="emit('command', connection, 'ENABLE')"
          >
            <Power :size="15" aria-hidden="true" />
            {{ t("common.enable") }}
          </button>
          <button
            v-if="connectionAllows(connection, 'DISABLE')"
            class="button button--danger"
            type="button"
            :disabled="busyRef === connection.ref"
            @click="emit('command', connection, 'DISABLE')"
          >
            <PowerOff :size="15" aria-hidden="true" />
            {{ t("common.disable") }}
          </button>
        </div>
      </article>
    </div>
    <div v-else class="connection-empty">
      <PowerOff :size="28" aria-hidden="true" />
      <h3>{{ t("integrations.noConnectionsTitle") }}</h3>
      <p>{{ t("integrations.noConnections") }}</p>
      <p>{{ t("integrations.webOnlyReady") }}</p>
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
.credential-state {
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
.panel-heading p,
.connection-main p,
.result-count,
.connection-facts,
.connection-empty p {
  color: var(--muted);
}
.connection-table {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.connection-row {
  display: grid;
  grid-template-columns: minmax(280px, 1fr) minmax(150px, auto) minmax(
      250px,
      auto
    );
  align-items: center;
  gap: 18px;
  min-height: 104px;
  padding: 13px 14px;
  border-top: 1px solid var(--hairline);
}
.connection-row:first-child {
  border-top: 0;
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
.last-test {
  font-size: 0.8rem;
}
.credential-state {
  flex-wrap: wrap;
  color: var(--muted);
  font-size: 0.8rem;
}
.connection-capabilities {
  flex-wrap: wrap;
  margin-top: 3px;
}
.connection-capabilities span {
  padding: 3px 6px;
  border-radius: 5px;
  color: var(--muted);
  background: var(--panel);
  font-size: 0.74rem;
}
.connection-facts {
  align-items: stretch;
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
  .connection-row {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .connection-actions {
    grid-column: 1 / -1;
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
  .connection-row {
    grid-template-columns: 1fr;
  }
  .connection-actions,
  .connection-facts {
    grid-column: auto;
  }
  .connection-actions .button {
    flex: 1 1 130px;
  }
}
</style>
