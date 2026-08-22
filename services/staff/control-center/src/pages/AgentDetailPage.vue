<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const agentRef = computed(() => String(route.params.agentRef));
const projectRef = computed(() => String(route.params.projectRef));
const agent = computed(() => platform.agents[agentRef.value]);
const instructions = ref("");
const task = ref("");
const busy = ref(false);
const problem = ref<AppProblem>();
async function load() {
  await platform.loadAgent(agentRef.value);
  instructions.value =
    agent.value?.draftInstructions?.content ??
    agent.value?.publishedInstructions?.content ??
    "";
}
async function saveInstructions() {
  if (!agent.value) return;
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
  if (!agent.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.instructionCommand(agent.value, action);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function launch() {
  if (!agent.value || !task.value.trim()) return;
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
  if (!agent.value) return;
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
            <h2>{{ $t("agents.role") }}</h2>
            <p>{{ agent.roleDescription }}</p>
            <dl class="metadata">
              <div>
                <dt>{{ $t("agents.runtime") }}</dt>
                <dd>{{ agent.runtimeName ?? $t("common.noData") }}</dd>
              </div>
              <div>
                <dt>{{ $t("common.version", { version: agent.version }) }}</dt>
                <dd>{{ new Date(agent.updatedAt).toLocaleString() }}</dd>
              </div>
            </dl>
          </article>
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
            <textarea v-model="instructions" maxlength="65536" />
            <div class="inline-actions">
              <button
                class="button"
                type="button"
                :disabled="busy || !agent.nextActions.includes('EDIT')"
                @click="saveInstructions"
              >
                {{ $t("common.save") }}</button
              ><button
                class="button"
                type="button"
                :disabled="busy || !agent.nextActions.includes('VALIDATE')"
                @click="instructionAction('VALIDATE')"
              >
                {{ $t("agents.validate") }}</button
              ><button
                class="button button--primary"
                type="button"
                :disabled="busy || !agent.nextActions.includes('PUBLISH')"
                @click="instructionAction('PUBLISH')"
              >
                {{ $t("agents.publish") }}
              </button>
            </div>
          </article>
          <ProblemNotice v-if="problem" :problem="problem" compact />
        </section>
        <aside class="detail-side">
          <section class="panel launch-panel">
            <h2>{{ $t("runs.new") }}</h2>
            <label class="field"
              ><span>{{ $t("runs.task") }}</span
              ><textarea v-model="task" required maxlength="8000" /></label
            ><button
              class="button button--primary"
              type="button"
              :disabled="
                busy || !task.trim() || !agent.nextActions.includes('LAUNCH')
              "
              @click="launch"
            >
              {{ $t("common.launch") }}
            </button>
          </section>
          <section class="panel">
            <h2>{{ $t("agents.capabilities") }}</h2>
            <div class="chip-list">
              <span
                v-for="capability in agent.capabilities"
                :key="capability.key"
                >{{ capability.name }}</span
              >
              <p v-if="!agent.capabilities.length">{{ $t("common.empty") }}</p>
            </div>
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
@media (max-width: 900px) {
  .detail-layout {
    grid-template-columns: 1fr;
  }
}
</style>
