<script setup lang="ts">
import { Container, KeyRound, Plus, ServerCog } from "@lucide/vue";
import { computed } from "vue";
import {
  runtimeResourceCatalogPath,
  type RuntimeResourceScope,
} from "./resource-scope";

const props = defineProps<{
  resourceScope: RuntimeResourceScope;
  returnTo?: string;
}>();
const imagePath = computed(() =>
  runtimeResourceCatalogPath(props.resourceScope, "role-images"),
);
const secretPath = computed(() =>
  runtimeResourceCatalogPath(props.resourceScope, "secrets"),
);
const query = computed(() =>
  props.returnTo ? { returnTo: props.returnTo } : {},
);
</script>

<template>
  <nav
    class="runtime-resource-links"
    :aria-label="$t('assistant.resources.title')"
  >
    <RouterLink class="button" :to="{ path: imagePath, query }"
      ><Container :size="16" aria-hidden="true" />{{
        $t("assistant.resources.images")
      }}</RouterLink
    >
    <RouterLink class="button" :to="{ path: `${imagePath}/new`, query }"
      ><Plus :size="16" aria-hidden="true" />{{
        $t("assistant.resources.createImage")
      }}</RouterLink
    >
    <RouterLink class="button" :to="{ path: secretPath, query }"
      ><KeyRound :size="16" aria-hidden="true" />{{
        $t("assistant.resources.secrets")
      }}</RouterLink
    >
    <RouterLink
      v-if="resourceScope.kind === 'ORGANIZATION'"
      class="button"
      :to="{ path: '/organization/assistant/environment', query }"
      ><ServerCog :size="16" aria-hidden="true" />{{
        $t("assistant.resources.environment")
      }}</RouterLink
    >
  </nav>
</template>

<style scoped>
.runtime-resource-links {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
}
.runtime-resource-links .button {
  min-height: 32px;
  height: 32px;
  gap: 6px;
}
</style>
