<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import type { KubernetesFileContext } from "./kubernetes-file-input";
import { useKubernetesFileCleanup } from "./kubernetes-file-cleanup";
const props = defineProps<{
  context: KubernetesFileContext;
  sourceRef: string;
}>();
const { t } = useI18n();
const model = useKubernetesFileCleanup(
  computed(() => ({ ...props.context, sourceRef: props.sourceRef })),
);
const { current, problem, busy, uncertain, inactive, canRepair } = model;
</script>
<template>
  <section
    v-if="!inactive"
    data-testid="kubernetes-file-cleanup"
    :aria-label="t('kubernetesFile.cleanupTitle')"
  >
    <h5>{{ t("kubernetesFile.cleanupTitle") }}</h5>
    <p>{{ sourceRef }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="uncertain" role="status">
      {{ t("kubernetesFile.cleanupUncertain") }}
    </p>
    <template v-if="current">
      <p>
        {{
          t("kubernetesFile.cleanupStatus", {
            count: current.cleanupView.cleanup.outstandingCount,
            bytes: current.cleanupView.cleanup.outstandingBytes,
            exhausted: current.cleanupView.cleanup.exhaustedCount,
          })
        }}
      </p>
      <button
        v-if="
          current.cleanupView.repairAvailable === true &&
          current.cleanupView.cleanup.eligibleExhaustedCount === 1
        "
        type="button"
        class="button button--ghost"
        :disabled="!canRepair"
        @click="model.repair"
      >
        {{ t("kubernetesFile.cleanupRepair") }}
      </button>
      <p
        v-else-if="current.cleanupView.cleanup.eligibleExhaustedCount === 1"
        role="status"
      >
        {{ t("kubernetesFile.cleanupRepairUnavailable") }}
      </p>
      <p v-if="current.cleanupReceipt" role="status">
        {{ t("kubernetesFile.cleanupAccepted") }}
      </p>
    </template>
    <button
      type="button"
      class="button button--ghost"
      :disabled="busy"
      @click="model.refresh"
    >
      {{ t("kubernetesFile.cleanupRefresh") }}
    </button>
    <p>{{ t("kubernetesFile.cleanupLimit") }}</p>
  </section>
</template>
