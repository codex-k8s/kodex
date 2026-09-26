<script setup lang="ts">
import type { RoleImageManagedLineage } from "@/shared/api/generated/openapi/types.gen";
defineProps<{ lineage?: RoleImageManagedLineage; collapsible?: boolean }>();
</script>
<template>
  <component
    :is="collapsible ? 'details' : 'div'"
    class="role-image-lineage"
    :class="{ 'role-image-lineage--collapsible': collapsible }"
  >
    <summary v-if="collapsible">
      <strong
        >{{ $t("roleImages.lineage") }}:
        {{ lineage?.managedBy ?? $t("common.unavailable") }}</strong
      >
      <span>{{ $t("roleImages.technicalDetails") }}</span>
    </summary>
    <strong v-else
      >{{ $t("roleImages.lineage") }}:
      {{ lineage?.managedBy ?? $t("common.unavailable") }}</strong
    >
    <div v-if="lineage" class="role-image-lineage__details">
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
    </div>
  </component>
</template>
<style scoped>
.role-image-lineage {
  display: grid;
  gap: 4px;
  min-width: 0;
  font-size: 0.76rem;
  overflow-wrap: anywhere;
}
.role-image-lineage summary {
  display: flex;
  cursor: pointer;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  list-style-position: inside;
}
.role-image-lineage summary span {
  color: var(--text-secondary);
  font-weight: 400;
}
.role-image-lineage__details {
  display: grid;
  gap: 4px;
  padding: 8px 0 0 18px;
}
</style>
