<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import KubernetesCapturePanel from "./KubernetesCapturePanel.vue";
import { useKubernetesCaptureHistory } from "./kubernetes-capture-history";
const props = defineProps<{ runRef: string; nodeRef: string }>();
const { t } = useI18n();
const model = useKubernetesCaptureHistory(
  computed(() => ({ runRef: props.runRef, nodeRef: props.nodeRef })),
);
const { items, nextPageToken, problem, busy } = model;
</script>
<template>
  <section
    data-testid="kubernetes-capture-history"
    :aria-label="t('kubernetesCapture.historyTitle')"
  >
    <h3>{{ t("kubernetesCapture.historyTitle") }}</h3>
    <ProblemNotice v-if="problem" :problem="problem" />
    <button
      type="button"
      class="button button--ghost"
      :disabled="busy"
      @click="model.refresh()"
    >
      {{ t("kubernetesCapture.historyRefresh") }}
    </button>
    <button
      v-if="nextPageToken"
      type="button"
      class="button button--ghost"
      :disabled="busy"
      @click="model.refresh(nextPageToken)"
    >
      {{ t("kubernetesCapture.historyNext") }}
    </button>
    <p v-if="!items.length && !busy && !problem">
      {{ t("kubernetesCapture.historyEmpty") }}
    </p>
    <KubernetesCapturePanel
      v-for="item in items"
      :key="item.summary.captureRef"
      :context="{
        invocationRef: item.invocationRef,
        runRef: item.runRef,
        nodeRef: item.nodeRef,
      }"
      :summary="item.summary"
    />
  </section>
</template>
