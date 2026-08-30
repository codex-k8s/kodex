<script setup lang="ts">
import {
  File,
  FolderOpen,
  Link2,
  Paperclip,
  RotateCcw,
  Upload,
  X,
} from "@lucide/vue";
import { computed, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  createAttachmentArtifactLoader,
  type AttachmentArtifactPickerItem,
} from "@/shared/api/attachment-artifacts";
import { asProblem } from "@/shared/api/problem";
import AsyncEntityPicker, {
  type AsyncEntityPickerLabels,
} from "@/shared/ui/AsyncEntityPicker.vue";
import {
  attachmentAggregateLimitBytes,
  attachmentComposerState,
  createAttachmentUploadQueue,
  formatAttachmentSize,
  type AttachmentComposerHandle,
  type AttachmentComposerState,
  type ExistingAttachmentSelection,
  type AttachmentUploadRequest,
} from "@/shared/ui/attachment-composer";
import type { AsyncEntityLoader } from "@/shared/ui/async-entity-picker";

const props = withDefaults(
  defineProps<{
    upload: (
      file: File,
      request: AttachmentUploadRequest,
    ) => Promise<{ ref: string }>;
    disabled?: boolean;
    compact?: boolean;
    reservedBytes?: number;
    projectRef?: string;
    loadExisting?: AsyncEntityLoader<AttachmentArtifactPickerItem>;
  }>(),
  { disabled: false, compact: false, reservedBytes: 0 },
);
const emit = defineEmits<{
  change: [state: AttachmentComposerState];
}>();

const { locale, t } = useI18n();
const input = ref<HTMLInputElement>();
const dragDepth = ref(0);
const existingPickerOpen = ref(false);
const existingRefs = ref<string[]>([]);
const knownExisting = shallowRef(
  new Map<string, AttachmentArtifactPickerItem>(),
);
const queue = createAttachmentUploadQueue({
  upload: (file, request) => props.upload(file, request),
  disabled: () => props.disabled,
  reservedBytes: () => props.reservedBytes,
  formatError: (error) =>
    asProblem(error).detail || t("attachments.uploadFailed"),
});
const { items, state: uploadState } = queue;
const dragActive = computed(() => dragDepth.value > 0);
const existingLoader = computed(() => {
  if (props.loadExisting) return props.loadExisting;
  return props.projectRef
    ? createAttachmentArtifactLoader(props.projectRef)
    : undefined;
});
const existingSelections = computed<ExistingAttachmentSelection[]>(() =>
  existingRefs.value.flatMap((reference) => {
    const item = knownExisting.value.get(reference);
    return item
      ? [
          {
            mediaType: item.artifact.mediaType,
            name: item.artifact.fileName,
            ref: item.artifact.ref,
            size: item.artifact.sizeBytes,
          },
        ]
      : [];
  }),
);
const state = computed(() =>
  attachmentComposerState(uploadState.value, existingSelections.value),
);
const existingPickerLabels = computed<AsyncEntityPickerLabels>(() => ({
  label: t("attachments.existing.label"),
  searchPlaceholder: t("attachments.existing.search"),
  loading: t("attachments.existing.loading"),
  loadingMore: t("attachments.existing.loadingMore"),
  empty: t("attachments.existing.empty"),
  error: t("attachments.existing.error"),
  retry: t("common.retry"),
}));

watch(state, (value) => emit("change", { ...value, refs: [...value.refs] }), {
  immediate: true,
});
watch(
  () => props.disabled,
  (disabled) => {
    if (!disabled) queue.process();
  },
);
watch(
  () => props.projectRef,
  (next, previous) => {
    if (next === previous) return;
    existingPickerOpen.value = false;
    existingRefs.value = [];
    knownExisting.value = new Map();
  },
);
watch(
  () => props.reservedBytes,
  () => queue.process(),
);

function enqueue(files: Iterable<File>): void {
  queue.enqueue(files);
}

function handleInput(event: Event): void {
  const target = event.target as HTMLInputElement;
  if (target.files) enqueue(target.files);
  target.value = "";
}

function handleDragEnter(): void {
  if (!props.disabled) dragDepth.value += 1;
}

function handleDragLeave(): void {
  dragDepth.value = Math.max(0, dragDepth.value - 1);
}

function handleDrop(event: DragEvent): void {
  dragDepth.value = 0;
  if (!props.disabled && event.dataTransfer?.files)
    enqueue(event.dataTransfer.files);
}

function remove(key: string): void {
  queue.remove(key);
}

function retry(key: string): void {
  queue.retry(key);
}

function updateExistingRefs(value: string | null | readonly string[]): void {
  if (value === null || typeof value === "string") return;
  existingRefs.value = [...value];
}

function rememberExisting(item: AttachmentArtifactPickerItem): void {
  const next = new Map(knownExisting.value);
  next.set(item.artifact.ref, item);
  knownExisting.value = next;
}

function detachExisting(reference: string): void {
  existingRefs.value = existingRefs.value.filter(
    (candidate) => candidate !== reference,
  );
}

function clear(): void {
  queue.clear();
  dragDepth.value = 0;
  existingPickerOpen.value = false;
  existingRefs.value = [];
  knownExisting.value = new Map();
}

onBeforeUnmount(clear);

defineExpose<AttachmentComposerHandle>({ clear });
</script>

<template>
  <section
    class="attachment-composer"
    :class="{
      'attachment-composer--compact': compact,
      'attachment-composer--dragging': dragActive,
      'attachment-composer--invalid': state.overLimit,
    }"
    @dragenter.prevent="handleDragEnter"
    @dragover.prevent
    @dragleave.prevent="handleDragLeave"
    @drop.prevent="handleDrop"
  >
    <input
      ref="input"
      class="sr-only"
      type="file"
      multiple
      :disabled="disabled"
      @change="handleInput"
    />

    <button
      class="attachment-composer__picker"
      type="button"
      :disabled="disabled"
      @click="input?.click()"
    >
      <Paperclip :size="17" aria-hidden="true" />
      <span>{{ t("attachments.add") }}</span>
      <small>{{ t("attachments.dropHint") }}</small>
    </button>

    <button
      v-if="existingLoader"
      class="attachment-composer__existing-toggle"
      type="button"
      :disabled="disabled"
      :aria-expanded="existingPickerOpen"
      @click="existingPickerOpen = !existingPickerOpen"
    >
      <FolderOpen :size="17" aria-hidden="true" />
      <span>{{ t("attachments.existing.choose") }}</span>
    </button>

    <div
      v-if="existingPickerOpen && existingLoader"
      class="attachment-composer__existing-picker"
    >
      <header>
        <strong>{{ t("attachments.existing.title") }}</strong>
        <span>{{ t("attachments.existing.hint") }}</span>
      </header>
      <AsyncEntityPicker
        :model-value="existingRefs"
        :load-items="existingLoader"
        :labels="existingPickerLabels"
        :disabled="disabled"
        multiple
        @update:model-value="updateExistingRefs"
        @select="rememberExisting"
      >
        <template #option="{ item }">
          <File :size="17" aria-hidden="true" />
          <span class="attachment-composer__existing-copy">
            <strong>{{ item.artifact.fileName }}</strong>
            <small>
              {{ item.artifact.mediaType }} ·
              {{ formatAttachmentSize(item.artifact.sizeBytes, locale) }}
            </small>
          </span>
        </template>
      </AsyncEntityPicker>
    </div>

    <div
      v-if="existingSelections.length || items.length"
      class="attachment-composer__queue"
    >
      <article
        v-for="item in existingSelections"
        :key="`existing:${item.ref}`"
        class="attachment-composer__item attachment-composer__item--existing"
      >
        <Link2 :size="17" aria-hidden="true" />
        <span class="attachment-composer__copy">
          <strong :title="item.name">{{ item.name }}</strong>
          <small>
            {{ formatAttachmentSize(item.size, locale) }} ·
            {{ t("attachments.existing.attached") }}
          </small>
        </span>
        <span class="attachment-composer__detach-hint">
          {{ t("attachments.existing.detachHint") }}
        </span>
        <button
          class="icon-button"
          type="button"
          :aria-label="t('attachments.detach', { name: item.name })"
          :title="t('attachments.detach', { name: item.name })"
          @click="detachExisting(item.ref)"
        >
          <X :size="15" aria-hidden="true" />
        </button>
      </article>
      <article
        v-for="item in items"
        :key="item.key"
        class="attachment-composer__item"
        :class="`attachment-composer__item--${item.state.toLowerCase()}`"
      >
        <File :size="17" aria-hidden="true" />
        <span class="attachment-composer__copy">
          <strong :title="item.name">{{ item.name }}</strong>
          <small>
            {{ formatAttachmentSize(item.size, locale) }} ·
            {{ t(`attachments.states.${item.state}`) }}
          </small>
          <span v-if="item.error" class="attachment-composer__error">
            {{ item.error }}
          </span>
        </span>
        <progress
          v-if="item.state === 'UPLOADING'"
          class="attachment-composer__progress"
          :value="item.progress?.loadedBytes"
          :max="item.progress?.totalBytes"
          :aria-label="t('attachments.uploading', { name: item.name })"
        />
        <Upload
          v-else-if="item.state === 'UPLOADED'"
          :size="16"
          class="attachment-composer__ready"
          aria-hidden="true"
        />
        <button
          v-if="item.state === 'FAILED'"
          class="icon-button"
          type="button"
          :aria-label="t('attachments.retry', { name: item.name })"
          :title="t('attachments.retry', { name: item.name })"
          @click="retry(item.key)"
        >
          <RotateCcw :size="15" aria-hidden="true" />
        </button>
        <button
          class="icon-button"
          type="button"
          :aria-label="t('attachments.remove', { name: item.name })"
          :title="t('attachments.remove', { name: item.name })"
          @click="remove(item.key)"
        >
          <X :size="15" aria-hidden="true" />
        </button>
      </article>
    </div>

    <footer v-if="state.count" class="attachment-composer__summary">
      <span>
        {{
          t("attachments.progress", {
            uploaded: state.uploadedCount,
            count: state.count,
          })
        }}
      </span>
      <span>
        {{ formatAttachmentSize(state.totalBytes, locale) }} /
        {{ formatAttachmentSize(attachmentAggregateLimitBytes, locale) }}
      </span>
    </footer>
    <p v-if="state.overLimit" class="attachment-composer__limit" role="alert">
      {{ t("attachments.aggregateLimit") }}
    </p>
    <div v-if="dragActive" class="attachment-composer__drop">
      <Upload :size="24" aria-hidden="true" />
      <strong>{{ t("attachments.drop") }}</strong>
    </div>
  </section>
</template>

<style scoped>
.attachment-composer {
  position: relative;
  display: grid;
  min-width: 0;
  gap: 8px;
  padding: 10px;
  border: 1px dashed var(--border);
  border-radius: 7px;
  background: var(--surface);
}
.attachment-composer--compact {
  padding: 7px;
}
.attachment-composer--dragging {
  border-color: var(--accent);
}
.attachment-composer--invalid {
  border-color: var(--danger);
}
.attachment-composer__picker {
  display: flex;
  min-height: 38px;
  align-items: center;
  gap: 7px;
  padding: 7px 10px;
  border: 0;
  border-radius: 6px;
  background: var(--panel);
  color: var(--text);
  cursor: pointer;
}
.attachment-composer__existing-toggle {
  display: inline-flex;
  min-height: 34px;
  width: fit-content;
  align-items: center;
  gap: 7px;
  padding: 6px 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  cursor: pointer;
}
.attachment-composer__existing-toggle:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
.attachment-composer__existing-picker {
  display: grid;
  min-height: 0;
  gap: 8px;
  padding: 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
}
.attachment-composer__existing-picker > header,
.attachment-composer__existing-copy {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.attachment-composer__existing-picker > header span,
.attachment-composer__existing-copy small,
.attachment-composer__detach-hint {
  color: var(--text-secondary);
  font-size: 12px;
}
.attachment-composer__picker small {
  margin-left: auto;
  color: var(--text-secondary);
}
.attachment-composer__queue {
  display: grid;
  max-height: 220px;
  gap: 6px;
  overflow: auto;
}
.attachment-composer__item {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 7px;
  padding: 7px 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
}
.attachment-composer__item--failed {
  border-color: var(--danger);
}
.attachment-composer__item--existing {
  grid-template-columns: auto minmax(0, 1fr) minmax(0, auto) auto;
}
.attachment-composer__copy {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.attachment-composer__copy strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.attachment-composer__copy small,
.attachment-composer__summary {
  color: var(--text-secondary);
  font-size: 12px;
}
.attachment-composer__error,
.attachment-composer__limit {
  color: var(--danger);
  font-size: 12px;
}
.attachment-composer__progress {
  width: 64px;
}
.attachment-composer__ready {
  color: var(--success);
}
.attachment-composer__summary {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.attachment-composer__limit {
  margin: 0;
}
.attachment-composer__drop {
  position: absolute;
  z-index: 2;
  inset: 0;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 6px;
  border-radius: inherit;
  background: color-mix(in srgb, var(--accent-soft) 92%, transparent);
  color: var(--accent);
  pointer-events: none;
}
@media (max-width: 720px) {
  .attachment-composer__picker small {
    display: none;
  }
  .attachment-composer__progress {
    width: 42px;
  }
  .attachment-composer__detach-hint {
    display: none;
  }
}
</style>
