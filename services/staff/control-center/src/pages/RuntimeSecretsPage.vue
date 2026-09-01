<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import RuntimeSecretsWorkspace from "@/features/runtime-secrets/RuntimeSecretsWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
</script>

<template>
  <PageFrame
    :title="$t('runtimeSecrets.title')"
    :subtitle="$t('runtimeSecrets.subtitle')"
  >
    <RuntimeSecretsWorkspace :project-ref="projectRef" />
  </PageFrame>
</template>
