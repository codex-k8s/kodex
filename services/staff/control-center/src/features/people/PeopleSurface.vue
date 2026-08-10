<script setup lang="ts">
import { Bot, History, Plus, RefreshCw, Trash2 } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { usePeopleStore } from "@/features/people/store";
import {
  type AgentModel,
  type AssignmentModel,
  parseCapabilities,
  type RoleDefinitionModel,
} from "@/features/people/model";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = usePeopleStore();
const { t } = useI18n();
const roleOpen = ref(false);
const agentOpen = ref(false);
const assignmentOpen = ref(false);
const historyOpen = ref(false);
const botOpen = ref(false);
const sourceOpen = ref(false);
const selectedAgent = ref<AgentModel | null>(null);
const selectedRole = ref<RoleDefinitionModel | null>(null);
const editingAgent = ref<AgentModel | null>(null);
const selectedIdentity = ref("");
const createBotIdentity = ref(false);
const botUsernameIntent = ref("");
const botDisplayName = ref("");
const roleForm = reactive({
  name: "",
  stableKey: "",
  description: "",
  capabilities: "",
  allowedTargetRoleDefinitionRefs: [] as string[],
  roleImageRecipeRef: "",
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
  store.instructionSets.data.filter((item) => item.stableKey),
);
const poolOptions = computed(() => store.pools.data);
const roomOptions = computed(() =>
  store.rooms.data.filter((item) => item.stableKey),
);

async function load(): Promise<void> {
  await store.loadPeople();
}

async function createRole(): Promise<void> {
  const capabilities = parseCapabilities(roleForm.capabilities);
  if (!capabilities) {
    window.alert(t("people.invalidCapabilities"));
    return;
  }
  const value = selectedRole.value;
  const recipe = store.roleImageRecipes.data.find(
    (item) => item.id === roleForm.roleImageRecipeRef,
  );
  const ok = await store.saveRoleDraft(value, {
    name: roleForm.name.trim(),
    stableKey: roleForm.stableKey.trim(),
    description: roleForm.description.trim(),
    capabilities,
    allowedTargetRoleDefinitionRefs: roleForm.allowedTargetRoleDefinitionRefs,
    ...(recipe?.specSha256
      ? {
          roleImageRecipeRef: recipe.id,
          roleImageRecipeVersion: recipe.version,
          roleImageRecipeSha256: recipe.specSha256,
        }
      : value?.roleImageRecipeRef &&
          value.roleImageRecipeVersion &&
          value.roleImageRecipeSha256
        ? {
            roleImageRecipeRef: value.roleImageRecipeRef,
            roleImageRecipeVersion: value.roleImageRecipeVersion,
            roleImageRecipeSha256: value.roleImageRecipeSha256,
          }
        : {}),
  });
  if (ok) roleOpen.value = false;
}

function beginRole(value?: RoleDefinitionModel): void {
  selectedRole.value = value ?? null;
  Object.assign(roleForm, {
    name: value?.name ?? "",
    stableKey: value?.stableKey ?? "",
    description: value?.description ?? "",
    capabilities: value?.capabilities.join(", ") ?? "",
    allowedTargetRoleDefinitionRefs:
      value?.allowedTargetRoleDefinitionRefs ?? [],
    roleImageRecipeRef: value?.roleImageRecipeRef ?? "",
  });
  roleOpen.value = true;
}

async function archiveRole(role: RoleDefinitionModel): Promise<void> {
  if (!window.confirm(t("people.confirmArchive", { name: role.name }))) return;
  await store.executeRoleAction(role, "ARCHIVE");
}

async function deleteRole(role: RoleDefinitionModel): Promise<void> {
  if (!window.confirm(t("people.confirmDelete", { name: role.name }))) return;
  await store.executeRoleAction(role, "DELETE");
}

async function roleAction(
  role: RoleDefinitionModel,
  action: "PAUSE" | "RESUME",
): Promise<void> {
  if (role.ownership.managedBy === "git") {
    window.alert(t("people.gitOwned"));
    return;
  }
  if (!window.confirm(t("people.confirmAction", { action, name: role.name })))
    return;
  await store.executeRoleAction(role, action);
}

async function createAgent(): Promise<void> {
  const capabilities = parseCapabilities(agentForm.capabilities);
  if (!capabilities) {
    window.alert(t("people.invalidCapabilities"));
    return;
  }
  const value = editingAgent.value;
  const ok = await store.saveAgentDraft(value, {
    name: agentForm.name.trim(),
    stableKey: agentForm.stableKey.trim(),
    runtimeSelectionKey: agentForm.runtimeSelectionKey,
    instructionSetStableKey: agentForm.instructionSetStableKey,
    providerPoolStableKey: agentForm.providerPoolStableKey,
    capabilities,
    enabled: agentForm.enabled,
  });
  if (ok) agentOpen.value = false;
}

async function beginAgent(value?: AgentModel): Promise<void> {
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

async function archiveAgent(agent: AgentModel): Promise<void> {
  if (!(await agentIsUiOwned(agent))) return;
  if (!window.confirm(t("people.confirmArchive", { name: agent.displayName })))
    return;
  await store.executeAgentAction(agent, "ARCHIVE");
}

async function deleteAgent(agent: AgentModel): Promise<void> {
  if (!(await agentIsUiOwned(agent))) return;
  if (!window.confirm(t("people.confirmDelete", { name: agent.displayName })))
    return;
  await store.executeAgentAction(agent, "DELETE");
}

async function agentAction(
  agent: AgentModel,
  action: "PAUSE" | "RESUME" | "ENABLE" | "DISABLE",
): Promise<void> {
  if (!(await agentIsUiOwned(agent))) return;
  if (
    !window.confirm(
      t("people.confirmAction", { action, name: agent.displayName }),
    )
  )
    return;
  await store.executeAgentAction(agent, action);
}

async function agentIsUiOwned(agent: AgentModel): Promise<boolean> {
  await store.loadConfigurationSource(agent.agentRef, "AGENT");
  const allowed =
    store.configurationSource.phase === "ready" &&
    store.configurationSource.data?.managedBy === "ui";
  if (!allowed) window.alert(t("people.gitOwned"));
  return allowed;
}

async function assign(): Promise<void> {
  const ok = await store.assignAgent(
    assignmentForm.name.trim(),
    assignmentForm.agentStableKey,
    assignmentForm.roomStableKey,
  );
  if (ok) assignmentOpen.value = false;
}

async function unassign(item: AssignmentModel): Promise<void> {
  if (!window.confirm(t("people.confirmUnassign", { name: item.name }))) return;
  await store.unassignAgent(item);
}

async function showRoleHistory(item: RoleDefinitionModel): Promise<void> {
  await store.loadRoleHistory(item.id);
  historyOpen.value = true;
}

async function showAgentHistory(item: AgentModel): Promise<void> {
  await store.loadAgentHistory(item.agentRef);
  historyOpen.value = true;
}

async function showAssignmentHistory(item: AssignmentModel): Promise<void> {
  await store.loadAssignmentHistory(item.id);
  historyOpen.value = true;
}

async function showSource(
  resourceRef: string,
  kind: "ROLE_DEFINITION" | "AGENT",
): Promise<void> {
  await store.loadConfigurationSource(resourceRef, kind);
  sourceOpen.value = store.configurationSource.phase === "ready";
}

function showBot(agent: AgentModel): void {
  selectedAgent.value = agent;
  selectedIdentity.value = "";
  createBotIdentity.value = false;
  botUsernameIntent.value = "";
  botDisplayName.value = agent.displayName;
  botOpen.value = true;
}

async function bindBot(): Promise<void> {
  if (!selectedAgent.value) return;
  if (createBotIdentity.value) {
    if (!botUsernameIntent.value.trim() || !botDisplayName.value.trim()) return;
    const ok = await store.createAndBindBotIdentity(
      selectedAgent.value,
      botUsernameIntent.value.trim(),
      botDisplayName.value.trim(),
    );
    if (ok) botOpen.value = false;
    return;
  }
  if (!selectedIdentity.value) return;
  const ok = await store.bindBotIdentity(
    selectedAgent.value,
    selectedIdentity.value,
  );
  if (ok) botOpen.value = false;
}

async function revokeBot(): Promise<void> {
  if (!selectedAgent.value || !window.confirm(t("people.confirmRevokeBot")))
    return;
  const ok = await store.revokeBotIdentity(selectedAgent.value);
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
                    {{ item.capabilities.join(", ") || $t("common.noValue") }}
                  </td>
                  <td>
                    <StatusBadge :state="item.ownership.managedBy" />
                    <small>
                      {{ item.ownership.source }} ·
                      {{ item.ownership.revision }} ·
                      {{ item.ownership.drift }}
                    </small>
                  </td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>
                    <div class="data-table__actions">
                      <button
                        v-if="item.ownership.managedBy !== 'git'"
                        class="button button--text"
                        type="button"
                        @click="beginRole(item)"
                      >
                        {{ $t("common.edit") }}
                      </button>
                      <button
                        v-if="item.ownership.managedBy !== 'git'"
                        class="button button--text"
                        type="button"
                        @click="deleteRole(item)"
                      >
                        {{ $t("common.delete") }}
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
                        class="button button--text"
                        type="button"
                        @click="showSource(item.id, 'ROLE_DEFINITION')"
                      >
                        {{ $t("common.source") }}</button
                      ><button
                        v-if="item.ownership.managedBy !== 'git'"
                        class="button button--text"
                        type="button"
                        @click="roleAction(item, 'PAUSE')"
                      >
                        PAUSE</button
                      ><button
                        v-if="item.ownership.managedBy !== 'git'"
                        class="button button--text"
                        type="button"
                        @click="roleAction(item, 'RESUME')"
                      >
                        RESUME</button
                      ><button
                        v-if="
                          item.ownership.managedBy !== 'git' &&
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
                        class="button button--text"
                        type="button"
                        @click="showSource(item.agentRef, 'AGENT')"
                      >
                        {{ $t("common.source") }}</button
                      ><button
                        v-for="action in [
                          'PAUSE',
                          'RESUME',
                          'ENABLE',
                          'DISABLE',
                        ] as const"
                        :key="action"
                        class="button button--text"
                        type="button"
                        @click="agentAction(item, action)"
                      >
                        {{ action }}</button
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
                      <button
                        class="button button--text"
                        type="button"
                        @click="deleteAgent(item)"
                      >
                        {{ $t("common.delete") }}
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
                        (agent) => agent.agentRef === item.agentRef,
                      )?.displayName ?? $t("common.noValue")
                    }}
                  </td>
                  <td>
                    {{
                      store.rooms.data.find((room) => room.id === item.roomRef)
                        ?.name ?? $t("common.noValue")
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
          ><input v-model="roleForm.name" required maxlength="160" /></label
        ><label class="form-field"
          ><span>{{ $t("people.stableKey") }}</span
          ><input
            v-model="roleForm.stableKey"
            required
            maxlength="160" /></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("people.description") }}</span
          ><textarea v-model="roleForm.description" maxlength="2000" /></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("people.capabilities") }}</span
          ><textarea v-model="roleForm.capabilities" maxlength="10303" /></label
        ><label class="form-field"
          ><span>{{ $t("people.roles") }}</span
          ><select v-model="roleForm.allowedTargetRoleDefinitionRefs" multiple>
            <option
              v-for="item in store.roleDefinitions.data.filter(
                (role) => role.id !== selectedRole?.id,
              )"
              :key="item.id"
              :value="item.id"
            >
              {{ item.name }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("roleImages.title") }}</span
          ><select v-model="roleForm.roleImageRecipeRef">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in store.roleImageRecipes.data"
              :key="item.id"
              :value="item.id"
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
          ><input v-model="agentForm.name" required maxlength="160" /></label
        ><label class="form-field"
          ><span>{{ $t("people.stableKey") }}</span
          ><input
            v-model="agentForm.stableKey"
            required
            maxlength="160" /></label
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
              :value="item.stableKey"
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
          ><textarea
            v-model="agentForm.capabilities"
            maxlength="10303"
          /></label
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
            maxlength="160" /></label
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
              :value="item.stableKey"
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
        <div v-if="store.botOperation.data" class="callout">
          <strong>{{ $t("workspaceTeam.operation") }}</strong>
          <span
            >{{ store.botOperation.data.action }} ·
            {{ store.botOperation.data.state }}</span
          >
        </div>
        <label class="form-field"
          ><span>{{ $t("people.createBot") }}</span
          ><input v-model="createBotIdentity" type="checkbox"
        /></label>
        <template v-if="createBotIdentity">
          <label class="form-field"
            ><span>{{ $t("people.botUsernameIntent") }}</span
            ><input v-model="botUsernameIntent" required maxlength="160"
          /></label>
          <label class="form-field"
            ><span>{{ $t("common.name") }}</span
            ><input v-model="botDisplayName" required maxlength="160"
          /></label>
        </template>
        <label v-else class="form-field"
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
            :disabled="
              store.mutating ||
              (createBotIdentity
                ? !botUsernameIntent.trim() || !botDisplayName.trim()
                : !selectedIdentity)
            "
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
      :open="sourceOpen"
      :title="$t('common.source')"
      @close="sourceOpen = false"
    >
      <dl v-if="store.configurationSource.data" class="detail-list">
        <div>
          <dt>{{ $t("common.managedBy") }}</dt>
          <dd>{{ store.configurationSource.data.managedBy }}</dd>
        </div>
        <div>
          <dt>{{ $t("common.source") }}</dt>
          <dd>{{ store.configurationSource.data.source }}</dd>
        </div>
        <div>
          <dt>{{ $t("common.revision") }}</dt>
          <dd>{{ store.configurationSource.data.sourceRevision }}</dd>
        </div>
        <div>
          <dt>Drift</dt>
          <dd>{{ store.configurationSource.data.drift }}</dd>
        </div>
        <div v-if="store.configurationSource.data.sourceSha256">
          <dt>{{ $t("common.sourceDigest") }}</dt>
          <dd>
            <code>{{ store.configurationSource.data.sourceSha256 }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ $t("common.version", { version: "" }) }}</dt>
          <dd>{{ store.configurationSource.data.version }}</dd>
        </div>
      </dl>
    </ModalDialog>

    <ModalDialog
      :open="historyOpen"
      :title="$t('people.history')"
      @close="historyOpen = false"
      ><div class="timeline">
        <article
          v-for="entry in store.history.data"
          :key="`${entry.resourceId}-${entry.resourceVersion}`"
        >
          <strong>{{ entry.resourceName }}</strong
          ><span
            >{{ entry.action }} ·
            {{ $t("common.version", { version: entry.resourceVersion }) }}</span
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
