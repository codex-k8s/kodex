<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import RuntimeSecretsWorkspace from "@/features/runtime-secrets/RuntimeSecretsWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const initialSecretRef = computed(() =>
  typeof route.query.secretRef === "string" ? route.query.secretRef : undefined,
);
</script>

<template>
  <PageFrame :title="$t('runtimeSecrets.title')">
    <RuntimeSecretsWorkspace
      :key="projectRef"
      :project-ref="projectRef"
      :initial-secret-ref="initialSecretRef"
    />
  </PageFrame>
</template>
