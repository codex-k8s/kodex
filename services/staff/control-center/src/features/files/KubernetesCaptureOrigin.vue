<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { assertArtifactCaptureOrigin } from "@/shared/api/kubernetes-capture-origin";
import type { KubernetesArtifactCaptureOrigin } from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{ origin?: KubernetesArtifactCaptureOrigin }>();
const { t } = useI18n();
const valid = computed(() => {
  try {
    return assertArtifactCaptureOrigin(props.origin) !== undefined;
  } catch {
    return false;
  }
});
const remote = computed(() => {
  switch (props.origin?.remoteOutcome) {
    case "SUCCEEDED":
      return t("fileRevision.capture.remoteSucceeded");
    case "FAILED":
      return t("fileRevision.capture.remoteFailed");
    default:
      return t("fileRevision.capture.remoteUnknown");
  }
});
</script>

<template>
  <section
    v-if="origin"
    class="capture-origin"
    :aria-label="t('fileRevision.capture.title')"
  >
    <p v-if="!valid" role="alert">{{ t("fileRevision.capture.invalid") }}</p>
    <template v-else>
      <strong>{{
        t(
          origin.kind === "FULL"
            ? "fileRevision.capture.full"
            : "fileRevision.capture.partial",
        )
      }}</strong>
      <p>
        <code>{{ origin.operation }}</code> · {{ origin.channel }}
      </p>
      <p>
        {{ origin.targetRef }} · {{ origin.resource.resource }}
        {{ origin.resource.namespace }}/{{ origin.resource.name }}
      </p>
      <p>
        {{
          t("fileRevision.capture.coverage", {
            prefix: origin.acknowledgedPrefix,
            watermark: origin.outputWatermark,
          })
        }}
      </p>
      <p v-if="origin.droppedBytes !== '0'">
        {{ t("fileRevision.capture.dropped", { bytes: origin.droppedBytes }) }}
      </p>
      <p v-if="!origin.nativeTerminalKnown">
        {{ t("fileRevision.capture.nativeUnknown") }}
      </p>
      <template v-else>
        <p>{{ t("fileRevision.capture.remote", { outcome: remote }) }}</p>
        <p v-if="origin.exitCode !== undefined">
          {{ t("fileRevision.capture.exit", { code: origin.exitCode }) }}
        </p>
      </template>
      <p>
        {{
          t(
            origin.transportJoined
              ? "fileRevision.capture.joined"
              : "fileRevision.capture.unjoined",
          )
        }}
      </p>
      <p>{{ t("fileRevision.capture.limitation") }}</p>
    </template>
  </section>
</template>

<style scoped>
.capture-origin {
  display: grid;
  gap: 6px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow-wrap: anywhere;
}
.capture-origin p {
  margin: 0;
}
</style>
