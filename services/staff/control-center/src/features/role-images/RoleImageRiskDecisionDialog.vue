<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { validRiskDecisionReason } from "./vulnerability-report";

const props = defineProps<{
  action: "ACCEPT_RISK" | "REJECT_RISK";
  bindingKey: string;
  imageDigest: string;
  reportDigest: string;
  busy: boolean;
  problem?: AppProblem;
  originalReason?: string;
}>();
const emit = defineEmits<{ close: []; confirm: [reason: string] }>();
const { t } = useI18n();
const reason = ref(props.originalReason ?? "");
const accepted = computed(() => props.action === "ACCEPT_RISK");
const valid = computed(() => validRiskDecisionReason(reason.value));
watch(
  () => props.bindingKey,
  () => {
    reason.value = "";
    emit("close");
  },
);
function confirm(): void {
  if (props.busy || !valid.value) return;
  emit("confirm", reason.value.trim());
}
</script>

<template>
  <ModalDialog
    :title="
      t(
        accepted
          ? 'imageVulnerabilities.acceptTitle'
          : 'imageVulnerabilities.rejectTitle',
      )
    "
    :busy="busy"
    size="md"
    @close="emit('close')"
  >
    <p>
      {{
        t(
          accepted
            ? "imageVulnerabilities.acceptHelp"
            : "imageVulnerabilities.rejectHelp",
        )
      }}
    </p>
    <dl class="risk-digests">
      <dt>{{ t("imageVulnerabilities.imageDigest") }}</dt>
      <dd>
        <code>{{ imageDigest }}</code>
      </dd>
      <dt>{{ t("imageVulnerabilities.reportDigest") }}</dt>
      <dd>
        <code>{{ reportDigest }}</code>
      </dd>
    </dl>
    <label class="field">
      <span>{{ t("imageVulnerabilities.reason") }}</span>
      <textarea
        v-model="reason"
        rows="3"
        maxlength="2048"
        required
        :disabled="busy"
        :readonly="originalReason !== undefined"
        data-dialog-initial-focus
      />
      <small>{{ t("imageVulnerabilities.reasonHelp") }}</small>
    </label>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <template #actions>
      <button
        type="button"
        class="button button--secondary"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ t("common.cancel") }}
      </button>
      <button
        type="button"
        class="button"
        :disabled="busy || !valid"
        @click="confirm"
      >
        {{
          t(
            originalReason !== undefined
              ? "imageVulnerabilities.retryOriginal"
              : accepted
                ? "imageVulnerabilities.accept"
                : "imageVulnerabilities.reject",
          )
        }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.risk-digests {
  display: grid;
  gap: 6px;
  margin: 16px 0;
  font-size: 12px;
}
.risk-digests dt {
  color: var(--muted);
}
.risk-digests dd {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
}
textarea {
  min-height: 72px;
  max-height: 180px;
  resize: vertical;
}
</style>
