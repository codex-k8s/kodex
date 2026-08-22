<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import { usePlatformStore } from "@/features/platform/store";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const openGates = computed(() =>
  platform.gateList.filter((item) => item.state === "OPEN"),
);
const comments = ref<Record<string, string>>({});
const busyRef = ref("");
const problem = ref<AppProblem>();

async function decide(
  gate: OwnerGate,
  decision: "APPROVE" | "REJECT" | "REQUEST_CHANGES" | "CANCEL",
): Promise<void> {
  if (
    !gate.nextActions.includes("RESOLVE_GATE") ||
    !gate.allowedDecisions.includes(decision)
  )
    return;
  busyRef.value = gate.ref;
  problem.value = undefined;
  try {
    await platform.decide(gate, {
      decision,
      comment: comments.value[gate.ref] ?? "",
    });
  } catch (error) {
    problem.value = asProblem(error);
    if (problem.value.kind === "conflict") await platform.loadGates();
  } finally {
    busyRef.value = "";
  }
}

onMounted(() => void platform.loadGates());
</script>

<template>
  <PageFrame
    :title="$t('decisions.title')"
    :subtitle="$t('decisions.subtitle')"
  >
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <AsyncState
      :loading="platform.loading.gates"
      :problem="platform.problems.gates"
      :empty="openGates.length === 0"
      :empty-title="$t('decisions.emptyTitle')"
      @retry="platform.loadGates()"
    >
      <div class="decision-grid">
        <article v-for="gate in openGates" :key="gate.ref" class="panel">
          <div class="card-heading">
            <div>
              <h2>{{ gate.title }}</h2>
              <RouterLink :to="`/runs/${gate.runRef}`">{{
                $t("decisions.openRun")
              }}</RouterLink>
            </div>
            <StatusBadge :state="gate.state" />
          </div>
          <h3>{{ $t("decisions.requestedBy") }}</h3>
          <p>{{ gate.requestedBy.displayName }}</p>
          <h3>{{ $t("decisions.consequences") }}</h3>
          <p>{{ gate.consequencesSummary }}</p>
          <label class="field"
            ><span>{{ $t("decisions.comment") }}</span
            ><textarea v-model="comments[gate.ref]" maxlength="2000" />
          </label>
          <div class="decision-actions">
            <button
              v-if="
                gate.allowedDecisions.includes('APPROVE') &&
                gate.nextActions.includes('RESOLVE_GATE')
              "
              class="button button--primary"
              type="button"
              :disabled="busyRef === gate.ref"
              @click="decide(gate, 'APPROVE')"
            >
              {{ $t("common.approve") }}</button
            ><button
              v-if="
                gate.allowedDecisions.includes('REQUEST_CHANGES') &&
                gate.nextActions.includes('RESOLVE_GATE')
              "
              class="button"
              type="button"
              :disabled="busyRef === gate.ref"
              @click="decide(gate, 'REQUEST_CHANGES')"
            >
              {{ $t("common.requestChanges") }}</button
            ><button
              v-if="
                gate.allowedDecisions.includes('REJECT') &&
                gate.nextActions.includes('RESOLVE_GATE')
              "
              class="button button--danger"
              type="button"
              :disabled="busyRef === gate.ref"
              @click="decide(gate, 'REJECT')"
            >
              {{ $t("common.reject") }}
            </button>
          </div>
        </article>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.decision-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 16px;
}
.card-heading,
.decision-actions {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 10px;
}
.decision-actions {
  justify-content: flex-start;
  flex-wrap: wrap;
  margin-top: 14px;
}
</style>
