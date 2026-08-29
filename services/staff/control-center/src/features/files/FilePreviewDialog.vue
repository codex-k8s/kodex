<script setup lang="ts">
import { Download, RotateCcw, Search, ShieldCheck, Trash2 } from "@lucide/vue";
import { computed, ref, watch } from "vue";

import FileTypeIcon from "@/features/files/FileTypeIcon.vue";
import type { FilePreviewLabels } from "@/features/files/model";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  artifact: Artifact;
  imageUrl?: string;
  labels: FilePreviewLabels;
  deleteLabel: string;
  lifecycleAction?: "DELETE" | "RESTORE";
  loading?: boolean;
  previewText?: string;
  unavailable?: boolean;
  formatBytes: (value: number) => string;
  formatDate: (value: string) => string;
  sourceLabel: (source: Artifact["source"]) => string;
}>();
const emit = defineEmits<{
  close: [];
  download: [];
  requestDelete: [];
}>();
const find = ref("");
const zoom = ref(100);

watch(
  () => props.artifact.ref,
  () => {
    find.value = "";
    zoom.value = 100;
  },
);

const textChunks = computed(() => {
  const text = props.previewText ?? "";
  const query = find.value.trim();
  if (!query) return [{ match: false, value: text }];
  const lowerText = text.toLocaleLowerCase();
  const lowerQuery = query.toLocaleLowerCase();
  const chunks: Array<{ match: boolean; value: string }> = [];
  let offset = 0;
  while (offset < text.length) {
    const index = lowerText.indexOf(lowerQuery, offset);
    if (index < 0) {
      chunks.push({ match: false, value: text.slice(offset) });
      break;
    }
    if (index > offset)
      chunks.push({ match: false, value: text.slice(offset, index) });
    chunks.push({
      match: true,
      value: text.slice(index, index + query.length),
    });
    offset = index + query.length;
  }
  return chunks;
});
</script>

<template>
  <ModalDialog
    :title="artifact.fileName"
    :busy="loading"
    size="full"
    @close="emit('close')"
  >
    <div class="file-preview-dialog">
      <section class="file-preview-dialog__viewer">
        <header class="file-preview-dialog__toolbar">
          <span class="file-preview-dialog__safe">
            <ShieldCheck :size="16" aria-hidden="true" />
            {{ labels.protectedPreview }}
          </span>
          <label v-if="previewText" class="file-preview-dialog__search">
            <Search :size="15" aria-hidden="true" />
            <span class="sr-only">{{ labels.find }}</span>
            <input v-model="find" type="search" :placeholder="labels.find" />
          </label>
          <label v-else-if="imageUrl" class="file-preview-dialog__zoom">
            <span>{{ labels.zoom }}</span>
            <input v-model="zoom" type="range" min="50" max="180" step="10" />
            <output>{{ zoom }}%</output>
          </label>
        </header>
        <div class="file-preview-dialog__content">
          <span v-if="loading" class="file-preview-dialog__state" role="status">
            {{ labels.loading }}
          </span>
          <pre v-else-if="previewText" tabindex="0"><template
              v-for="(chunk, index) in textChunks"
              :key="index"
            ><mark v-if="chunk.match">{{ chunk.value }}</mark><template v-else>{{ chunk.value }}</template></template></pre>
          <div v-else-if="imageUrl" class="file-preview-dialog__image-wrap">
            <img
              :src="imageUrl"
              :alt="artifact.fileName"
              :style="{ width: `${zoom}%` }"
            />
          </div>
          <div v-else class="file-preview-dialog__state">
            <FileTypeIcon :artifact="artifact" large />
            <p>{{ labels.unavailable }}</p>
          </div>
        </div>
      </section>

      <aside class="file-preview-dialog__details">
        <div class="file-preview-dialog__identity">
          <FileTypeIcon :artifact="artifact" large />
          <div>
            <h3>{{ artifact.fileName }}</h3>
            <StatusBadge :state="artifact.scanState" />
          </div>
        </div>
        <dl>
          <div>
            <dt>{{ labels.size }}</dt>
            <dd>{{ formatBytes(artifact.sizeBytes) }}</dd>
          </div>
          <div>
            <dt>{{ labels.version }}</dt>
            <dd class="mono">v{{ artifact.revision }}</dd>
          </div>
          <div>
            <dt>{{ labels.source }}</dt>
            <dd>{{ sourceLabel(artifact.source) }}</dd>
          </div>
          <div>
            <dt>{{ labels.added }}</dt>
            <dd>{{ formatDate(artifact.createdAt) }}</dd>
          </div>
        </dl>
      </aside>
    </div>
    <template #actions>
      <button
        class="button"
        :class="lifecycleAction === 'RESTORE' ? '' : 'button--danger'"
        type="button"
        :disabled="loading"
        @click="emit('requestDelete')"
      >
        <RotateCcw
          v-if="lifecycleAction === 'RESTORE'"
          :size="16"
          aria-hidden="true"
        />
        <Trash2 v-else :size="16" aria-hidden="true" />
        {{ deleteLabel }}
      </button>
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="emit('close')"
      >
        {{ labels.close }}
      </button>
      <button
        v-if="artifact.nextActions.includes('DOWNLOAD')"
        class="button button--primary"
        type="button"
        :disabled="loading"
        @click="emit('download')"
      >
        <Download :size="16" aria-hidden="true" />
        {{ labels.download }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.file-preview-dialog {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(220px, 260px);
  width: calc(100% + 40px);
  height: calc(100% + 40px);
  min-height: 0;
  margin: -20px;
}
.file-preview-dialog__viewer {
  display: flex;
  min-width: 0;
  flex-direction: column;
  border-right: 1px solid var(--border);
  background: var(--canvas);
}
.file-preview-dialog__toolbar {
  display: flex;
  min-height: 48px;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.file-preview-dialog__safe,
.file-preview-dialog__zoom {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 0.8rem;
}
.file-preview-dialog__safe svg {
  color: var(--success);
}
.file-preview-dialog__search {
  display: flex;
  width: min(260px, 45%);
  align-items: center;
  gap: 7px;
  margin-left: auto;
  padding: 0 9px;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  background: var(--surface);
}
.file-preview-dialog__search input {
  width: 100%;
  min-height: 32px;
  padding: 0;
  border: 0;
  outline: 0;
}
.file-preview-dialog__zoom {
  margin-left: auto;
}
.file-preview-dialog__zoom input {
  width: 120px;
}
.file-preview-dialog__zoom output {
  width: 42px;
  font-family: var(--font-mono);
}
.file-preview-dialog__content {
  display: flex;
  min-height: 0;
  flex: 1;
  align-items: stretch;
  justify-content: center;
  overflow: auto;
  padding: 20px;
}
.file-preview-dialog__content pre {
  width: min(720px, 100%);
  min-height: 100%;
  padding: 28px;
  margin: 0;
  border: 1px solid var(--border);
  background: var(--surface);
  box-shadow: 0 8px 24px rgb(16 22 30 / 9%);
  font-family: var(--font-mono);
  font-size: 0.84rem;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
.file-preview-dialog__content mark {
  color: inherit;
  background: #ffe89a;
}
.file-preview-dialog__image-wrap {
  width: 100%;
  overflow: auto;
  text-align: center;
}
.file-preview-dialog__image-wrap img {
  display: inline-block;
  max-width: none;
  height: auto;
  border: 1px solid var(--border);
  background: var(--surface);
}
.file-preview-dialog__state {
  display: grid;
  max-width: 420px;
  place-items: center;
  align-content: center;
  gap: 14px;
  color: var(--muted);
  text-align: center;
}
.file-preview-dialog__details {
  padding: 18px;
  overflow: auto;
  background: var(--surface);
}
.file-preview-dialog__identity {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding-bottom: 18px;
  border-bottom: 1px solid var(--border);
}
.file-preview-dialog__identity h3 {
  margin: 0 0 8px;
  overflow-wrap: anywhere;
  font-size: 1rem;
}
.file-preview-dialog__details dl {
  margin: 0;
}
.file-preview-dialog__details dl div {
  padding: 12px 0;
  border-bottom: 1px solid var(--hairline);
}
.file-preview-dialog__details dt {
  color: var(--subtle);
  font-size: 0.74rem;
}
.file-preview-dialog__details dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
@media (max-width: 760px) {
  .file-preview-dialog {
    display: block;
    width: auto;
    min-height: 60vh;
  }
  .file-preview-dialog__viewer {
    min-height: 54vh;
    border-right: 0;
  }
  .file-preview-dialog__details {
    display: none;
  }
  .file-preview-dialog__safe {
    display: none;
  }
  .file-preview-dialog__search {
    width: 100%;
  }
}
</style>
