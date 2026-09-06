<script setup lang="ts">
import OrganizationCatalog from "@/features/catalogs/OrganizationCatalog.vue";
import type { CatalogKind } from "@/features/catalogs/api";
import PageFrame from "@/shared/ui/PageFrame.vue";
defineProps<{ kind: CatalogKind }>();
</script>
<template>
  <PageFrame :title="$t(`catalog.${kind}`)"
    ><OrganizationCatalog :key="kind" :kind="kind"
  /></PageFrame>
</template>
