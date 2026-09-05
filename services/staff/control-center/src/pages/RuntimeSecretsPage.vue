<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";

import RuntimeSecretsWorkspace from "@/features/runtime-secrets/RuntimeSecretsWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const router = useRouter();
const initialDraftRef = computed(() =>
  typeof route.query.draftRef === "string" ? route.query.draftRef : undefined,
);
function rememberDraft(draftRef: string): void {
  void router.replace({
    query: {
      ...route.query,
      draftRef,
      ...(initialDraftRef.value !== draftRef ? { planRef: undefined } : {}),
    },
  });
}
const initialPlanRef = computed(() =>
  typeof route.query.planRef === "string" ? route.query.planRef : undefined,
);
function rememberPlan(draftRef: string, planRef: string): void {
  void router.replace({ query: { ...route.query, draftRef, planRef } });
}
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
      :initial-draft-ref="initialDraftRef"
      :initial-plan-ref="initialPlanRef"
      @draft-saved="rememberDraft"
      @plan-prepared="rememberPlan"
    />
  </PageFrame>
</template>
