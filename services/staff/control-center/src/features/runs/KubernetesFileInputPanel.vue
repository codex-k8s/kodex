<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import KubernetesFileCleanupPanel from "./KubernetesFileCleanupPanel.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import {
  useKubernetesFileInputs,
  type KubernetesFileContext,
} from "./kubernetes-file-input";
const props = defineProps<{ context: KubernetesFileContext }>();
const { t } = useI18n();
const model = useKubernetesFileInputs(computed(() => props.context));
const { items, cleanupItems, problem, busy, inactive } = model;
</script>
<template>
  <section
    data-testid="kubernetes-file-input"
    :aria-label="t('kubernetesFile.title')"
  >
    <h4>{{ t("kubernetesFile.title") }}</h4>
    <p>{{ t("kubernetesFile.description") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <button
      type="button"
      class="button button--ghost"
      :disabled="busy || inactive"
      @click="model.refresh()"
    >
      {{ t("kubernetesFile.refresh") }}
    </button>
    <p v-if="!items.length && !cleanupItems.length && !busy && !problem">
      {{ t("kubernetesFile.empty") }}
    </p>
    <ul>
      <li v-for="item in items" :key="item.sourceRef">
        <strong>{{ t(`kubernetesFile.states.${item.state}`) }}</strong>
        <p>
          {{ item.artifactRef }} · {{ item.artifactRevisionRef }} ·
          {{ item.transferFormat }}
        </p>
        <p>
          {{
            t("kubernetesFile.progress", {
              acknowledged: item.acknowledgedBytes,
              staged: item.stagedBytes,
              total: item.normalizedSizeBytes,
            })
          }}
        </p>
        <p v-if="item.reason !== 'NONE'">
          {{ t(`kubernetesFile.reasons.${item.reason}`) }}
        </p>
        <p v-if="item.eofAcknowledged">{{ t("kubernetesFile.eof") }}</p>
        <details>
          <summary>{{ t("kubernetesFile.integrity") }}</summary>
          <p>{{ item.artifactSHA256 }}</p>
          <p v-if="item.normalizedSHA256">{{ item.normalizedSHA256 }}</p>
          <p>{{ item.sourceRef }}</p>
        </details>
      </li>
    </ul>
    <KubernetesFileCleanupPanel
      v-for="item in cleanupItems"
      :key="item.sourceRef"
      :context="context"
      :source-ref="item.sourceRef"
    />
    <p>{{ t("kubernetesFile.limit") }}</p>
  </section>
</template>
