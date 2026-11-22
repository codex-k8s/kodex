<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, useId } from "vue";
import { useI18n } from "vue-i18n";
import { KeyRound, RefreshCw, Search } from "@lucide/vue";
import {
  personalConnectionAllows,
  usePersonalConnections,
} from "../personal-connections";
import { personalCredentialDelivery } from "../credential-delivery";
import SSHIdentitySetupPanel from "./SSHIdentitySetupPanel.vue";
import { personalSSHSetupContext } from "../ssh-identity-setup-context";
import GitHubRepositoryScopeSelector from "./GitHubRepositoryScopeSelector.vue";
import { usesMultilineSSHCredential } from "../credential-input";
import KubernetesResourceMaterialPanel from "./KubernetesResourceMaterialPanel.vue";
import KubernetesIdentitySetupPanel from "./KubernetesIdentitySetupPanel.vue";
import { isPersonalKubernetesSetup } from "../kubernetes-identity-setup-api";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const { t } = useI18n();
const prefix = useId();
const model = usePersonalConnections();
const {
  items,
  query,
  nextPageToken,
  loading,
  busyRef,
  problem,
  requiresRefresh,
  credentialValue,
  editingRef,
  success,
  unavailable,
} = model;
const entries = computed(() =>
  items.value.map((connection) => ({
    connection,
    sshContext: personalSSHSetupContext(connection),
  })),
);
onMounted(() => {
  window.addEventListener("pagehide", model.suspend);
  void model.load();
});
onBeforeUnmount(() => window.removeEventListener("pagehide", model.suspend));
</script>

<template>
  <section class="personal-connections" :aria-labelledby="`${prefix}-title`">
    <header class="personal-heading">
      <div>
        <h2 :id="`${prefix}-title`">{{ t("personalConnections.title") }}</h2>
        <p class="muted">{{ t("personalConnections.description") }}</p>
      </div>
      <button
        class="button"
        type="button"
        :disabled="!!busyRef || loading || unavailable"
        @click="model.load()"
      >
        <RefreshCw :size="16" aria-hidden="true" />{{
          t("personalConnections.refresh")
        }}
      </button>
    </header>
    <form class="personal-search" @submit.prevent="model.load()">
      <label :for="`${prefix}-search`">{{
        t("personalConnections.search")
      }}</label>
      <input
        :id="`${prefix}-search`"
        v-model="query"
        type="search"
        maxlength="200"
        :disabled="!!busyRef || unavailable"
      />
      <button
        class="button"
        type="submit"
        :disabled="!!busyRef || loading || unavailable"
      >
        <Search :size="16" aria-hidden="true" />{{
          t("personalConnections.find")
        }}
      </button>
    </form>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="requiresRefresh" role="alert">
      {{ t("personalConnections.refreshRequired") }}
    </p>
    <p v-if="success" role="status">
      {{ t(`personalConnections.success.${success}`) }}
    </p>
    <p v-if="loading" role="status">{{ t("personalConnections.loading") }}</p>
    <p v-if="!loading && !problem && !items.length">
      {{ t("personalConnections.empty") }}
    </p>

    <article
      v-for="{ connection, sshContext } in entries"
      :key="connection.connectionRef"
      class="card personal-connection"
      :data-connection-ref="connection.connectionRef"
    >
      <header class="personal-heading">
        <h3>{{ connection.name }}</h3>
        <span>{{ t(`personalConnections.policy.${connection.policy}`) }}</span>
      </header>
      <p>
        {{
          t(
            `personalConnections.states.${connection.credential?.state ?? "NOT_CONFIGURED"}`,
          )
        }}
      </p>
      <p v-if="!connection.available || !connection.enabled" class="muted">
        {{ t("personalConnections.unavailable") }}
      </p>
      <p
        v-if="!personalCredentialDelivery(connection)"
        class="muted"
        role="status"
      >
        {{ t("personalConnections.metadataUnknown") }}
      </p>
      <p
        v-else-if="
          personalCredentialDelivery(connection)?.kind === 'BROKER_IDENTITY'
        "
        class="muted"
      >
        {{ t("personalConnections.brokerManaged") }}
      </p>
      <template v-if="connection.credential">
        <p v-if="connection.credential.expiresAt">
          {{
            t("personalConnections.expiry", {
              value: connection.credential.expiresAt,
            })
          }}
        </p>
        <p>{{ t("personalConnections.confirmed") }}</p>
        <ul v-if="connection.credential.verifiedCapabilityKeys.length">
          <li
            v-for="key in connection.credential.verifiedCapabilityKeys"
            :key="key"
          >
            {{ key }}
          </li>
        </ul>
        <p v-else class="muted">{{ t("personalConnections.noneConfirmed") }}</p>
        <p v-if="connection.credential.deniedCapabilityKeys.length">
          {{
            t("personalConnections.denied", {
              operations: connection.credential.deniedCapabilityKeys.join(", "),
            })
          }}
        </p>
      </template>
      <SSHIdentitySetupPanel
        v-if="sshContext"
        :context="sshContext"
        :disabled="!!busyRef || loading || requiresRefresh || unavailable"
        @changed="model.load()"
      />
      <GitHubRepositoryScopeSelector
        v-if="
          connection.definitionKey === 'github' &&
          connection.policy === 'INITIATOR_REQUIRED' &&
          connection.credential?.verifiedCapabilityKeys.includes(
            'github.identity.read',
          )
        "
        :connection="connection"
        @changed="model.load()"
      />
      <KubernetesIdentitySetupPanel
        v-if="isPersonalKubernetesSetup(connection)"
        :context="{
          connectionRef: connection.connectionRef,
          connectionVersion: connection.connectionVersion,
          expectedVersion: connection.credential?.version ?? 0,
          requirements: connection.kubernetesSetupRequirements,
          policy: connection.policy,
          available: connection.available,
          enabled: connection.enabled,
        }"
        :disabled="
          !!busyRef ||
          loading ||
          requiresRefresh ||
          unavailable ||
          connection.credential?.state === 'CHECKING'
        "
        @changed="model.load()"
      />
      <KubernetesResourceMaterialPanel
        v-if="isPersonalKubernetesSetup(connection)"
        :context="{
          connectionRef: connection.connectionRef,
          connectionVersion: connection.connectionVersion,
          expectedVersion: connection.credential?.version ?? 0,
          policy: connection.policy,
          available: connection.available,
          enabled: connection.enabled,
        }"
        :disabled="
          !!busyRef ||
          loading ||
          requiresRefresh ||
          unavailable ||
          connection.credential?.state === 'CHECKING'
        "
      />
      <p class="muted">{{ t("personalConnections.permissionsHelp") }}</p>
      <p
        v-if="
          ['gitlab', 'jira-server-dc', 'confluence-server-dc'].includes(
            connection.definitionKey,
          ) && connection.policy === 'INITIATOR_REQUIRED'
        "
        class="muted"
      >
        {{ t("personalConnections.objectReadHelp") }}
      </p>
      <p
        v-if="
          connection.definitionKey === 'gitlab' &&
          connection.policy === 'INITIATOR_REQUIRED'
        "
        class="muted"
      >
        {{ t("personalConnections.gitlabWriteHelp") }}
      </p>
      <p
        v-if="
          connection.definitionKey === 'confluence-server-dc' &&
          connection.policy === 'INITIATOR_REQUIRED'
        "
        class="muted"
      >
        {{ t("personalConnections.pageUpdateHelp") }}
      </p>
      <p
        v-if="
          connection.definitionKey === 'jira-server-dc' &&
          connection.policy === 'INITIATOR_REQUIRED'
        "
        class="muted"
      >
        {{ t("personalConnections.jiraWriteHelp") }}
      </p>
      <div class="personal-actions">
        <button
          v-if="personalConnectionAllows(connection, 'SAVE')"
          class="button"
          type="button"
          :disabled="!!busyRef || loading || requiresRefresh || unavailable"
          @click="model.edit(connection)"
        >
          <KeyRound :size="16" aria-hidden="true" />{{
            t(
              connection.credential?.credentialConfigured
                ? "personalConnections.replace"
                : "personalConnections.add",
            )
          }}
        </button>
        <button
          v-if="personalConnectionAllows(connection, 'TEST')"
          class="button"
          type="button"
          :disabled="!!busyRef || loading || requiresRefresh || unavailable"
          @click="model.execute(connection.connectionRef, 'TEST')"
        >
          {{ t("personalConnections.test") }}
        </button>
        <button
          v-if="personalConnectionAllows(connection, 'REVOKE')"
          class="button button--danger"
          type="button"
          :disabled="!!busyRef || loading || requiresRefresh || unavailable"
          @click="model.execute(connection.connectionRef, 'REVOKE')"
        >
          {{ t("personalConnections.revoke") }}
        </button>
      </div>
      <form
        v-if="editingRef === connection.connectionRef"
        class="personal-editor"
        @submit.prevent="model.execute(connection.connectionRef, 'SAVE')"
      >
        <label :for="`${prefix}-key`">{{
          t(
            usesMultilineSSHCredential(connection.definitionKey)
              ? "integrations.sshPrivateKeyValue"
              : "personalConnections.key",
          )
        }}</label>
        <textarea
          v-if="usesMultilineSSHCredential(connection.definitionKey)"
          :id="`${prefix}-key`"
          v-model="credentialValue"
          name="personal-integration-key"
          rows="7"
          autocomplete="off"
          maxlength="16384"
          autocapitalize="none"
          spellcheck="false"
          required
        />
        <input
          v-else
          :id="`${prefix}-key`"
          v-model="credentialValue"
          name="personal-integration-key"
          type="password"
          autocomplete="off"
          maxlength="16384"
          spellcheck="false"
          required
        />
        <p class="muted">
          {{
            t(
              usesMultilineSSHCredential(connection.definitionKey)
                ? "integrations.sshPrivateKeyHelp"
                : "personalConnections.keyHelp",
            )
          }}
        </p>
        <div class="personal-actions">
          <button
            class="button button--primary"
            type="submit"
            :disabled="!credentialValue.trim() || !!busyRef || unavailable"
          >
            {{ t("personalConnections.save") }}
          </button>
          <button class="button" type="button" @click="model.closeEditor()">
            {{ t("personalConnections.cancel") }}
          </button>
        </div>
      </form>
    </article>
    <button
      v-if="nextPageToken"
      class="button"
      type="button"
      :disabled="loading || !!busyRef || requiresRefresh || unavailable"
      @click="model.load(true)"
    >
      {{ t("personalConnections.more") }}
    </button>
  </section>
</template>

<style scoped>
.personal-connections {
  display: grid;
  gap: 16px;
}
.personal-heading,
.personal-actions,
.personal-search {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.personal-heading {
  justify-content: space-between;
}
.personal-heading h2,
.personal-heading h3 {
  margin: 0;
}
.personal-search input {
  flex: 1;
  min-width: 180px;
}
.personal-connection {
  padding: 20px;
}
.personal-editor {
  display: grid;
  gap: 8px;
  margin-top: 16px;
}
.personal-editor input {
  width: 100%;
}
</style>
