<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import type { KubernetesCaptureContext } from "@/shared/api/kubernetes-capture";
import type { KubernetesCaptureSummary } from "@/shared/api/generated/openapi/types.gen";
import { useKubernetesCapture } from "./kubernetes-capture";
const props = defineProps<{
  context: KubernetesCaptureContext;
  summary: KubernetesCaptureSummary;
}>();
const { t } = useI18n();
const model = useKubernetesCapture(
  computed(() => ({ ...props.context, captureRef: props.summary.captureRef })),
);
const {
  current,
  problem,
  busy,
  uncertain,
  canRepair,
  canRepairCleanup,
  inactive,
  hidden,
} = model;
const summary = computed(() => current.value?.view.summary ?? props.summary);
</script>
<template>
  <section
    v-if="!inactive"
    data-testid="kubernetes-capture"
    :aria-label="t('kubernetesCapture.title')"
  >
    <h4>{{ t("kubernetesCapture.title") }}</h4>
    <p>{{ t("kubernetesCapture.explanation") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="uncertain" role="status">{{ t("kubernetesCapture.uncertain") }}</p>
    <template v-if="!hidden">
      <StatusBadge :state="summary.state" />
      <p v-if="summary.reasonCode" role="status">
        {{ t(`kubernetesCapture.reason.${summary.reasonCode}`) }}
      </p>
      <p v-if="current">
        {{
          t("kubernetesCapture.attempts", { count: current.view.attemptCount })
        }}
      </p>
      <p v-if="current?.view.kind === 'PARTIAL'">
        {{ t("kubernetesCapture.partial") }}
      </p>
      <div v-if="current?.view.cleanup" data-testid="kubernetes-cleanup">
        <p>
          {{
            t("kubernetesCapture.cleanupStatus", {
              count: current.view.cleanup.outstandingCount,
              bytes: current.view.cleanup.outstandingBytes,
              exhausted: current.view.cleanup.exhaustedCount,
            })
          }}
        </p>
        <button
          v-if="current.view.cleanup.eligibleExhaustedCount > 0"
          type="button"
          class="button button--ghost"
          :disabled="!canRepairCleanup"
          @click="model.repairCleanup"
        >
          {{ t("kubernetesCapture.cleanupRepair") }}
        </button>
        <p v-if="current.view.cleanup.exhaustedCount > 0">
          {{ t("kubernetesCapture.cleanupExplanation") }}
        </p>
        <p v-if="current.cleanupReceipt" role="status">
          {{
            t("kubernetesCapture.cleanupReceipt", {
              count: current.cleanupReceipt.repairedCount,
              remaining: current.cleanupReceipt.remainingExhaustedCount,
            })
          }}
        </p>
      </div>
      <ul v-if="current?.view.artifacts.length">
        <li
          v-for="artifact in current.view.artifacts"
          :key="artifact.revisionRef"
        >
          {{ artifact.channel }} · {{ artifact.artifactRef }} ·
          {{ artifact.sizeBytes }} {{ t("kubernetesCapture.bytes") }}
        </li>
      </ul>
      <button
        v-if="summary.state === 'WAITING_OWNER'"
        type="button"
        class="button button--ghost"
        :disabled="!canRepair"
        @click="model.repair"
      >
        {{ t("kubernetesCapture.repair") }}
      </button>
      <p v-if="summary.state === 'WAITING_OWNER'">
        {{ t("kubernetesCapture.permission") }}
      </p>
    </template>
    <button
      type="button"
      class="button button--ghost"
      :disabled="busy"
      @click="model.refresh"
    >
      {{ t("kubernetesCapture.refresh") }}
    </button>
  </section>
</template>
