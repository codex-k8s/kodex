<script setup lang="ts">
import { ArrowLeft } from "@lucide/vue";
import { computed } from "vue";
import { useRoute } from "vue-router";
import ProviderAccountsWorkspace from "@/features/providers/ProviderAccountsWorkspace.vue";
import ModelDiscoveryCatalog from "@/features/providers/ModelDiscoveryCatalog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
defineProps<{ personal?: boolean }>();
const route = useRoute();
const returnTo = computed(() => {
  const value = route.query.returnTo;
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    value.includes("\\")
  )
    return undefined;
  const url = new URL(value, window.location.origin);
  return url.origin === window.location.origin
    ? `${url.pathname}${url.search}${url.hash}`
    : undefined;
});
</script>

<template>
  <PageFrame
    :title="$t(personal ? 'providers.personalTitle' : 'providers.title')"
    :subtitle="
      $t(personal ? 'providers.personalSubtitle' : 'providers.subtitle')
    "
  >
    <template #actions
      ><RouterLink
        v-if="returnTo"
        class="icon-button"
        :to="returnTo"
        :aria-label="$t('vfs.back')"
        :title="$t('vfs.back')"
        ><ArrowLeft :size="18" /></RouterLink
    ></template>
    <ProviderAccountsWorkspace
      :key="personal ? 'personal' : 'shared'"
      :personal="personal"
    />
    <ModelDiscoveryCatalog v-if="!personal" />
  </PageFrame>
</template>
