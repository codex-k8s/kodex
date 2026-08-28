<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import InstructionHistory from "@/features/agents/components/InstructionHistory.vue";
import AgentRuntimePanel from "@/features/runtime/components/AgentRuntimePanel.vue";
import { asProblem, type AppProblem } from "@/shared/api/problem";
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
const canManageCapabilities = computed(() =>
  agent.value?.nextActions.includes("MANAGE_CAPABILITIES"),
);
const canEdit = computed(
  () => agent.value?.nextActions.includes("EDIT") ?? false,
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
const selectedEnvironment = ref("");
const latestBuild = computed(() => {
  const recipe = roleImageRecipe.value;
  if (!recipe) return undefined;
  return Object.values(platform.roleImageBuilds)
    .filter((item) => item.recipeRef === recipe.ref)
    .sort((left, right) => right.updatedAt.localeCompare(left.updatedAt))[0];
});
const instructions = ref("");
const instructionHistory = computed(
  () => platform.instructionVersions[agentRef.value] ?? [],
);
const task = ref("");
const busy = ref(false);
const editingProfile = ref(false);
const capabilityBusy = ref("");
const problem = ref<AppProblem>();
const profile = reactive({
  name: "",
  purpose: "",
  roleDescription: "",
  avatarUrl: "",
});

function syncProfile(): void {
  if (!agent.value) return;
  profile.name = agent.value.name;
  profile.purpose = agent.value.purpose;
  profile.roleDescription = agent.value.roleDescription;
  profile.avatarUrl = agent.value.avatarUrl ?? "";
}

function hasCapability(key: string): boolean {
  return agent.value?.capabilities.some((item) => item.key === key) ?? false;
}

async function load() {
  await Promise.all([
    platform.loadAgent(agentRef.value),
    platform.loadInstructionVersions(agentRef.value),
    platform.loadCapabilities(),
  ]);
  syncProfile();
  instructions.value =
    agent.value?.draftInstructions?.content ??
    agent.value?.publishedInstructions?.content ??
    "";
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
        ?.key ?? "";
  }
}

async function saveProfile(): Promise<void> {
  if (!agent.value || !canEdit.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.saveAgent(
      projectRef.value,
      {
        name: profile.name,
        purpose: profile.purpose,
        roleDescription: profile.roleDescription,
        roleDefinitionRef: agent.value.roleDefinitionRef,
        avatarUrl: profile.avatarUrl || undefined,
      },
      agent.value,
    );
    syncProfile();
    editingProfile.value = false;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

function cancelProfile(): void {
  syncProfile();
  editingProfile.value = false;
}

async function toggleCapability(key: string): Promise<void> {
  if (!agent.value || !canManageCapabilities.value || capabilityBusy.value)
    return;
  capabilityBusy.value = key;
  problem.value = undefined;
  try {
    await platform.changeAgent(agent.value, {
      action: hasCapability(key) ? "REVOKE_CAPABILITY" : "GRANT_CAPABILITY",
      capabilityKey: key,
    });
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    capabilityBusy.value = "";
  }
}

async function prepareRoleEnvironment() {
  if (
    !agent.value?.roleDefinitionRef ||
    !canEdit.value ||
    !selectedEnvironment.value ||
    !platform.roleEnvironments[selectedEnvironment.value]?.available
  )
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.saveRoleImageRecipe(
      projectRef.value,
      agent.value.roleDefinitionRef,
      t("roleEnvironments.recipeName", { name: agent.value.name }),
      { environmentKey: selectedEnvironment.value },
      roleImageRecipe.value,
    );
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function changeRoleEnvironment(action: "ARCHIVE" | "RESTORE") {
  if (!roleImageRecipe.value?.nextActions.includes(action)) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.changeRoleImageRecipe(
      projectRef.value,
      roleImageRecipe.value,
      action,
    );
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function saveInstructions() {
  if (!agent.value?.nextActions.includes("EDIT")) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.saveInstructions(agent.value, instructions.value);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function instructionAction(action: "VALIDATE" | "PUBLISH" | "ROLLBACK") {
  if (!agent.value?.nextActions.includes(action)) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const updated = await platform.instructionCommand(agent.value, action);
    instructions.value =
      updated.draftInstructions?.content ??
      updated.publishedInstructions?.content ??
      "";
    await platform.loadInstructionVersions(agentRef.value);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function rollbackInstructions(publishedInstructionRef: string) {
  if (!agent.value?.nextActions.includes("ROLLBACK")) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const updated = await platform.instructionCommand(
      agent.value,
      "ROLLBACK",
      publishedInstructionRef,
    );
    instructions.value = updated.publishedInstructions?.content ?? "";
    await platform.loadInstructionVersions(agentRef.value);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function launch() {
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
    await router.push(`/runs/${run.ref}`);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function toggle() {
  if (
    !agent.value?.nextActions.includes(
      agent.value.enabled ? "DISABLE" : "ENABLE",
    )
  )
    return;
  busy.value = true;
  try {
    await platform.changeAgent(agent.value, {
      action: agent.value.enabled ? "DISABLE" : "ENABLE",
    });
  } catch (error) {
    problem.value = asProblem(error);
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
  >
    <template #actions
      ><StatusBadge v-if="agent" :state="agent.state" /><button
        v-if="agent?.nextActions.includes(agent.enabled ? 'DISABLE' : 'ENABLE')"
        class="button"
        type="button"
        :disabled="busy"
        @click="toggle"
      >
        {{ agent.enabled ? $t("common.disable") : $t("common.enable") }}
      </button></template
    >
    <AsyncState
      :loading="platform.loading.agent"
      :problem="platform.problems.agent"
      @retry="load"
    >
      <div v-if="agent" class="detail-layout">
        <section class="detail-main">
          <article class="panel">
            <div class="section-header">
              <h2>{{ $t("agents.profile") }}</h2>
              <button
                v-if="!editingProfile && agent.nextActions.includes('EDIT')"
                class="button"
                type="button"
                :disabled="busy"
                @click="editingProfile = true"
              >
                {{ $t("common.edit") }}
              </button>
            </div>
            <form
              v-if="editingProfile"
              class="form-grid profile-form"
              @submit.prevent="saveProfile"
            >
              <label class="field"
                ><span>{{ $t("common.name") }}</span
                ><input v-model.trim="profile.name" required maxlength="120"
              /></label>
              <label class="field"
                ><span>{{ $t("common.purpose") }}</span
                ><input
                  v-model.trim="profile.purpose"
                  required
                  maxlength="1000"
              /></label>
              <label class="field field--wide"
                ><span>{{ $t("agents.role") }}</span
                ><textarea
                  v-model.trim="profile.roleDescription"
                  required
                  maxlength="1000"
                />
              </label>
              <label class="field"
                ><span>{{ $t("agents.avatar") }}</span
                ><input
                  v-model.trim="profile.avatarUrl"
                  type="url"
                  maxlength="500"
              /></label>
              <div class="inline-actions field--wide">
                <button
                  class="button"
                  type="button"
                  :disabled="busy"
                  @click="cancelProfile"
                >
                  {{ $t("common.cancel") }}
                </button>
                <button
                  class="button button--primary"
                  type="submit"
                  :disabled="busy"
                >
                  {{ $t("common.save") }}
                </button>
              </div>
            </form>
            <template v-else>
              <h3>{{ agent.roleDefinitionName ?? agent.name }}</h3>
              <p>{{ agent.roleDescription }}</p>
              <dl class="metadata">
                <div>
                  <dt>{{ $t("agents.runtime") }}</dt>
                  <dd>{{ agent.runtimeName }}</dd>
                </div>
                <div>
                  <dt>
                    {{ $t("common.version", { version: agent.version }) }}
                  </dt>
                  <dd>{{ new Date(agent.updatedAt).toLocaleString() }}</dd>
                </div>
              </dl>
            </template>
            <ProblemNotice
              v-if="platform.problems.runtimes"
              :problem="platform.problems.runtimes"
              compact
            />
          </article>
          <AgentRuntimePanel
            :agent="agent"
            :project-ref="projectRef"
            :can-edit="canEdit"
          />
          <article class="panel">
            <div class="section-header">
              <h2>{{ $t("agents.instructions") }}</h2>
              <StatusBadge
                :state="
                  agent.draftInstructions?.state ??
                  agent.publishedInstructions?.state ??
                  'DRAFT'
                "
              />
            </div>
            <textarea
              v-model="instructions"
              maxlength="65536"
              :readonly="!canEdit"
            />
            <div class="inline-actions">
              <button
                v-if="agent.nextActions.includes('EDIT')"
                class="button"
                type="button"
                :disabled="busy"
                @click="saveInstructions"
              >
                {{ $t("common.save") }}</button
              ><button
                v-if="agent.nextActions.includes('VALIDATE')"
                class="button"
                type="button"
                :disabled="busy"
                @click="instructionAction('VALIDATE')"
              >
                {{ $t("agents.validate") }}</button
              ><button
                v-if="agent.nextActions.includes('PUBLISH')"
                class="button button--primary"
                type="button"
                :disabled="busy"
                @click="instructionAction('PUBLISH')"
              >
                {{ $t("agents.publish") }}
              </button>
            </div>
            <InstructionHistory
              :versions="instructionHistory"
              :current-ref="agent.publishedInstructions?.ref"
              :can-rollback="agent.nextActions.includes('ROLLBACK')"
              :busy="busy"
              @rollback="rollbackInstructions"
            />
            <ProblemNotice
              v-if="platform.problems.instructionVersions"
              :problem="platform.problems.instructionVersions"
              compact
            />
          </article>
          <article class="panel role-environment-panel">
            <div class="section-header">
              <div>
                <h2>{{ $t("roleEnvironments.title") }}</h2>
                <p>{{ $t("roleEnvironments.description") }}</p>
              </div>
              <StatusBadge v-if="latestBuild" :state="latestBuild.stage" />
              <StatusBadge
                v-else-if="roleImageRecipe?.promotedImageReady"
                state="READY"
              />
            </div>
            <fieldset
              class="environment-options"
              :disabled="busy || !agent.roleDefinitionRef || !canEdit"
            >
              <legend class="sr-only">
                {{ $t("roleEnvironments.choose") }}
              </legend>
              <label
                v-for="environment in roleEnvironments"
                :key="environment.key"
                class="environment-option"
                :class="{
                  'environment-option--selected':
                    selectedEnvironment === environment.key,
                  'environment-option--unavailable': !environment.available,
                }"
              >
                <input
                  v-model="selectedEnvironment"
                  type="radio"
                  name="role-environment"
                  :value="environment.key"
                  :disabled="!environment.available"
                />
                <span>
                  <strong>{{ $t(environment.nameMessageKey) }}</strong>
                  <small v-if="environment.recommended">
                    {{ $t("roleEnvironments.recommended") }}
                  </small>
                  <span>{{ $t(environment.descriptionMessageKey) }}</span>
                  <span class="environment-software">
                    {{
                      environment.softwareMessageKeys
                        .map((key) => $t(key))
                        .join(" · ")
                    }}
                  </span>
                  <span v-if="!environment.available">
                    {{
                      $t(
                        environment.unavailableMessageKey ??
                          "roleEnvironments.unavailable",
                      )
                    }}
                  </span>
                </span>
              </label>
            </fieldset>
            <p v-if="!agent.roleDefinitionRef" role="status">
              {{ $t("roleEnvironments.roleUnavailable") }}
            </p>
            <p v-if="latestBuild" class="build-progress" aria-live="polite">
              {{
                $t("roleEnvironments.buildProgress", {
                  progress: latestBuild.progressPercent,
                })
              }}
            </p>
            <div class="inline-actions">
              <button
                v-if="canEdit"
                class="button button--primary"
                type="button"
                :disabled="
                  busy ||
                  !agent.roleDefinitionRef ||
                  !selectedEnvironment ||
                  !platform.roleEnvironments[selectedEnvironment]?.available
                "
                @click="prepareRoleEnvironment"
              >
                {{ $t("roleEnvironments.prepare") }}
              </button>
              <button
                v-if="roleImageRecipe?.nextActions.includes('ARCHIVE')"
                class="button"
                type="button"
                :disabled="busy"
                @click="changeRoleEnvironment('ARCHIVE')"
              >
                {{ $t("common.archive") }}
              </button>
              <button
                v-if="roleImageRecipe?.nextActions.includes('RESTORE')"
                class="button"
                type="button"
                :disabled="busy"
                @click="changeRoleEnvironment('RESTORE')"
              >
                {{ $t("roleEnvironments.restore") }}
              </button>
            </div>
            <details v-if="roleImageRecipe || latestBuild">
              <summary>{{ $t("common.advanced") }}</summary>
              <dl class="metadata">
                <div v-if="roleImageRecipe">
                  <dt>
                    {{
                      $t("common.version", {
                        version: roleImageRecipe.version,
                      })
                    }}
                  </dt>
                  <dd>{{ $t("states." + roleImageRecipe.state) }}</dd>
                </div>
                <div v-if="latestBuild">
                  <dt>{{ $t("roleEnvironments.lastBuild") }}</dt>
                  <dd>{{ $t("states." + latestBuild.stage) }}</dd>
                </div>
              </dl>
            </details>
            <ProblemNotice
              v-if="platform.problems.roleImages"
              :problem="platform.problems.roleImages"
              compact
            />
          </article>
          <ProblemNotice v-if="problem" :problem="problem" compact />
        </section>
        <aside class="detail-side">
          <section
            v-if="agent.nextActions.includes('LAUNCH')"
            class="panel launch-panel"
          >
            <h2>{{ $t("runs.new") }}</h2>
            <label class="field"
              ><span>{{ $t("runs.task") }}</span
              ><textarea v-model="task" required maxlength="8000" /></label
            ><button
              class="button button--primary"
              type="button"
              :disabled="busy || !task.trim()"
              @click="launch"
            >
              {{ $t("common.launch") }}
            </button>
          </section>
          <section class="panel">
            <div class="section-header">
              <h2>{{ $t("agents.capabilities") }}</h2>
              <span v-if="canManageCapabilities" class="secondary-text">
                {{ $t("agents.capabilitiesHelp") }}
              </span>
            </div>
            <div class="capability-list">
              <label
                v-for="capability in capabilityCatalog"
                :key="capability.key"
                class="capability-item"
              >
                <input
                  v-if="canManageCapabilities"
                  type="checkbox"
                  :checked="hasCapability(capability.key)"
                  :disabled="Boolean(capabilityBusy)"
                  @change="toggleCapability(capability.key)"
                />
                <span>
                  <strong>{{ capability.name }}</strong>
                  <small>{{ capability.description }}</small>
                </span>
              </label>
              <p v-if="!capabilityCatalog.length">{{ $t("common.empty") }}</p>
            </div>
            <p v-if="capabilityBusy" class="secondary-text" aria-live="polite">
              {{ $t("agents.capabilitySaving") }}
            </p>
            <ProblemNotice
              v-if="platform.problems.capabilities"
              :problem="platform.problems.capabilities"
              compact
            />
          </section>
          <section class="panel">
            <h2>{{ $t("agents.knowledge") }}</h2>
            <p>{{ agent.knowledgeArtifactRefs.length }}</p>
          </section>
        </aside>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.detail-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.6fr) minmax(280px, 0.7fr);
  gap: 18px;
}
.detail-main,
.detail-side {
  display: grid;
  align-content: start;
  gap: 16px;
}
.metadata {
  display: flex;
  gap: 28px;
}
.metadata dt {
  color: var(--subtle);
  font-size: 0.8rem;
}
.metadata dd {
  margin: 4px 0;
}
.inline-actions,
.chip-list {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 12px;
}
.chip-list span {
  padding: 5px 8px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent-strong);
  font-size: 0.82rem;
}
.launch-panel {
  display: grid;
  gap: 12px;
}
.profile-form,
.capability-list {
  display: grid;
  gap: 10px;
}
.runtime-details {
  margin-top: 14px;
}
.runtime-details summary {
  cursor: pointer;
}
.capability-item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 9px;
}
.capability-item strong,
.capability-item small {
  display: block;
}
.capability-item small,
.secondary-text {
  color: var(--text-secondary);
  font-size: 0.84rem;
}
.environment-options {
  display: grid;
  gap: 10px;
  padding: 0;
  border: 0;
}
.environment-option {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  cursor: pointer;
}
.environment-option > span,
.environment-option > span > span {
  display: block;
}
.environment-option strong {
  margin-right: 8px;
}
.environment-option small {
  color: var(--accent-strong);
}
.environment-option--selected {
  border-color: var(--accent-strong);
}
.environment-option--unavailable {
  cursor: not-allowed;
  opacity: 0.65;
}
.environment-software,
.build-progress {
  margin-top: 6px;
  color: var(--subtle);
  font-size: 0.86rem;
}
@media (max-width: 900px) {
  .detail-layout {
    grid-template-columns: 1fr;
  }
}
</style>
