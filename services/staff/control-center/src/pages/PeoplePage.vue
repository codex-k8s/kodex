<script setup lang="ts">
import { Bot, History, Plus, RefreshCw, Trash2 } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useOwnerControlStore } from "@/features/owner-control/store";
import type {
  AgentView,
  Resource,
} from "@/shared/api/generated/openapi/types.gen";
import { resourceOwnership } from "@/shared/lib/resources";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useOwnerControlStore();
const { t } = useI18n();
const roleOpen = ref(false);
const agentOpen = ref(false);
const assignmentOpen = ref(false);
const historyOpen = ref(false);
const botOpen = ref(false);
const selectedAgent = ref<AgentView | null>(null);
const selectedRole = ref<Resource | null>(null);
const editingAgent = ref<AgentView | null>(null);
const selectedIdentity = ref("");
const roleForm = reactive({
  name: "",
  stableKey: "",
  description: "",
  capabilities: "",
});
const agentForm = reactive({
  name: "",
  stableKey: "",
  runtimeSelectionKey: "",
  instructionSetStableKey: "",
  providerPoolStableKey: "",
  capabilities: "",
  enabled: true,
});
const assignmentForm = reactive({
  name: "",
  agentStableKey: "",
  roomStableKey: "",
});
const instructionOptions = computed(() =>
  store.instructionSets.data.filter((item) => item.spec.instructionSet),
);
const poolOptions = computed(() => store.pools.data);
const roomOptions = computed(() =>
  store.rooms.data.filter((item) => item.spec.chat?.stableKey),
);

const values = (input: string) =>
  input
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);

async function load(): Promise<void> {
  await Promise.all([
    store.loadPeople(),
    store.loadInstructions(),
    store.loadProviders(),
  ]);
}

async function createRole(): Promise<void> {
  const value = selectedRole.value;
  const ok = await store.saveRole(
    {
      action: value ? "UPDATE" : "CREATE",
      ...(value ? { resourceRef: value.id } : {}),
      name: roleForm.name.trim(),
      stableKey: roleForm.stableKey.trim(),
      description: roleForm.description.trim(),
      capabilities: values(roleForm.capabilities),
      ...(value?.spec.roleDefinition
        ? {
            allowedTargetRoleDefinitionRefs:
              value.spec.roleDefinition.allowedTargetRoleDefinitionRefs,
            roleImageRecipeRef: value.spec.roleDefinition.roleImageRecipeRef,
            roleImageRecipeVersion:
              value.spec.roleDefinition.roleImageRecipeVersion,
            roleImageRecipeSha256:
              value.spec.roleDefinition.roleImageRecipeSha256,
          }
        : {}),
    },
    value?.version,
  );
  if (ok) roleOpen.value = false;
}

function beginRole(value?: Resource): void {
  selectedRole.value = value ?? null;
  Object.assign(roleForm, {
    name: value?.name ?? "",
    stableKey: value?.spec.roleDefinition?.stableKey ?? "",
    description: value?.spec.roleDefinition?.description ?? "",
    capabilities: value?.spec.roleDefinition?.capabilities.join(", ") ?? "",
  });
  roleOpen.value = true;
}

async function archiveRole(role: Resource): Promise<void> {
  if (!window.confirm(t("people.confirmArchive", { name: role.name }))) return;
  await store.saveRole(
    { action: "ARCHIVE", resourceRef: role.id },
    role.version,
  );
}

async function createAgent(): Promise<void> {
  const value = editingAgent.value;
  const ok = await store.saveAgent(
    {
      action: value ? "UPDATE" : "CREATE",
      ...(value ? { resourceRef: value.agentRef } : {}),
      name: agentForm.name.trim(),
      stableKey: agentForm.stableKey.trim(),
      runtimeSelectionKey: agentForm.runtimeSelectionKey,
      instructionSetStableKey: agentForm.instructionSetStableKey,
      providerPoolStableKey: agentForm.providerPoolStableKey,
      capabilities: values(agentForm.capabilities),
      enabled: agentForm.enabled,
    },
    value?.version,
  );
  if (ok) agentOpen.value = false;
}

async function beginAgent(value?: AgentView): Promise<void> {
  if (value) {
    await store.loadConfigurationSource(value.agentRef, "AGENT");
    if (
      store.configurationSource.phase !== "ready" ||
      store.configurationSource.data?.managedBy === "git"
    ) {
      window.alert(t("people.gitOwned"));
      return;
    }
  }
  editingAgent.value = value ?? null;
  Object.assign(agentForm, {
    name: value?.displayName ?? "",
    stableKey: value?.stableKey ?? "",
    runtimeSelectionKey: value?.runtimeSelection.selectionKey ?? "",
    instructionSetStableKey: value?.instructionSelection.selector ?? "",
    providerPoolStableKey: value?.providerPoolSelection.selector ?? "",
    capabilities: value?.capabilities.join(", ") ?? "",
    enabled: value?.enabled ?? true,
  });
  agentOpen.value = true;
}

async function archiveAgent(agent: AgentView): Promise<void> {
  if (!window.confirm(t("people.confirmArchive", { name: agent.displayName })))
    return;
  await store.saveAgent(
    { action: "ARCHIVE", resourceRef: agent.agentRef },
    agent.version,
  );
}

async function assign(): Promise<void> {
  const ok = await store.saveAssignment({
    action: "ASSIGN",
    name: assignmentForm.name.trim(),
    agentStableKey: assignmentForm.agentStableKey,
    roomStableKey: assignmentForm.roomStableKey,
  });
  if (ok) assignmentOpen.value = false;
}

async function unassign(item: Resource): Promise<void> {
  if (!window.confirm(t("people.confirmUnassign", { name: item.name }))) return;
  await store.saveAssignment(
    { action: "UNASSIGN", resourceRef: item.id },
    item.version,
  );
}

async function showRoleHistory(item: Resource): Promise<void> {
  await store.loadRoleHistory(item.id);
  historyOpen.value = true;
}

async function showAgentHistory(item: AgentView): Promise<void> {
  await store.loadAgentHistory(item.agentRef);
  historyOpen.value = true;
}

async function showAssignmentHistory(item: Resource): Promise<void> {
  await store.loadAssignmentHistory(item.id);
  historyOpen.value = true;
}

function showBot(agent: AgentView): void {
  selectedAgent.value = agent;
  selectedIdentity.value = "";
  botOpen.value = true;
}

async function bindBot(): Promise<void> {
  if (!selectedAgent.value || !selectedIdentity.value) return;
  const action =
    selectedAgent.value.botIdentity.status === "BOUND" ? "REBIND" : "BIND";
  const ok = await store.saveBotIdentity(
    selectedAgent.value.agentRef,
    selectedAgent.value.version,
    { action, identitySelector: selectedIdentity.value },
  );
  if (ok) botOpen.value = false;
}

async function revokeBot(): Promise<void> {
  if (!selectedAgent.value || !window.confirm(t("people.confirmRevokeBot")))
    return;
  const ok = await store.saveBotIdentity(
    selectedAgent.value.agentRef,
    selectedAgent.value.version,
    {
      action: "REVOKE",
      expectedProviderGeneration:
        selectedAgent.value.botIdentity.providerGeneration,
    },
  );
  if (ok) botOpen.value = false;
}

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader :title="$t('people.title')" :subtitle="$t('people.subtitle')">
      <template #actions>
        <button class="button button--secondary" type="button" @click="load">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button>
      </template>
    </PageHeader>
    <ProblemNotice :problem="store.mutationProblem" />
    <div class="section-stack" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("people.roles") }}</h2>
          <button
            class="button button--primary"
            type="button"
            @click="beginRole()"
          >
            <Plus :size="15" aria-hidden="true" />{{ $t("people.createRole") }}
          </button>
        </header>
        <AsyncPanel
          :phase="store.roleDefinitions.phase"
          :problem="store.roleDefinitions.problem"
          @retry="load"
        >
          <div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("people.capabilities") }}</th>
                  <th>{{ $t("common.managedBy") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in store.roleDefinitions.data" :key="item.id">
                  <td class="data-table__name">{{ item.name }}</td>
                  <td>
                    {{
                      item.spec.roleDefinition?.capabilities.join(", ") ||
                      $t("common.noValue")
                    }}
                  </td>
                  <td>
                    <StatusBadge
                      :state="resourceOwnership(item)?.managedBy ?? 'ui'"
                    />
                  </td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>
                    <div class="data-table__actions">
                      <button
                        v-if="resourceOwnership(item)?.managedBy !== 'git'"
                        class="button button--text"
                        type="button"
                        @click="beginRole(item)"
                      >
                        {{ $t("common.edit") }}
                      </button>
                      <button
                        class="button button--text"
                        type="button"
                        @click="showRoleHistory(item)"
                      >
                        <History :size="14" aria-hidden="true" />{{
                          $t("people.history")
                        }}</button
                      ><button
                        v-if="
                          resourceOwnership(item)?.managedBy !== 'git' &&
                          item.state !== 'ARCHIVED'
                        "
                        class="button button--text"
                        type="button"
                        @click="archiveRole(item)"
                      >
                        <Trash2 :size="14" aria-hidden="true" />{{
                          $t("common.archive")
                        }}
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </AsyncPanel>
      </section>

      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("people.agents") }}</h2>
          <button
            class="button button--primary"
            type="button"
            @click="beginAgent()"
          >
            <Plus :size="15" aria-hidden="true" />{{ $t("people.createAgent") }}
          </button>
        </header>
        <AsyncPanel
          :phase="store.agents.phase"
          :problem="store.agents.problem"
          @retry="load"
        >
          <div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("people.runtime") }}</th>
                  <th>{{ $t("people.bot") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in store.agents.data" :key="item.agentRef">
                  <td class="data-table__name">{{ item.displayName }}</td>
                  <td>{{ item.runtimeSelection.displayName }}</td>
                  <td>
                    {{
                      item.botIdentity.username || item.botIdentity.maskedStatus
                    }}
                  </td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>
                    <div class="data-table__actions">
                      <button
                        class="button button--text"
                        type="button"
                        @click="beginAgent(item)"
                      >
                        {{ $t("common.edit") }}
                      </button>
                      <button
                        class="button button--text"
                        type="button"
                        @click="showBot(item)"
                      >
                        <Bot :size="14" aria-hidden="true" />{{
                          $t("people.bot")
                        }}</button
                      ><button
                        class="button button--text"
                        type="button"
                        @click="showAgentHistory(item)"
                      >
                        <History :size="14" aria-hidden="true" />{{
                          $t("people.history")
                        }}</button
                      ><button
                        v-if="item.state !== 'ARCHIVED'"
                        class="button button--text"
                        type="button"
                        @click="archiveAgent(item)"
                      >
                        <Trash2 :size="14" aria-hidden="true" />{{
                          $t("common.archive")
                        }}
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </AsyncPanel>
      </section>

      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("people.assignments") }}</h2>
          <button
            class="button button--primary"
            type="button"
            @click="assignmentOpen = true"
          >
            <Plus :size="15" aria-hidden="true" />{{ $t("people.assign") }}
          </button>
        </header>
        <AsyncPanel
          :phase="store.assignments.phase"
          :problem="store.assignments.problem"
          @retry="load"
        >
          <div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("people.agent") }}</th>
                  <th>{{ $t("people.room") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in store.assignments.data" :key="item.id">
                  <td class="data-table__name">{{ item.name }}</td>
                  <td>
                    {{
                      store.agents.data.find(
                        (agent) =>
                          agent.agentRef ===
                          item.spec.agentAssignment?.agentRef,
                      )?.displayName ?? $t("common.noValue")
                    }}
                  </td>
                  <td>
                    {{
                      store.rooms.data.find(
                        (room) =>
                          room.id === item.spec.agentAssignment?.roomRef,
                      )?.name ?? $t("common.noValue")
                    }}
                  </td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>
                    <button
                      class="button button--text"
                      type="button"
                      @click="showAssignmentHistory(item)"
                    >
                      <History :size="14" aria-hidden="true" />{{
                        $t("people.history")
                      }}
                    </button>
                    <button
                      class="button button--text"
                      type="button"
                      @click="unassign(item)"
                    >
                      {{ $t("people.unassign") }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </AsyncPanel>
      </section>
    </div>

    <ModalDialog
      :open="roleOpen"
      :title="selectedRole ? $t('people.editRole') : $t('people.createRole')"
      @close="roleOpen = false"
      ><form class="form-grid" @submit.prevent="createRole">
        <label class="form-field"
          ><span>{{ $t("common.name") }}</span
          ><input v-model="roleForm.name" required maxlength="120" /></label
        ><label class="form-field"
          ><span>{{ $t("people.stableKey") }}</span
          ><input v-model="roleForm.stableKey" required maxlength="80" /></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("people.description") }}</span
          ><textarea
            v-model="roleForm.description"
            required
            maxlength="1000"
          /></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("people.capabilities") }}</span
          ><input v-model="roleForm.capabilities" maxlength="1000"
        /></label>
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating"
          >
            {{ selectedRole ? $t("common.save") : $t("common.create") }}
          </button>
        </div>
      </form></ModalDialog
    >

    <ModalDialog
      :open="agentOpen"
      :title="editingAgent ? $t('people.editAgent') : $t('people.createAgent')"
      @close="agentOpen = false"
      ><form class="form-grid" @submit.prevent="createAgent">
        <label class="form-field"
          ><span>{{ $t("common.name") }}</span
          ><input v-model="agentForm.name" required maxlength="120" /></label
        ><label class="form-field"
          ><span>{{ $t("people.stableKey") }}</span
          ><input
            v-model="agentForm.stableKey"
            required
            maxlength="80" /></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("people.runtime") }}</span
          ><select v-model="agentForm.runtimeSelectionKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.ownerCatalog.data?.runtimeSelections ?? []"
              :key="item.selectionKey"
              :value="item.selectionKey"
            >
              {{ item.displayName }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("people.instructions") }}</span
          ><select v-model="agentForm.instructionSetStableKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in instructionOptions"
              :key="item.id"
              :value="item.spec.instructionSet?.stableKey"
            >
              {{ item.name }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("people.pool") }}</span
          ><select v-model="agentForm.providerPoolStableKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in poolOptions"
              :key="item.poolRef"
              :value="item.stableKey"
            >
              {{ item.displayName }}
            </option>
          </select></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("people.capabilities") }}</span
          ><input v-model="agentForm.capabilities" /></label
        ><label class="check-field form-field--full"
          ><input v-model="agentForm.enabled" type="checkbox" />{{
            $t("people.enabled")
          }}</label
        >
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating"
          >
            {{ editingAgent ? $t("common.save") : $t("common.create") }}
          </button>
        </div>
      </form></ModalDialog
    >

    <ModalDialog
      :open="assignmentOpen"
      :title="$t('people.assign')"
      @close="assignmentOpen = false"
      ><form class="form-grid" @submit.prevent="assign">
        <label class="form-field form-field--full"
          ><span>{{ $t("common.name") }}</span
          ><input
            v-model="assignmentForm.name"
            required
            maxlength="120" /></label
        ><label class="form-field"
          ><span>{{ $t("people.agent") }}</span
          ><select v-model="assignmentForm.agentStableKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.agents.data"
              :key="item.agentRef"
              :value="item.stableKey"
            >
              {{ item.displayName }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("people.room") }}</span
          ><select v-model="assignmentForm.roomStableKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in roomOptions"
              :key="item.id"
              :value="item.spec.chat?.stableKey"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating"
          >
            {{ $t("people.assign") }}
          </button>
        </div>
      </form></ModalDialog
    >

    <ModalDialog
      :open="botOpen"
      :title="$t('people.bot')"
      @close="botOpen = false"
      ><div class="section-stack">
        <label class="form-field"
          ><span>{{ $t("people.botIdentity") }}</span
          ><select v-model="selectedIdentity">
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.botIdentities.data.filter(
                (identity) => identity.status === 'AVAILABLE',
              )"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.displayName }} · {{ item.username }}
            </option>
          </select></label
        >
        <div class="button-row">
          <button
            class="button button--primary"
            type="button"
            :disabled="!selectedIdentity || store.mutating"
            @click="bindBot"
          >
            {{ $t("people.bindBot") }}</button
          ><button
            v-if="selectedAgent?.botIdentity.status === 'BOUND'"
            class="button button--danger"
            type="button"
            :disabled="store.mutating"
            @click="revokeBot"
          >
            {{ $t("people.revokeBot") }}
          </button>
        </div>
      </div></ModalDialog
    >

    <ModalDialog
      :open="historyOpen"
      :title="$t('people.history')"
      @close="historyOpen = false"
      ><div class="timeline">
        <article
          v-for="entry in store.history.data"
          :key="`${entry.resource.id}-${entry.resource.version}`"
        >
          <strong>{{ entry.resource.name }}</strong
          ><span
            >{{ entry.action }} ·
            {{
              $t("common.version", { version: entry.resource.version })
            }}</span
          >
        </article>
        <article
          v-for="entry in store.agentHistory.data"
          :key="`${entry.agentRef}-${entry.version}`"
        >
          <strong>{{ entry.displayName }}</strong
          ><span
            >{{ $t("common.version", { version: entry.version }) }} ·
            {{ entry.state }}</span
          >
        </article>
      </div></ModalDialog
    >
  </div>
</template>
