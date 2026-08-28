<script setup lang="ts">
import { Files, X } from "@lucide/vue";
import { computed, nextTick, ref, shallowRef, watch } from "vue";

import FileTypeIcon from "@/features/new-run/components/FileTypeIcon.vue";
import {
  formatFileSize,
  formatTimestamp,
  type ArtifactPickerItem,
} from "@/features/new-run/model";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import AsyncEntityPicker, {
  type AsyncEntityPickerLabels,
} from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityLoader } from "@/shared/ui/async-entity-picker";
import OverlayPanel from "@/shared/ui/OverlayPanel.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import ViewModeToggle from "@/shared/ui/ViewModeToggle.vue";
import type { ViewMode } from "@/shared/ui/view-mode-toggle";

export interface NewRunFilePickerLabels {
  title: string;
  subtitle: string;
  close: string;
  cancel: string;
  confirm: string;
  selected: string;
  selectedCount: (count: number) => string;
  remove: (name: string) => string;
  viewMode: string;
  listView: string;
  gridView: string;
  unavailable: string;
  revision: (revision: number) => string;
  scanStates: Record<Artifact["scanState"], string>;
  picker: AsyncEntityPickerLabels;
}

const props = defineProps<{
  open: boolean;
  selectedArtifacts: readonly Artifact[];
  loadItems: AsyncEntityLoader<ArtifactPickerItem>;
  labels: NewRunFilePickerLabels;
  locale: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  close: [];
  confirm: [artifacts: Artifact[]];
}>();

const maximumFiles = 50;
const viewMode = ref<ViewMode>("list");
const draftRefs = ref<string[]>([]);
const knownArtifacts = shallowRef(new Map<string, Artifact>());
const selectedItems = computed(() =>
  draftRefs.value.flatMap((reference) => {
    const artifact = knownArtifacts.value.get(reference);
    return artifact ? [artifact] : [];
  }),
);

function seedSelection(): void {
  knownArtifacts.value = new Map(
    props.selectedArtifacts.map((artifact) => [artifact.ref, artifact]),
  );
  draftRefs.value = props.selectedArtifacts.map((artifact) => artifact.ref);
}

function updateDraft(value: string | null | readonly string[]): void {
  if (value === null || typeof value === "string") return;
  draftRefs.value = value.slice(0, maximumFiles);
}

function remember(item: ArtifactPickerItem): void {
  const next = new Map(knownArtifacts.value);
  next.set(item.artifact.ref, item.artifact);
  knownArtifacts.value = next;
}

function remove(reference: string): void {
  draftRefs.value = draftRefs.value.filter((item) => item !== reference);
}

function confirm(): void {
  emit("confirm", selectedItems.value);
}

function requestClose(open: boolean): void {
  if (!open) emit("close");
}

function scanTone(
  state: Artifact["scanState"],
): "danger" | "neutral" | "success" | "warning" {
  if (state === "CLEAN") return "success";
  if (state === "QUARANTINED" || state === "FAILED") return "danger";
  if (state === "SCANNING") return "warning";
  return "neutral";
}

watch(
  () => [props.open, props.selectedArtifacts] as const,
  async ([open]) => {
    if (!open) return;
    seedSelection();
    await nextTick();
    if (typeof document === "undefined") return;
    document
      .querySelector<HTMLInputElement>(
        ".new-run-file-picker__overlay input[type='search']",
      )
      ?.focus();
  },
  { immediate: true },
);
</script>

<template>
  <OverlayPanel
    class="new-run-file-picker__overlay"
    :open="open"
    mode="modal"
    :ariaLabel="labels.title"
    :close-label="labels.close"
    @update:open="requestClose"
  >
    <template #header>
      <div class="new-run-file-picker__heading">
        <h2>{{ labels.title }}</h2>
        <p>{{ labels.subtitle }}</p>
      </div>
    </template>

    <div class="new-run-file-picker">
      <div class="new-run-file-picker__toolbar">
        <span aria-live="polite">{{
          labels.selectedCount(draftRefs.length)
        }}</span>
        <ViewModeToggle
          v-model="viewMode"
          :ariaLabel="labels.viewMode"
          :list-label="labels.listView"
          :grid-label="labels.gridView"
        />
      </div>

      <AsyncEntityPicker
        class="new-run-file-picker__picker"
        :class="`new-run-file-picker__picker--${viewMode}`"
        :model-value="draftRefs"
        :load-items="loadItems"
        :labels="labels.picker"
        :disabled="disabled"
        multiple
        @update:model-value="updateDraft"
        @select="remember"
      >
        <template #option="{ item }">
          <FileTypeIcon
            :artifact="item.artifact"
            :large="viewMode === 'grid'"
          />
          <span class="new-run-file-picker__file-copy">
            <strong>{{ item.artifact.fileName }}</strong>
            <span class="new-run-file-picker__metadata">
              <span>{{ formatFileSize(item.artifact.sizeBytes, locale) }}</span>
              <span>{{ labels.revision(item.artifact.revision) }}</span>
              <span>{{
                formatTimestamp(item.artifact.createdAt, locale)
              }}</span>
            </span>
            <span v-if="item.disabled" class="new-run-file-picker__unavailable">
              {{ labels.unavailable }}
            </span>
          </span>
          <StatusBadge
            :state="item.artifact.scanState"
            :label="labels.scanStates[item.artifact.scanState]"
            :tone="scanTone(item.artifact.scanState)"
          />
        </template>
      </AsyncEntityPicker>

      <div class="new-run-file-picker__selection" aria-live="polite">
        <strong>{{ labels.selected }}</strong>
        <div v-if="selectedItems.length" class="new-run-file-picker__chips">
          <span
            v-for="artifact in selectedItems"
            :key="artifact.ref"
            class="new-run-file-picker__chip"
          >
            <span :title="artifact.fileName">{{ artifact.fileName }}</span>
            <button
              type="button"
              :aria-label="labels.remove(artifact.fileName)"
              :title="labels.remove(artifact.fileName)"
              @click="remove(artifact.ref)"
            >
              <X :size="14" aria-hidden="true" />
            </button>
          </span>
        </div>
        <span v-else class="new-run-file-picker__selection-empty">
          {{ labels.picker.empty }}
        </span>
      </div>
    </div>

    <template #footer>
      <button
        class="button new-run-file-picker__action"
        type="button"
        @click="emit('close')"
      >
        {{ labels.cancel }}
      </button>
      <button
        class="button button--primary new-run-file-picker__action"
        type="button"
        :disabled="disabled"
        @click="confirm"
      >
        <Files :size="17" aria-hidden="true" />
        {{ labels.confirm }}
      </button>
    </template>
  </OverlayPanel>
</template>

<style scoped>
.new-run-file-picker__heading {
  min-width: 0;
}
.new-run-file-picker__heading h2 {
  margin: 0;
  font-size: 16px;
}
.new-run-file-picker__heading p {
  margin: 3px 0 0;
  color: var(--text-secondary);
  font-size: 12px;
}
.new-run-file-picker {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
}
.new-run-file-picker__toolbar {
  display: flex;
  min-height: 48px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 12px;
}
.new-run-file-picker__picker {
  min-height: 0;
  flex: 1;
  border: 0;
  border-radius: 0;
}
.new-run-file-picker__picker :deep(.async-picker__option) {
  min-height: 64px;
}
.new-run-file-picker__file-copy {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 3px;
}
.new-run-file-picker__file-copy strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.new-run-file-picker__metadata {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 4px 10px;
  color: var(--text-secondary);
  font-size: 12px;
}
.new-run-file-picker__unavailable {
  color: var(--danger);
  font-size: 12px;
}
.new-run-file-picker__picker--grid :deep(.async-picker__list) {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-content: start;
  gap: 10px;
  padding: 12px;
}
.new-run-file-picker__picker--grid :deep(.async-picker__option) {
  min-height: 188px;
  align-content: start;
  align-items: center;
  flex-direction: column;
  justify-content: center;
  border: 1px solid var(--border);
  border-radius: 7px;
  text-align: center;
}
.new-run-file-picker__picker--grid .new-run-file-picker__file-copy {
  width: 100%;
  flex: 0 0 auto;
}
.new-run-file-picker__picker--grid .new-run-file-picker__metadata {
  justify-content: center;
}
.new-run-file-picker__picker--grid :deep(.async-picker__sentinel),
.new-run-file-picker__picker--grid :deep(.async-picker__more),
.new-run-file-picker__picker--grid :deep(.async-picker__state) {
  grid-column: 1 / -1;
}
.new-run-file-picker__selection {
  display: flex;
  min-height: 50px;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
  padding: 7px 12px;
  overflow-x: auto;
  border-top: 1px solid var(--border);
  background: var(--panel);
  font-size: 12px;
}
.new-run-file-picker__chips {
  display: flex;
  min-width: 0;
  gap: 6px;
}
.new-run-file-picker__chip {
  display: inline-flex;
  max-width: 230px;
  min-height: 30px;
  flex: 0 0 auto;
  align-items: center;
  gap: 5px;
  padding: 3px 4px 3px 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
}
.new-run-file-picker__chip > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.new-run-file-picker__chip button {
  display: grid;
  width: 26px;
  height: 26px;
  flex: 0 0 auto;
  place-items: center;
  padding: 0;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
.new-run-file-picker__chip button:hover {
  background: var(--accent-soft);
  color: var(--text);
}
.new-run-file-picker__selection-empty {
  color: var(--text-secondary);
}
.new-run-file-picker__action {
  width: 190px;
  min-height: 44px;
}
.new-run-file-picker__overlay :deep(.overlay-panel--modal) {
  width: min(1120px, calc(100vw - 40px));
  height: min(720px, calc(100dvh - 40px));
}
.new-run-file-picker__overlay :deep(.overlay-panel__body) {
  overflow: hidden;
  padding: 0;
}
@media (max-width: 760px) {
  .new-run-file-picker__overlay {
    align-items: flex-end;
    padding: 0;
  }
  .new-run-file-picker__overlay :deep(.overlay-panel--modal) {
    width: 100%;
    height: calc(100dvh - 40px);
    max-height: none;
    border-width: 1px 0 0;
    border-radius: 10px 10px 0 0;
  }
  .new-run-file-picker__toolbar {
    min-height: 52px;
  }
  .new-run-file-picker__picker--grid :deep(.async-picker__list) {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
    padding: 8px;
  }
  .new-run-file-picker__picker--grid :deep(.async-picker__option) {
    min-height: 180px;
  }
  .new-run-file-picker__selection {
    min-height: 56px;
  }
  .new-run-file-picker__action {
    width: auto;
    flex: 1;
  }
}
</style>
