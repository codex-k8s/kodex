<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { computed, onMounted, ref } from "vue";

import {
  assistantEffectiveRuntimeState,
  latestAssistantSnapshot,
} from "@/features/assistant/model";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import EntityIcon from "@/shared/ui/EntityIcon.vue";

const platform = usePlatformStore();
const ownerInstructions = ref("");
const busy = ref(false);
const problem = ref<AppProblem>();
const state = computed(() => platform.administration);
const currentAssistant = computed(() =>
  state.value
    ? latestAssistantSnapshot(state.value.assistant, platform.assistant)
    : undefined,
);
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
      ><RouterLink class="button" to="/administration/providers">{{
        $t("providers.title")
      }}</RouterLink
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
        ><nav class="configuration-links" :aria-label="$t('managed.title')">
          <RouterLink
            v-for="kind in [
              'PROMPT_TEMPLATE',
              'ROLE_IMAGE',
              'INTEGRATION_DEFINITION',
              'SYSTEM_STT',
            ]"
            :key="kind"
            class="button"
            :to="`/configurations/${kind}`"
            >{{ $t(`managed.kinds.${kind}`) }}</RouterLink
          >
        </nav>
        <div class="metric-grid">
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
            ><StatusBadge
              :state="
                currentAssistant
                  ? assistantEffectiveRuntimeState(currentAssistant)
                  : 'RECOVERING'
              "
              :label="currentAssistant?.readinessSummary"
            />
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
            <VoiceTextarea
              v-model="ownerInstructions"
              maxlength="32768"
            /><button
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
            <div class="administration-table-wrap">
              <table
                class="administration-table administration-table--environments"
              >
                <thead>
                  <tr>
                    <th scope="col">{{ $t("common.name") }}</th>
                    <th scope="col">
                      {{ $t("roleEnvironments.catalogSoftware") }}
                    </th>
                    <th scope="col">{{ $t("common.status") }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="environment in environments"
                    :key="environment.key"
                  >
                    <td>
                      <div class="administration-table__identity">
                        <EntityIcon kind="ENVIRONMENT" /><span
                          ><strong>{{ $t(environment.nameMessageKey) }}</strong
                          ><small>{{
                            $t(environment.descriptionMessageKey)
                          }}</small></span
                        >
                      </div>
                    </td>
                    <td>
                      {{
                        environment.softwareMessageKeys
                          .map((key) => $t(key))
                          .join(" · ")
                      }}
                    </td>
                    <td>
                      <StatusBadge
                        :state="environment.available ? 'READY' : 'UNAVAILABLE'"
                      />
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <ProblemNotice
              v-if="platform.problems.roleEnvironments"
              :problem="platform.problems.roleEnvironments"
              compact
            />
          </section>
          <section class="panel">
            <h2>{{ $t("administration.incidents") }}</h2>
            <div
              v-if="state.incidents.length"
              class="administration-table-wrap"
            >
              <table
                class="administration-table administration-table--incidents"
              >
                <thead>
                  <tr>
                    <th scope="col">{{ $t("common.description") }}</th>
                    <th scope="col">{{ $t("administration.nextStep") }}</th>
                    <th scope="col">{{ $t("common.status") }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="incident in state.incidents" :key="incident.ref">
                    <td>
                      <strong>{{ incident.safeSummary }}</strong>
                    </td>
                    <td>{{ incident.safeNextStep }}</td>
                    <td><StatusBadge :state="incident.state" /></td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else>{{ $t("administration.noIncidents") }}</p>
          </section>
        </div></template
      >
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.configuration-links {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}
.administration-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(300px, 0.8fr);
  gap: 16px;
}
.panel :deep(textarea) {
  margin: 12px 0;
}
.metric-text {
  font-size: 1.05rem;
}
.administration-table-wrap {
  min-width: 0;
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.administration-table {
  width: 100%;
  min-width: 700px;
  border-collapse: collapse;
  table-layout: fixed;
  background: var(--surface);
}
.administration-table th,
.administration-table td {
  padding: 10px 12px;
  text-align: left;
  vertical-align: middle;
}
.administration-table th {
  color: var(--muted);
  font-size: 0.72rem;
  font-weight: 600;
}
.administration-table tbody tr + tr {
  border-top: 1px solid var(--border);
}
.administration-table--environments th:first-child {
  width: 39%;
}
.administration-table--environments th:nth-child(2) {
  width: 45%;
}
.administration-table--environments th:last-child {
  width: 16%;
}
.administration-table--incidents th:first-child {
  width: 40%;
}
.administration-table--incidents th:nth-child(2) {
  width: 42%;
}
.administration-table--incidents th:last-child {
  width: 18%;
}
.administration-table__identity {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.administration-table__identity > span {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.administration-table__identity small {
  color: var(--muted);
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
