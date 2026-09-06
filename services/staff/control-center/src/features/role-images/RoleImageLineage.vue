<script setup lang="ts">
import type { RoleImageManagedLineage } from "@/shared/api/generated/openapi/types.gen";
defineProps<{ lineage?: RoleImageManagedLineage }>();
</script>
<template>
  <div class="role-image-lineage">
    <strong
      >{{ $t("roleImages.lineage") }}:
      {{ lineage?.managedBy ?? $t("common.unavailable") }}</strong
    >
    <template v-if="lineage">
      <span
        >{{ lineage.sourceRef
        }}<template v-if="lineage.sourceRevision">
          · {{ lineage.sourceRevision }}</template
        ></span
      >
      <RouterLink
        v-if="lineage.configurationRef"
        :to="`/configurations/ROLE_IMAGE/${encodeURIComponent(lineage.configurationRef)}`"
      >
        {{ $t("roleImages.configuration") }} · {{ lineage.configurationRef }}
      </RouterLink>
      <span v-if="lineage.revisionRef"
        >{{ lineage.revisionRef }} · {{ lineage.revision }}</span
      >
    </template>
  </div>
</template>
<style scoped>
.role-image-lineage {
  display: grid;
  gap: 4px;
  min-width: 0;
  font-size: 0.76rem;
  overflow-wrap: anywhere;
}
</style>
