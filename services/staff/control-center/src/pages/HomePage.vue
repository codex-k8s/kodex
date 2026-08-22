<script setup lang="ts">
import { computed, onMounted } from "vue";

import { usePlatformStore } from "@/features/platform/store";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const activeRuns = computed(() => platform.overview?.activeRuns ?? []);

onMounted(() => void platform.loadOverview());
</script>

<template>
  <PageFrame :title="$t('home.title')" :subtitle="$t('home.subtitle')">
    <template #actions>
      <RouterLink class="button button--primary" to="/projects">{{
        $t("home.newRun")
      }}</RouterLink>
    </template>
    <AsyncState
      :loading="platform.loading.overview"
      :problem="platform.problems.overview"
      @retry="platform.loadOverview()"
    >
      <section class="metric-grid" aria-label="Overview">
        <article class="metric-card">
          <span>{{ $t("home.projects") }}</span
          ><strong>{{ platform.overview?.projectCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("home.agents") }}</span
          ><strong>{{ platform.overview?.agentCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("home.activeRuns") }}</span
          ><strong>{{ platform.overview?.activeRunCount ?? 0 }}</strong>
        </article>
        <article class="metric-card">
          <span>{{ $t("home.pending") }}</span
          ><strong>{{ platform.overview?.pendingGateCount ?? 0 }}</strong>
        </article>
      </section>
      <div class="home-layout">
        <section>
          <div class="section-header">
            <h2>{{ $t("home.activeRuns") }}</h2>
            <RouterLink to="/runs">{{ $t("common.all") }}</RouterLink>
          </div>
          <div v-if="activeRuns.length" class="entity-list">
            <RouterLink
              v-for="run in activeRuns"
              :key="run.ref"
              :to="`/runs/${run.ref}`"
              class="entity-row"
            >
              <div>
                <h3>{{ run.title }}</h3>
                <p>{{ run.currentActivity ?? run.target.displayName }}</p>
              </div>
              <StatusBadge :state="run.state" /><span>{{ run.source }}</span>
            </RouterLink>
          </div>
          <div v-else class="empty-compact">{{ $t("common.empty") }}</div>
        </section>
        <section>
          <div class="section-header">
            <h2>{{ $t("home.pending") }}</h2>
            <RouterLink to="/decisions">{{ $t("common.all") }}</RouterLink>
          </div>
          <div class="entity-list">
            <RouterLink
              v-for="gate in platform.overview?.pendingGates ?? []"
              :key="gate.ref"
              :to="`/runs/${gate.runRef}`"
              class="entity-row"
            >
              <div>
                <h3>{{ gate.title }}</h3>
                <p>{{ gate.contextSummary }}</p>
              </div>
              <StatusBadge :state="gate.state" />
            </RouterLink>
          </div>
        </section>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.home-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(300px, 0.8fr);
  gap: 24px;
}
.empty-compact {
  padding: 24px;
  border: 1px dashed var(--border);
  border-radius: 9px;
  color: var(--muted);
  text-align: center;
}
@media (max-width: 1000px) {
  .home-layout {
    grid-template-columns: 1fr;
  }
}
</style>
