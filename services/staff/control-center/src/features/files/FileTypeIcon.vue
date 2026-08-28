<script setup lang="ts">
import {
  File,
  FileArchive,
  FileCode2,
  FileImage,
  FileSpreadsheet,
  FileText,
} from "@lucide/vue";
import { computed } from "vue";

import { fileVisual } from "@/features/files/model";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

const props = withDefaults(
  defineProps<{ artifact: Artifact; large?: boolean }>(),
  { large: false },
);
const visual = computed(() => fileVisual(props.artifact));
</script>

<template>
  <span
    class="file-type-icon"
    :class="[
      `file-type-icon--${visual.icon}`,
      { 'file-type-icon--large': large },
    ]"
    aria-hidden="true"
  >
    <FileText v-if="visual.icon === 'pdf' || visual.icon === 'document'" />
    <FileSpreadsheet v-else-if="visual.icon === 'spreadsheet'" />
    <FileImage v-else-if="visual.icon === 'image'" />
    <FileArchive v-else-if="visual.icon === 'archive'" />
    <FileCode2 v-else-if="visual.icon === 'code'" />
    <File v-else />
    <small>{{ visual.extension }}</small>
  </span>
</template>

<style scoped>
.file-type-icon {
  position: relative;
  display: inline-grid;
  flex: 0 0 38px;
  width: 38px;
  height: 38px;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 7px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.file-type-icon svg {
  width: 19px;
  height: 19px;
}
.file-type-icon small {
  position: absolute;
  right: 2px;
  bottom: 1px;
  max-width: 32px;
  overflow: hidden;
  color: currentColor;
  font-family: var(--font-mono);
  font-size: 0.48rem;
  font-weight: 600;
  line-height: 1;
  text-overflow: ellipsis;
  text-transform: uppercase;
  white-space: nowrap;
}
.file-type-icon--large {
  flex-basis: 64px;
  width: 64px;
  height: 64px;
}
.file-type-icon--large svg {
  width: 28px;
  height: 28px;
}
.file-type-icon--large small {
  right: 5px;
  bottom: 4px;
  max-width: 50px;
  font-size: 0.6rem;
}
.file-type-icon--pdf {
  color: #a32e2e;
  background: var(--danger-soft);
}
.file-type-icon--spreadsheet {
  color: var(--success);
  background: var(--success-soft);
}
.file-type-icon--archive {
  color: var(--warning);
  background: var(--warning-soft);
}
.file-type-icon--image {
  color: #7053a3;
  background: #f0ebf8;
}
@media (prefers-color-scheme: dark) {
  .file-type-icon--image {
    color: #c2a7ec;
    background: #302742;
  }
}
</style>
