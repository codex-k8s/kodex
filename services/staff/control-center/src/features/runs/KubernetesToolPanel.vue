<script setup lang="ts">
import { computed } from "vue";
import KubernetesFileInputPanel from "./KubernetesFileInputPanel.vue";
import KubernetesCapturePanel from "./KubernetesCapturePanel.vue";
import { useI18n } from "vue-i18n";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { kubernetesToolIsActive } from "@/shared/api/kubernetes-tool";
import { useKubernetesTools } from "./kubernetes-tool";
const props = withDefaults(
  defineProps<{ runRef: string; nodeRef: string; showCaptures?: boolean }>(),
  { showCaptures: true },
);
const { t } = useI18n();
const model = useKubernetesTools(
  computed(() => ({ runRef: props.runRef, nodeRef: props.nodeRef })),
);
const { items, problem, nextPageToken, busy, writing, uncertain } = model;
</script>
<template>
  <section
    class="kubernetes-tools"
    data-testid="kubernetes-tools"
    :aria-label="t('kubernetesTool.title')"
  >
    <h3>{{ t("kubernetesTool.title") }}</h3>
    <p>{{ t("kubernetesTool.description") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="uncertain" role="status">{{ t("kubernetesTool.uncertain") }}</p>
    <button
      type="button"
      class="button button--ghost"
      :disabled="busy || writing"
      @click="model.refresh()"
    >
      {{ t("kubernetesTool.refresh") }}
    </button>
    <button
      v-if="nextPageToken"
      type="button"
      class="button button--ghost"
      :disabled="busy || writing"
      @click="model.refresh(nextPageToken)"
    >
      {{ t("kubernetesTool.next") }}
    </button>
    <p v-if="!items.length && !busy && !problem">
      {{ t("kubernetesTool.empty") }}
    </p>
    <ul>
      <li v-for="item in items" :key="item.invocationRef">
        <strong>{{ item.operation }}</strong>
        <StatusBadge :state="item.state" />
        <p
          v-if="
            item.safeReasonCode ===
            'KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED'
          "
          role="status"
        >
          {{ t("kubernetesTool.portForwardProfileUnsupported") }}
        </p>
        <span v-if="item.waitingForGate">{{
          t("kubernetesTool.waiting")
        }}</span>
        <details>
          <summary>{{ t("kubernetesTool.details") }}</summary>
          <p>
            {{ item.targetRef }} · {{ item.connectionRef }} ·
            {{ item.invocationRef }}
          </p>
        </details>
        <KubernetesFileInputPanel
          v-if="item.operation === 'kubernetes.cp.upload'"
          :context="{
            invocationRef: item.invocationRef,
            runRef: item.runRef,
            nodeRef: item.nodeRef,
            active: kubernetesToolIsActive(item),
          }"
        />
        <template v-if="showCaptures">
          <KubernetesCapturePanel
            v-for="capture in item.captureStatuses"
            :key="capture.captureRef"
            :context="{
              invocationRef: item.invocationRef,
              runRef: item.runRef,
              nodeRef: item.nodeRef,
            }"
            :summary="capture"
          />
        </template>
        <button
          v-if="kubernetesToolIsActive(item)"
          type="button"
          class="button button--ghost"
          :disabled="busy || writing || uncertain"
          @click="model.stop(item)"
        >
          {{ t("kubernetesTool.stop") }}
        </button>
      </li>
    </ul>
    <p v-if="items.length">{{ t("kubernetesTool.closure") }}</p>
  </section>
</template>
<style scoped>
.kubernetes-tools {
  display: grid;
  gap: 0.6rem;
}
.kubernetes-tools ul {
  list-style: none;
  padding: 0;
  display: grid;
  gap: 0.8rem;
}
.kubernetes-tools li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.6rem;
}
.kubernetes-tools details {
  flex-basis: 100%;
  overflow-wrap: anywhere;
}
</style>
