<script setup lang="ts">
import { AlertTriangle } from "@lucide/vue";

import type { AppProblem } from "@/shared/api/problem";

defineProps<{ problem: AppProblem | null }>();
</script>

<template>
  <div v-if="problem" class="notice notice--danger" role="alert">
    <AlertTriangle :size="18" aria-hidden="true" />
    <span>{{ $t("common.errorCode", { code: problem.code }) }}</span>
    <span v-if="problem.correlationId">{{
      $t("common.correlation", { id: problem.correlationId })
    }}</span>
  </div>
</template>
