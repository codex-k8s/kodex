<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import ConfigurationEditor from "@/features/managed-configurations/ConfigurationEditor.vue";
import type { ConfigurationKind } from "@/features/managed-configurations/api";
import type { ManagedConfiguration } from "@/shared/api/generated/openapi/types.gen";
import PageFrame from "@/shared/ui/PageFrame.vue";
const route = useRoute();
const router = useRouter();
const kinds: readonly ConfigurationKind[] = [
  "PROMPT_TEMPLATE",
  "ROLE_IMAGE",
  "INTEGRATION_DEFINITION",
  "SYSTEM_STT",
];
const kind = computed(() => kinds.find((value) => value === route.params.kind));
const configurationRef = computed(() =>
  typeof route.params.configurationRef === "string" &&
  route.params.configurationRef !== "new"
    ? route.params.configurationRef
    : undefined,
);
const projectRef = computed(() =>
  typeof route.query.projectRef === "string"
    ? route.query.projectRef
    : undefined,
);
function created(configuration: ManagedConfiguration): void {
  void router.replace({
    name: "configuration",
    params: { kind: configuration.kind, configurationRef: configuration.ref },
    query: configuration.projectRef
      ? { projectRef: configuration.projectRef }
      : {},
  });
}
</script>
<template>
  <PageFrame :title="kind ? $t(`managed.kinds.${kind}`) : $t('managed.title')">
    <ConfigurationEditor
      v-if="kind"
      :key="route.fullPath"
      :kind="kind"
      :configuration-ref="configurationRef"
      :project-ref="projectRef"
      @created="created"
    />
    <p v-else role="alert">{{ $t("errors.NOT_FOUND") }}</p>
  </PageFrame>
</template>
