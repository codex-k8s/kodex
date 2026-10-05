<script setup lang="ts">
import type { RoleImageAdmissionFailure } from "@/shared/api/generated/openapi/types.gen";

defineProps<{ failure: RoleImageAdmissionFailure }>();
</script>

<template>
  <section class="admission-failure" role="alert">
    <strong>{{ $t("roleImages.admissionFailed") }}</strong>
    <p>{{ $t(`roleImages.admissionFailureReasons.${failure.errorCode}`) }}</p>
    <p>{{ $t("roleImages.admissionFailureHelp") }}</p>
    <details>
      <summary>{{ $t("roleImages.technicalDetails") }}</summary>
      <code>{{ failure.errorCode }}</code>
    </details>
  </section>
</template>

<style scoped>
.admission-failure {
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--danger);
  border-radius: 8px;
  background: var(--danger-soft);
  color: var(--danger);
  font-size: 13px;
  overflow-wrap: anywhere;
}
.admission-failure p {
  margin: 4px 0;
}
.admission-failure details {
  margin-top: 8px;
  font-size: 12px;
}
</style>
