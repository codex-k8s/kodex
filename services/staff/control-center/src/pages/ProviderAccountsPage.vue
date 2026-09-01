<script setup lang="ts">
import ProviderAccountsWorkspace from "@/features/providers/ProviderAccountsWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
</script>

<template>
  <PageFrame
    :title="$t('providers.title')"
    :subtitle="$t('providers.subtitle')"
  >
    <ProviderAccountsWorkspace />
  </PageFrame>
</template>
