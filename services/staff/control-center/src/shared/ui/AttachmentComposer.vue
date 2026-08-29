<script setup lang="ts">
import { File, Paperclip, RotateCcw, Upload, X } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { asProblem } from "@/shared/api/problem";
import {
  attachmentAggregateLimitBytes,
  createAttachmentUploadQueue,
  formatAttachmentSize,
  type AttachmentComposerHandle,
  type AttachmentComposerState,
} from "@/shared/ui/attachment-composer";

const props = withDefaults(
  defineProps<{
    upload: (file: File) => Promise<{ ref: string }>;
    disabled?: boolean;
    compact?: boolean;
    reservedBytes?: number;
  }>(),
  { disabled: false, compact: false, reservedBytes: 0 },
);
const emit = defineEmits<{
  change: [state: AttachmentComposerState];
}>();

const { locale, t } = useI18n();
const input = ref<HTMLInputElement>();
const dragDepth = ref(0);
const queue = createAttachmentUploadQueue({
  upload: (file) => props.upload(file),
  disabled: () => props.disabled,
  reservedBytes: () => props.reservedBytes,
  formatError: (error) =>
    asProblem(error).detail || t("attachments.uploadFailed"),
});
const { items, state } = queue;
const dragActive = computed(() => dragDepth.value > 0);

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

function clear(): void {
  queue.clear();
  dragDepth.value = 0;
}

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

    <div v-if="items.length" class="attachment-composer__queue">
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

    <footer v-if="items.length" class="attachment-composer__summary">
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
}
</style>
