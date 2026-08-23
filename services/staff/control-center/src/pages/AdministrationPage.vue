<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const ownerInstructions = ref("");
const busy = ref(false);
const problem = ref<AppProblem>();
const state = computed(() => platform.administration);
const environments = computed(() =>
  Object.values(platform.roleEnvironments).sort((left, right) =>
    left.key.localeCompare(right.key),
  ),
);

async function load(): Promise<void> {
  await Promise.all([
    platform.loadAdministration(),
    platform.loadRoleEnvironments(),
  ]);
  ownerInstructions.value = platform.assistant?.ownerInstructions ?? "";
}

async function save(): Promise<void> {
  if (!state.value?.assistant.nextActions.includes("EDIT")) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.updateAssistantInstructions(ownerInstructions.value);
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
    :title="$t('administration.title')"
    :subtitle="$t('administration.subtitle')"
  >
    <template #actions
      ><RouterLink class="button" to="/administration/access">{{
        $t("access.title")
      }}</RouterLink
      ><RouterLink class="button" to="/administration/audit">{{
        $t("audit.title")
      }}</RouterLink></template
    >
    <AsyncState
      :loading="platform.loading.administration"
      :problem="platform.problems.administration"
      @retry="load"
    >
      <template v-if="state"
        ><div class="metric-grid">
          <article class="metric-card">
            <span>{{ $t("administration.profile") }}</span
            ><strong class="metric-text">{{
              $t(`administration.profiles.${state.profile}`)
            }}</strong>
          </article>
          <article class="metric-card">
            <span>{{ $t("administration.core") }}</span
            ><StatusBadge :state="state.coreReady ? 'READY' : 'FAILED'" />
            <p>{{ state.coreSummary }}</p>
          </article>
          <article class="metric-card">
            <span>{{ $t("administration.assistant") }}</span
            ><StatusBadge :state="state.assistant.runtimeState" />
            <p>{{ state.assistant.readinessSummary }}</p>
          </article>
          <article class="metric-card">
            <span>{{ $t("administration.adapters") }}</span
            ><strong>{{ state.optionalAdapters.length }}</strong>
          </article>
        </div>
        <div class="administration-grid">
          <section class="panel">
            <h2>{{ $t("administration.ownerInstructions") }}</h2>
            <p>{{ $t("administration.corePromptProtected") }}</p>
            <textarea v-model="ownerInstructions" maxlength="32768" /><button
              v-if="state.assistant.nextActions.includes('EDIT')"
              class="button button--primary"
              type="button"
              :disabled="busy"
              @click="save"
            >
              {{ $t("common.save") }}</button
            ><ProblemNotice v-if="problem" :problem="problem" compact />
          </section>
          <section class="panel environment-catalog">
            <h2>{{ $t("roleEnvironments.catalogTitle") }}</h2>
            <p>{{ $t("roleEnvironments.catalogDescription") }}</p>
            <div class="entity-list">
              <article
                v-for="environment in environments"
                :key="environment.key"
                class="card"
              >
                <div class="card-heading">
                  <strong>{{ $t(environment.nameMessageKey) }}</strong>
                  <StatusBadge
                    :state="environment.available ? 'READY' : 'UNAVAILABLE'"
                  />
                </div>
                <p>{{ $t(environment.descriptionMessageKey) }}</p>
                <small>
                  {{
                    environment.softwareMessageKeys
                      .map((key) => $t(key))
                      .join(" · ")
                  }}
                </small>
              </article>
            </div>
            <ProblemNotice
              v-if="platform.problems.roleEnvironments"
              :problem="platform.problems.roleEnvironments"
              compact
            />
          </section>
          <section class="panel">
            <h2>{{ $t("administration.incidents") }}</h2>
            <div v-if="state.incidents.length" class="entity-list">
              <article
                v-for="incident in state.incidents"
                :key="incident.ref"
                class="card"
              >
                <div class="card-heading">
                  <strong>{{ incident.safeSummary }}</strong
                  ><StatusBadge :state="incident.state" />
                </div>
                <p>{{ incident.safeNextStep }}</p>
              </article>
            </div>
            <p v-else>{{ $t("administration.noIncidents") }}</p>
          </section>
        </div></template
      >
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.administration-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(300px, 0.8fr);
  gap: 16px;
}
.panel textarea {
  margin: 12px 0;
}
.metric-text {
  font-size: 1.05rem;
}
.card-heading {
  display: flex;
  justify-content: space-between;
  gap: 10px;
}
.environment-catalog {
  grid-column: 1 / -1;
}
@media (max-width: 900px) {
  .administration-grid {
    grid-template-columns: 1fr;
  }
}
</style>
