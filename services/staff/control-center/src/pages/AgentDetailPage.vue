<script setup lang="ts">
import { Play, Power, PowerOff } from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import InstructionHistory from "@/features/agents/components/InstructionHistory.vue";
import AgentAccessPanel from "@/features/agents/detail/AgentAccessPanel.vue";
import AgentApiGaps from "@/features/agents/detail/AgentApiGaps.vue";
import AgentApplyState from "@/features/agents/detail/AgentApplyState.vue";
import AgentEnvironmentPanel from "@/features/agents/detail/AgentEnvironmentPanel.vue";
import AgentInstructionsPanel from "@/features/agents/detail/AgentInstructionsPanel.vue";
import AgentProfilePanel from "@/features/agents/detail/AgentProfilePanel.vue";
import AgentRuntimePanel from "@/features/agents/detail/AgentRuntimePanel.vue";
import {
  sameProfileDraft,
  type AgentDetailTab,
  type AgentProfileDraft,
  type ApplyBoundary,
} from "@/features/agents/detail/model";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const agentRef = computed(() => String(route.params.agentRef));
const projectRef = computed(() => String(route.params.projectRef));
const agent = computed(() => platform.agents[agentRef.value]);
const runtimes = computed(() => Object.values(platform.runtimes));
const canEdit = computed(
  () => agent.value?.nextActions.includes("EDIT") ?? false,
);
const canManageCapabilities = computed(
  () => agent.value?.nextActions.includes("MANAGE_CAPABILITIES") ?? false,
);
const capabilityCatalog = computed(() => {
  const values = [...platform.capabilities].sort(
    (left, right) =>
      left.category.localeCompare(right.category) ||
      left.name.localeCompare(right.name),
  );
  return canManageCapabilities.value
    ? values
    : values.filter((item) => hasCapability(item.key));
});
const roleImageRecipe = computed(() =>
  Object.values(platform.roleImageRecipes).find(
    (item) =>
      item.projectRef === projectRef.value &&
      item.roleDefinitionRef === agent.value?.roleDefinitionRef,
  ),
);
const roleEnvironments = computed(() =>
  Object.values(platform.roleEnvironments).sort(
    (left, right) =>
      Number(right.recommended) - Number(left.recommended) ||
      left.key.localeCompare(right.key),
  ),
);
const latestBuild = computed(() => {
  const recipe = roleImageRecipe.value;
  if (!recipe) return undefined;
  return Object.values(platform.roleImageBuilds)
    .filter((item) => item.recipeRef === recipe.ref)
    .sort((left, right) => right.updatedAt.localeCompare(left.updatedAt))[0];
});
const instructionHistory = computed(
  () => platform.instructionVersions[agentRef.value] ?? [],
);
const instructionState = computed(
  () =>
    agent.value?.draftInstructions?.state ??
    agent.value?.publishedInstructions?.state ??
    "DRAFT",
);
const instructionValidationMessages = computed(
  () => agent.value?.draftInstructions?.validationMessages ?? [],
);

const activeTab = ref<AgentDetailTab>("profile");
const profileDraft = ref<AgentProfileDraft>({
  name: "",
  purpose: "",
  roleDescription: "",
  avatarUrl: "",
});
const runtimeRef = ref("");
const instructions = ref("");
const selectedEnvironment = ref("");
const task = ref("");
const busy = ref(false);
const capabilityBusy = ref("");
const problem = ref<AppProblem>();
const applyState = ref<"APPLIED" | "DRAFT" | "RUNNING" | "FAILED">("APPLIED");
const applyScope = ref(t("agents.profile"));
const applyBoundary = ref<ApplyBoundary>("next-run");

const currentProfile = computed<AgentProfileDraft>(() => ({
  name: agent.value?.name ?? "",
  purpose: agent.value?.purpose ?? "",
  roleDescription: agent.value?.roleDescription ?? "",
  avatarUrl: agent.value?.avatarUrl ?? "",
}));
const profileDirty = computed(
  () => !sameProfileDraft(profileDraft.value, currentProfile.value),
);
const runtimeDirty = computed(
  () => Boolean(agent.value) && runtimeRef.value !== agent.value?.runtimeRef,
);
const authoritativeInstructions = computed(
  () =>
    agent.value?.draftInstructions?.content ??
    agent.value?.publishedInstructions?.content ??
    "",
);
const instructionsDirty = computed(
  () => instructions.value !== authoritativeInstructions.value,
);

const tabs = computed<Array<{ id: AgentDetailTab; label: string }>>(() => [
  { id: "profile", label: t("agents.profile") },
  { id: "instructions", label: t("agents.instructions") },
  { id: "runtime", label: "Runtime" },
  { id: "environment", label: t("roleEnvironments.title") },
  { id: "access", label: t("agents.capabilities") },
]);

function hasCapability(key: string): boolean {
  return agent.value?.capabilities.some((item) => item.key === key) ?? false;
}

function tabScope(tab: AgentDetailTab): string {
  return tabs.value.find((item) => item.id === tab)?.label ?? tab;
}

function tabBoundary(tab: AgentDetailTab): ApplyBoundary {
  if (tab === "instructions") return "published";
  if (tab === "runtime" || tab === "environment") return "next-turn";
  return "next-run";
}

function tabHasDraft(tab: AgentDetailTab): boolean {
  if (tab === "profile") return profileDirty.value;
  if (tab === "runtime") return runtimeDirty.value;
  if (tab === "instructions") return instructionsDirty.value;
  if (tab === "environment")
    return (
      selectedEnvironment.value !==
      (roleImageRecipe.value?.environment.environmentKey ?? "")
    );
  return false;
}

function selectTab(tab: AgentDetailTab): void {
  activeTab.value = tab;
  applyScope.value = tabScope(tab);
  applyBoundary.value = tabBoundary(tab);
  applyState.value = tabHasDraft(tab) ? "DRAFT" : "APPLIED";
}

function markDraft(scope: string, boundary: ApplyBoundary): void {
  applyState.value = "DRAFT";
  applyScope.value = scope;
  applyBoundary.value = boundary;
}

function markApplying(scope: string, boundary: ApplyBoundary): void {
  applyState.value = "RUNNING";
  applyScope.value = scope;
  applyBoundary.value = boundary;
}

function markApplied(): void {
  applyState.value = "APPLIED";
}

function markCurrent(scope: string, boundary: ApplyBoundary): void {
  applyScope.value = scope;
  applyBoundary.value = boundary;
  markApplied();
}

function markFailed(): void {
  applyState.value = "FAILED";
}

function syncProfile(value = agent.value): void {
  if (!value) return;
  profileDraft.value = {
    name: value.name,
    purpose: value.purpose,
    roleDescription: value.roleDescription,
    avatarUrl: value.avatarUrl ?? "",
  };
}

function syncRuntime(value = agent.value): void {
  if (value) runtimeRef.value = value.runtimeRef;
}

function syncInstructions(): void {
  instructions.value = authoritativeInstructions.value;
}

async function load(): Promise<void> {
  await Promise.all([
    platform.loadAgent(agentRef.value),
    platform.loadInstructionVersions(agentRef.value),
    platform.loadRuntimes(),
    platform.loadCapabilities(),
  ]);
  syncProfile();
  syncRuntime();
  syncInstructions();

  await Promise.all([
    platform.loadRoleEnvironments(),
    platform.loadRoleImageRecipes(
      projectRef.value,
      agent.value?.roleDefinitionRef,
    ),
  ]);
  if (roleImageRecipe.value) {
    selectedEnvironment.value =
      roleImageRecipe.value.environment.environmentKey;
    await platform.loadRoleImageRecipe(
      projectRef.value,
      roleImageRecipe.value.ref,
    );
  } else {
    selectedEnvironment.value =
      roleEnvironments.value.find((item) => item.recommended && item.available)
        ?.key ??
      roleEnvironments.value.find((item) => item.available)?.key ??
      "";
  }
}

function updateProfile(value: AgentProfileDraft): void {
  profileDraft.value = value;
  if (sameProfileDraft(value, currentProfile.value))
    markCurrent(tabScope("profile"), "next-run");
  else markDraft(tabScope("profile"), "next-run");
}

function updateRuntime(value: string): void {
  runtimeRef.value = value;
  if (value === agent.value?.runtimeRef)
    markCurrent(tabScope("runtime"), "next-turn");
  else markDraft(tabScope("runtime"), "next-turn");
}

function updateInstructions(value: string): void {
  instructions.value = value;
  if (value === authoritativeInstructions.value)
    markCurrent(tabScope("instructions"), "published");
  else markDraft(tabScope("instructions"), "published");
}

function updateEnvironment(value: string): void {
  selectedEnvironment.value = value;
  if (value === roleImageRecipe.value?.environment.environmentKey)
    markCurrent(tabScope("environment"), "next-turn");
  else markDraft(tabScope("environment"), "next-turn");
}

async function saveProfile(): Promise<void> {
  if (!agent.value || !canEdit.value || !profileDirty.value) return;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("profile"), "next-run");
  try {
    const updated = await platform.saveAgent(
      projectRef.value,
      {
        name: profileDraft.value.name.trim(),
        purpose: profileDraft.value.purpose.trim(),
        roleDescription: profileDraft.value.roleDescription.trim(),
        roleDefinitionRef: agent.value.roleDefinitionRef,
        avatarUrl: profileDraft.value.avatarUrl.trim() || undefined,
        runtimeRef: agent.value.runtimeRef,
      },
      agent.value,
    );
    syncProfile(updated);
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    busy.value = false;
  }
}

async function saveRuntime(): Promise<void> {
  if (!agent.value || !canEdit.value || !runtimeDirty.value) return;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("runtime"), "next-turn");
  try {
    const updated = await platform.saveAgent(
      projectRef.value,
      {
        name: agent.value.name,
        purpose: agent.value.purpose,
        roleDescription: agent.value.roleDescription,
        roleDefinitionRef: agent.value.roleDefinitionRef,
        avatarUrl: agent.value.avatarUrl,
        runtimeRef: runtimeRef.value,
      },
      agent.value,
    );
    syncRuntime(updated);
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    busy.value = false;
  }
}

async function saveInstructions(): Promise<void> {
  if (
    !agent.value?.nextActions.includes("EDIT") ||
    !instructionsDirty.value ||
    !instructions.value.trim()
  )
    return;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("instructions"), "published");
  try {
    const updated = await platform.saveInstructions(
      agent.value,
      instructions.value,
    );
    instructions.value =
      updated.draftInstructions?.content ??
      updated.publishedInstructions?.content ??
      "";
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    busy.value = false;
  }
}

async function instructionAction(
  action: "VALIDATE" | "PUBLISH",
): Promise<void> {
  if (!agent.value?.nextActions.includes(action) || instructionsDirty.value)
    return;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("instructions"), "published");
  try {
    const updated = await platform.instructionCommand(agent.value, action);
    instructions.value =
      updated.draftInstructions?.content ??
      updated.publishedInstructions?.content ??
      "";
    await platform.loadInstructionVersions(agentRef.value);
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    busy.value = false;
  }
}

async function rollbackInstructions(
  publishedInstructionRef: string,
): Promise<void> {
  if (!agent.value?.nextActions.includes("ROLLBACK")) return;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("instructions"), "published");
  try {
    const updated = await platform.instructionCommand(
      agent.value,
      "ROLLBACK",
      publishedInstructionRef,
    );
    instructions.value = updated.publishedInstructions?.content ?? "";
    await platform.loadInstructionVersions(agentRef.value);
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    busy.value = false;
  }
}

async function prepareRoleEnvironment(): Promise<void> {
  if (
    !agent.value?.roleDefinitionRef ||
    !canEdit.value ||
    !selectedEnvironment.value ||
    !platform.roleEnvironments[selectedEnvironment.value]?.available
  )
    return;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("environment"), "next-turn");
  try {
    await platform.saveRoleImageRecipe(
      projectRef.value,
      agent.value.roleDefinitionRef,
      t("roleEnvironments.recipeName", { name: agent.value.name }),
      { environmentKey: selectedEnvironment.value },
      roleImageRecipe.value,
    );
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    busy.value = false;
  }
}

async function changeRoleEnvironment(action: "ARCHIVE" | "RESTORE") {
  if (!roleImageRecipe.value?.nextActions.includes(action)) return;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("environment"), "next-turn");
  try {
    await platform.changeRoleImageRecipe(
      projectRef.value,
      roleImageRecipe.value,
      action,
    );
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    busy.value = false;
  }
}

async function toggleCapability(key: string): Promise<void> {
  if (!agent.value || !canManageCapabilities.value || capabilityBusy.value)
    return;
  capabilityBusy.value = key;
  problem.value = undefined;
  markApplying(tabScope("access"), "next-run");
  try {
    await platform.changeAgent(agent.value, {
      action: hasCapability(key) ? "REVOKE_CAPABILITY" : "GRANT_CAPABILITY",
      capabilityKey: key,
    });
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    capabilityBusy.value = "";
  }
}

async function launch(): Promise<void> {
  if (!agent.value?.nextActions.includes("LAUNCH") || !task.value.trim())
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    const run = await platform.launch({
      projectRef: projectRef.value,
      targetRef: agent.value.ref,
      targetType: "AGENT",
      title: task.value.trim().slice(0, 160),
      task: task.value.trim(),
    });
    await router.push(runPath(run.ref, projectRef.value));
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function toggle(): Promise<void> {
  if (
    !agent.value?.nextActions.includes(
      agent.value.enabled ? "DISABLE" : "ENABLE",
    )
  )
    return;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("profile"), "next-run");
  try {
    await platform.changeAgent(agent.value, {
      action: agent.value.enabled ? "DISABLE" : "ENABLE",
    });
    markApplied();
  } catch (error) {
    problem.value = asProblem(error);
    markFailed();
  } finally {
    busy.value = false;
  }
}

onMounted(() => void load());
</script>

<template>
  <PageFrame
    :title="agent?.name ?? $t('agents.title')"
    :subtitle="agent?.purpose"
    :eyebrow="$t('nav.agent')"
  >
    <template #actions>
      <StatusBadge v-if="agent" :state="agent.state" />
      <button
        v-if="agent?.nextActions.includes(agent.enabled ? 'DISABLE' : 'ENABLE')"
        class="button"
        type="button"
        :disabled="busy"
        @click="toggle"
      >
        <PowerOff v-if="agent.enabled" :size="16" aria-hidden="true" />
        <Power v-else :size="16" aria-hidden="true" />
        {{ agent.enabled ? $t("common.disable") : $t("common.enable") }}
      </button>
    </template>

    <AsyncState
      :loading="platform.loading.agent"
      :problem="platform.problems.agent"
      @retry="load"
    >
      <div v-if="agent" class="agent-detail-page">
        <AgentApplyState
          :state="applyState"
          :scope="applyScope"
          :boundary="applyBoundary"
        />

        <div class="agent-tabs" role="tablist" :aria-label="$t('nav.agent')">
          <button
            v-for="tab in tabs"
            :id="`agent-tab-${tab.id}`"
            :key="tab.id"
            class="agent-tab"
            type="button"
            role="tab"
            :aria-selected="activeTab === tab.id"
            :aria-controls="`agent-panel-${tab.id}`"
            @click="selectTab(tab.id)"
          >
            {{ tab.label }}
            <span v-if="tab.id === 'instructions' && agent.draftInstructions">
              {{ $t("states." + agent.draftInstructions.state) }}
            </span>
          </button>
        </div>

        <section
          v-if="activeTab === 'profile'"
          id="agent-panel-profile"
          class="agent-panel agent-profile-layout"
          role="tabpanel"
          aria-labelledby="agent-tab-profile"
        >
          <AgentProfilePanel
            :model-value="profileDraft"
            :role-name="agent.roleDefinitionName ?? agent.name"
            :can-edit="canEdit"
            :busy="busy"
            :dirty="profileDirty"
            @update:model-value="updateProfile"
            @save="saveProfile"
          />
          <aside class="agent-profile-aside">
            <section
              v-if="agent.nextActions.includes('LAUNCH')"
              class="panel launch-panel"
            >
              <h2>{{ $t("runs.new") }}</h2>
              <label class="field">
                <span>{{ $t("runs.task") }}</span>
                <textarea v-model="task" required maxlength="8000" />
              </label>
              <button
                class="button button--primary"
                type="button"
                :disabled="busy || !task.trim()"
                @click="launch"
              >
                <Play :size="16" aria-hidden="true" />{{ $t("common.launch") }}
              </button>
            </section>
            <section class="panel agent-summary">
              <h2>{{ $t("common.details") }}</h2>
              <dl>
                <div>
                  <dt>{{ $t("agents.runtime") }}</dt>
                  <dd>{{ agent.runtimeName }}</dd>
                </div>
                <div>
                  <dt>{{ $t("agents.provider") }}</dt>
                  <dd>{{ agent.runtimeProvider ?? $t("common.noData") }}</dd>
                </div>
                <div>
                  <dt>{{ $t("agents.model") }}</dt>
                  <dd class="mono">
                    {{ agent.runtimeModel ?? $t("common.noData") }}
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("agents.instructions") }}</dt>
                  <dd>
                    {{
                      agent.publishedInstructions
                        ? $t("agents.revision", {
                            revision: agent.publishedInstructions.revision,
                          })
                        : $t("common.noData")
                    }}
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("agents.capabilities") }}</dt>
                  <dd>{{ agent.capabilities.length }}</dd>
                </div>
              </dl>
            </section>
          </aside>
        </section>

        <section
          v-else-if="activeTab === 'instructions'"
          id="agent-panel-instructions"
          class="agent-panel"
          role="tabpanel"
          aria-labelledby="agent-tab-instructions"
        >
          <AgentInstructionsPanel
            :model-value="instructions"
            :state="instructionState"
            :validation-messages="instructionValidationMessages"
            :can-edit="canEdit"
            :can-validate="agent.nextActions.includes('VALIDATE')"
            :can-publish="agent.nextActions.includes('PUBLISH')"
            :busy="busy"
            :dirty="instructionsDirty"
            @update:model-value="updateInstructions"
            @save="saveInstructions"
            @validate="instructionAction('VALIDATE')"
            @publish="instructionAction('PUBLISH')"
          >
            <template #history>
              <InstructionHistory
                :versions="instructionHistory"
                :current-ref="agent.publishedInstructions?.ref"
                :can-rollback="agent.nextActions.includes('ROLLBACK')"
                :busy="busy"
                @rollback="rollbackInstructions"
              />
            </template>
          </AgentInstructionsPanel>
          <ProblemNotice
            v-if="platform.problems.instructionVersions"
            :problem="platform.problems.instructionVersions"
            compact
          />
        </section>

        <section
          v-else-if="activeTab === 'runtime'"
          id="agent-panel-runtime"
          class="agent-panel"
          role="tabpanel"
          aria-labelledby="agent-tab-runtime"
        >
          <AgentRuntimePanel
            :model-value="runtimeRef"
            :runtimes="runtimes"
            :can-edit="canEdit"
            :busy="busy"
            :dirty="runtimeDirty"
            @update:model-value="updateRuntime"
            @save="saveRuntime"
          />
          <ProblemNotice
            v-if="platform.problems.runtimes"
            :problem="platform.problems.runtimes"
            compact
          />
        </section>

        <section
          v-else-if="activeTab === 'environment'"
          id="agent-panel-environment"
          class="agent-panel"
          role="tabpanel"
          aria-labelledby="agent-tab-environment"
        >
          <AgentEnvironmentPanel
            :model-value="selectedEnvironment"
            :environments="roleEnvironments"
            :recipe="roleImageRecipe"
            :latest-build="latestBuild"
            :can-edit="canEdit && Boolean(agent.roleDefinitionRef)"
            :busy="busy"
            @update:model-value="updateEnvironment"
            @save="prepareRoleEnvironment"
            @archive="changeRoleEnvironment('ARCHIVE')"
            @restore="changeRoleEnvironment('RESTORE')"
          />
          <p
            v-if="!agent.roleDefinitionRef"
            class="agent-role-unavailable"
            role="status"
          >
            {{ $t("roleEnvironments.roleUnavailable") }}
          </p>
          <ProblemNotice
            v-if="platform.problems.roleEnvironments"
            :problem="platform.problems.roleEnvironments"
            compact
          />
          <ProblemNotice
            v-if="platform.problems.roleImages"
            :problem="platform.problems.roleImages"
            compact
          />
        </section>

        <section
          v-else
          id="agent-panel-access"
          class="agent-panel"
          role="tabpanel"
          aria-labelledby="agent-tab-access"
        >
          <AgentAccessPanel
            :capabilities="capabilityCatalog"
            :granted-keys="agent.capabilities.map((item) => item.key)"
            :integrations="agent.integrations"
            :knowledge-count="agent.knowledgeArtifactRefs.length"
            :can-manage="canManageCapabilities"
            :busy-key="capabilityBusy"
            @toggle="toggleCapability"
          />
          <ProblemNotice
            v-if="platform.problems.capabilities"
            :problem="platform.problems.capabilities"
            compact
          />
        </section>

        <ProblemNotice v-if="problem" :problem="problem" compact />
        <AgentApiGaps />
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.agent-detail-page {
  display: grid;
  gap: 16px;
}
.agent-tabs {
  display: flex;
  min-width: 0;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  scrollbar-width: thin;
}
.agent-tab {
  display: inline-flex;
  min-height: 42px;
  flex: 0 0 auto;
  align-items: center;
  gap: 7px;
  padding: 7px 13px;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--muted);
  background: transparent;
  cursor: pointer;
  font-weight: 600;
}
.agent-tab:hover,
.agent-tab:focus-visible {
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.agent-tab[aria-selected="true"] {
  border-bottom-color: var(--accent);
  color: var(--accent-strong);
}
.agent-tab span {
  padding: 2px 5px;
  border-radius: 4px;
  color: var(--warning);
  background: var(--warning-soft);
  font-size: 0.68rem;
  font-weight: 500;
}
.agent-panel {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.agent-profile-layout {
  grid-template-columns: minmax(0, 1fr) minmax(280px, 0.34fr);
  align-items: start;
}
.agent-profile-aside {
  display: grid;
  gap: 16px;
}
.launch-panel,
.agent-summary {
  display: grid;
  gap: 12px;
}
.launch-panel h2,
.agent-summary h2 {
  margin: 0;
  font-size: 1rem;
}
.launch-panel textarea {
  min-height: 150px;
}
.agent-summary dl {
  display: grid;
  gap: 0;
  margin: 0;
}
.agent-summary dl div {
  display: grid;
  grid-template-columns: minmax(110px, 0.8fr) minmax(0, 1.2fr);
  gap: 10px;
  padding: 8px 0;
  border-top: 1px solid var(--hairline);
}
.agent-summary dt {
  color: var(--subtle);
}
.agent-summary dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.agent-role-unavailable {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--warning);
  background: var(--warning-soft);
}
@media (max-width: 940px) {
  .agent-profile-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 640px) {
  .agent-tab {
    min-height: 40px;
    padding-inline: 10px;
  }
}
</style>
