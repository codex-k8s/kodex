<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";

import RuntimeSecretsWorkspace from "@/features/runtime-secrets/RuntimeSecretsWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import { usePlatformStore } from "@/features/platform/store";
import {
  organizationRuntimeResourceScope,
  runtimeResourceScopeKey,
} from "@/features/runtime/resource-scope";

const route = useRoute();
const router = useRouter();
const platform = usePlatformStore();
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
const projectRef = computed(() =>
  typeof route.params.projectRef === "string"
    ? route.params.projectRef
    : undefined,
);
const organizationScope = computed(() =>
  !projectRef.value
    ? organizationRuntimeResourceScope(platform.bootstrap)
    : undefined,
);
const scopeKey = computed(
  () =>
    projectRef.value ??
    (organizationScope.value
      ? runtimeResourceScopeKey(organizationScope.value)
      : "UNAVAILABLE"),
);
const initialSecretRef = computed(() =>
  typeof route.query.secretRef === "string" ? route.query.secretRef : undefined,
);
</script>

<template>
  <PageFrame
    :title="
      projectRef
        ? $t('runtimeSecrets.title')
        : $t('assistant.resources.organizationSecrets')
    "
    :subtitle="
      organizationScope ? $t('assistant.resources.organizationHelp') : undefined
    "
  >
    <RuntimeSecretsWorkspace
      v-if="projectRef || organizationScope"
      :key="scopeKey"
      :project-ref="projectRef"
      :organization-scope="organizationScope"
      :initial-secret-ref="initialSecretRef"
      :initial-draft-ref="initialDraftRef"
      :initial-plan-ref="initialPlanRef"
      @draft-saved="rememberDraft"
      @plan-prepared="rememberPlan"
    />
  </PageFrame>
</template>
