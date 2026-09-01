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

import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import { fileExtension, fileVisualKind } from "@/features/new-run/model";

const props = defineProps<{ artifact: Artifact; large?: boolean }>();
const kind = computed(() =>
  fileVisualKind(props.artifact.fileName, props.artifact.mediaType),
);
const extension = computed(() =>
  fileExtension(props.artifact.fileName, props.artifact.mediaType),
);
const icon = computed(() => {
  if (kind.value === "archive") return FileArchive;
  if (kind.value === "code") return FileCode2;
  if (kind.value === "image") return FileImage;
  if (kind.value === "spreadsheet") return FileSpreadsheet;
  if (kind.value === "document" || kind.value === "pdf") return FileText;
  return File;
});
</script>

<template>
  <span
    class="file-type-icon"
    :class="[`file-type-icon--${kind}`, { 'file-type-icon--large': large }]"
    :data-kind="kind"
    aria-hidden="true"
  >
    <component :is="icon" :size="large ? 26 : 18" />
    <span>{{ extension }}</span>
  </span>
</template>

<style scoped>
.file-type-icon {
  display: grid;
  width: 38px;
  height: 42px;
  flex: 0 0 38px;
  place-items: center;
  align-content: center;
  gap: 1px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text-secondary);
  background: var(--panel);
}
.file-type-icon span {
  max-width: 34px;
  overflow: hidden;
  font-family: var(--font-mono);
  font-size: 8px;
  line-height: 1;
  text-overflow: ellipsis;
  text-transform: uppercase;
  white-space: nowrap;
}
.file-type-icon--pdf {
  color: #a82e37;
  background: #fff1f2;
}
.file-type-icon--spreadsheet {
  color: #257247;
  background: #edf8f1;
}
.file-type-icon--image {
  color: #7b4c20;
  background: #fff7e8;
}
.file-type-icon--code,
.file-type-icon--archive {
  color: #5b4a91;
  background: #f4f0ff;
}
.file-type-icon--large {
  width: 54px;
  height: 60px;
  flex-basis: 54px;
}
.file-type-icon--large span {
  max-width: 48px;
  font-size: 9px;
}
:global([data-theme="dark"]) .file-type-icon--pdf {
  background: #3b2529;
}
:global([data-theme="dark"]) .file-type-icon--spreadsheet {
  background: #20352a;
}
:global([data-theme="dark"]) .file-type-icon--image {
  background: #3a3021;
}
:global([data-theme="dark"]) .file-type-icon--code,
:global([data-theme="dark"]) .file-type-icon--archive {
  background: #302a43;
}
</style>
