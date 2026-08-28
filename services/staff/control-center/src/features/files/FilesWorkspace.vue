<script setup lang="ts">
import { Download, Eye, Search, Upload, X } from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { loadArtifactPage } from "@/features/files/api";
import FilePreviewDialog from "@/features/files/FilePreviewDialog.vue";
import FileTypeIcon from "@/features/files/FileTypeIcon.vue";
import {
  matchesArtifactFilters,
  supportsInlinePreview,
  type FileKind,
  type FilePreviewLabels,
  type FileSource,
  type FileTab,
} from "@/features/files/model";
import { usePlatformStore } from "@/features/platform/store";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import ViewModeToggle from "@/shared/ui/ViewModeToggle.vue";
import {
  nearScrollEnd,
  useAsyncEntityCollection,
  useCursorInfiniteScroll,
} from "@/shared/ui/async-entity-picker";
import type { ViewMode } from "@/shared/ui/view-mode-toggle";

const props = defineProps<{
  projectRef: string;
  initialArtifactRef?: string;
}>();
const maximumUploadBytes = 16 << 20;
const maximumTextPreviewBytes = 256 << 10;
const viewPreferenceKey = "kodex.files.view";
const platform = usePlatformStore();
const { locale, t } = useI18n();
const fileInput = ref<HTMLInputElement>();
const scrollRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const activeTab = ref<FileTab>("FILES");
const kind = ref<FileKind>("ALL");
const scanState = ref<"ALL" | Artifact["scanState"]>("ALL");
const source = ref<FileSource>("ALL");
const viewMode = ref<ViewMode>("list");
const selectedRef = ref(props.initialArtifactRef ?? "");
const uploadBusy = ref(false);
const bindingBusy = ref("");
const contentBusy = ref(false);
const operationProblem = ref<AppProblem>();
const validationMessage = ref("");
const previewOpen = ref(false);
const previewText = ref("");
const previewImage = ref("");
const previewUnavailable = ref(false);

const collection = useAsyncEntityCollection(
  (request) => loadArtifactPage(props.projectRef, request),
  { debounceMs: 250 },
);
const {
  error: loadError,
  hasMore,
  initialLoading,
  items,
  loadMore,
  loadingMore,
  query,
  refresh,
} = collection;

const project = computed(() => platform.projects[props.projectRef]);
const canUpload = computed(() =>
  project.value?.nextActions.includes("UPLOAD_ARTIFACT"),
);
const agents = computed(() =>
  Object.values(platform.agents)
    .filter(
      (agent) =>
        agent.projectRef === props.projectRef &&
        !agent.system &&
        agent.state !== "ARCHIVED",
    )
    .sort((left, right) => left.name.localeCompare(right.name)),
);
const loadedArtifacts = computed(() =>
  items.value.map((item) => item.artifact),
);
const filteredArtifacts = computed(() =>
  loadedArtifacts.value.filter((artifact) =>
    matchesArtifactFilters(artifact, {
      kind: kind.value,
      scanState: scanState.value,
      source: source.value,
      tab: activeTab.value,
    }),
  ),
);
const selectedArtifact = computed(() =>
  loadedArtifacts.value.find((artifact) => artifact.ref === selectedRef.value),
);
const listProblem = computed(() =>
  loadError.value === undefined ? undefined : asProblem(loadError.value),
);
const previewLabels = computed<FilePreviewLabels>(() => ({
  added: locale.value.startsWith("en") ? "Added" : "Добавлен",
  close: t("common.close"),
  download: t("common.download"),
  find: locale.value.startsWith("en") ? "Find in file" : "Найти в файле",
  loading: t("common.loading"),
  protectedPreview: locale.value.startsWith("en")
    ? "Protected preview"
    : "Защищённый предпросмотр",
  size: t("files.size"),
  source: t("common.source"),
  unavailable: t("files.previewUnavailable"),
  version: t("files.revision"),
  zoom: locale.value.startsWith("en") ? "Zoom" : "Масштаб",
}));
const custom = computed(() =>
  locale.value.startsWith("en")
    ? {
        clearSearch: "Clear search",
        grid: "Grid",
        loaded: "Loaded",
        loadingMore: "Loading more…",
        preview: previewLabels.value,
        view: "File view",
      }
    : {
        clearSearch: "Очистить поиск",
        grid: "Сетка",
        loaded: "Загружено",
        loadingMore: "Загружаем ещё…",
        preview: previewLabels.value,
        view: "Вид файлов",
      },
);

watch(
  filteredArtifacts,
  (artifacts) => {
    if (initialLoading.value && artifacts.length === 0) return;
    if (!artifacts.some((artifact) => artifact.ref === selectedRef.value))
      selectedRef.value = artifacts[0]?.ref ?? "";
  },
  { immediate: true },
);
watch(viewMode, (mode) => {
  if (typeof window !== "undefined")
    window.localStorage.setItem(viewPreferenceKey, mode);
});

useCursorInfiniteScroll({
  root: scrollRoot,
  sentinel,
  enabled: hasMore,
  loadMore,
});

function handleScroll(event: Event): void {
  const target = event.currentTarget;
  if (target instanceof HTMLElement && hasMore.value && nearScrollEnd(target))
    void loadMore();
}

function formatBytes(value: number): string {
  const units = ["BYTE", "KILOBYTE", "MEGABYTE", "GIGABYTE"] as const;
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  const formatted = new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: unit === 0 ? 0 : 1,
  }).format(size);
  return t(`files.unit.${units[unit] ?? "BYTE"}`, { value: formatted });
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function sourceLabel(value: Artifact["source"]): string {
  return t(`files.source.${value}`);
}

function bindingNames(artifact: Artifact): string {
  return artifact.agentBindings
    .map((ref) => platform.agents[ref]?.name)
    .filter((name): name is string => Boolean(name))
    .join(", ");
}

function agentSupportsFiles(agentRef: string): boolean {
  return (
    platform.agents[agentRef]?.capabilities.some(
      (capability) => capability.key === "platform.artifact.manage",
    ) ?? false
  );
}

function replaceArtifact(artifact: Artifact): void {
  items.value = items.value.map((item) =>
    item.id === artifact.ref
      ? {
          ...item,
          artifact,
          description: artifact.mediaType,
          label: artifact.fileName,
        }
      : item,
  );
}

async function upload(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file || !canUpload.value) return;
  operationProblem.value = undefined;
  validationMessage.value = "";
  if (file.size > maximumUploadBytes) {
    validationMessage.value = t("files.uploadTooLarge");
    return;
  }
  uploadBusy.value = true;
  try {
    const artifact = await platform.uploadProjectArtifact(
      props.projectRef,
      file,
    );
    selectedRef.value = artifact.ref;
    activeTab.value = "FILES";
    refresh();
  } catch (error) {
    operationProblem.value = asProblem(error);
  } finally {
    uploadBusy.value = false;
  }
}

async function changeBinding(
  artifact: Artifact,
  agentRef: string,
  enabled: boolean,
): Promise<void> {
  if (!artifact.nextActions.includes("BIND")) return;
  bindingBusy.value = `${artifact.ref}:${agentRef}`;
  operationProblem.value = undefined;
  try {
    replaceArtifact(
      await platform.changeArtifactAgentBinding(artifact, agentRef, enabled),
    );
  } catch (error) {
    operationProblem.value = asProblem(error);
  } finally {
    bindingBusy.value = "";
  }
}

function clearPreview(): void {
  previewText.value = "";
  previewUnavailable.value = false;
  if (previewImage.value) URL.revokeObjectURL(previewImage.value);
  previewImage.value = "";
}

async function openPreview(artifact: Artifact): Promise<void> {
  selectedRef.value = artifact.ref;
  clearPreview();
  previewOpen.value = true;
  operationProblem.value = undefined;
  if (!supportsInlinePreview(artifact)) {
    previewUnavailable.value = true;
    return;
  }
  contentBusy.value = true;
  try {
    const body = await platform.downloadArtifactContent(
      artifact.ref,
      "PREVIEW",
    );
    if (
      artifact.mediaType.startsWith("text/") ||
      artifact.mediaType === "application/json"
    ) {
      if (body.size > maximumTextPreviewBytes) previewUnavailable.value = true;
      else previewText.value = await body.text();
    } else if (artifact.mediaType.startsWith("image/")) {
      previewImage.value = URL.createObjectURL(body);
    } else previewUnavailable.value = true;
  } catch (error) {
    operationProblem.value = asProblem(error);
    previewUnavailable.value = true;
  } finally {
    contentBusy.value = false;
  }
}

async function download(artifact: Artifact): Promise<void> {
  contentBusy.value = true;
  operationProblem.value = undefined;
  try {
    const body = await platform.downloadArtifactContent(
      artifact.ref,
      "DOWNLOAD",
    );
    const url = URL.createObjectURL(body);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = artifact.fileName;
    anchor.hidden = true;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
  } catch (error) {
    operationProblem.value = asProblem(error);
  } finally {
    contentBusy.value = false;
  }
}

function closePreview(): void {
  previewOpen.value = false;
  clearPreview();
}

onMounted(() => {
  const preferred = window.localStorage.getItem(viewPreferenceKey);
  if (preferred === "grid" || preferred === "list") viewMode.value = preferred;
  void Promise.all([
    platform.loadProject(props.projectRef),
    platform.loadAgents(props.projectRef),
  ]);
});
onBeforeUnmount(clearPreview);
</script>

<template>
  <section class="files-workspace" aria-label="files">
    <input
      ref="fileInput"
      class="sr-only"
      type="file"
      accept=".txt,.md,.markdown,.csv,.json,.pdf,.png,.jpg,.jpeg,.gif,.webp,.docx,.xlsx,.pptx"
      :aria-label="$t('common.upload')"
      @change="upload"
    />
    <div class="files-workspace__toolbar">
      <label class="files-workspace__search">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ $t("files.search") }}</span>
        <input
          v-model="query"
          type="search"
          :placeholder="$t('files.search')"
        />
        <button
          v-if="query"
          type="button"
          :title="custom.clearSearch"
          :aria-label="custom.clearSearch"
          @click="query = ''"
        >
          <X :size="15" aria-hidden="true" />
        </button>
      </label>
      <label>
        <span class="sr-only">{{ $t("files.typeFilter") }}</span>
        <select v-model="kind" :aria-label="$t('files.typeFilter')">
          <option value="ALL">{{ $t("files.kind.ALL") }}</option>
          <option value="TEXT">{{ $t("files.kind.TEXT") }}</option>
          <option value="DOCUMENT">{{ $t("files.kind.DOCUMENT") }}</option>
          <option value="IMAGE">{{ $t("files.kind.IMAGE") }}</option>
        </select>
      </label>
      <label>
        <span class="sr-only">{{ $t("files.stateFilter") }}</span>
        <select v-model="scanState" :aria-label="$t('files.stateFilter')">
          <option value="ALL">{{ $t("files.allStates") }}</option>
          <option value="PENDING">{{ $t("states.PENDING") }}</option>
          <option value="SCANNING">{{ $t("states.SCANNING") }}</option>
          <option value="CLEAN">{{ $t("states.CLEAN") }}</option>
          <option value="QUARANTINED">{{ $t("states.QUARANTINED") }}</option>
          <option value="FAILED">{{ $t("states.FAILED") }}</option>
        </select>
      </label>
      <label class="desktop-only">
        <span class="sr-only">{{ $t("files.sourceFilter") }}</span>
        <select v-model="source" :aria-label="$t('files.sourceFilter')">
          <option value="ALL">{{ $t("files.allSources") }}</option>
          <option value="CONTROL_CENTER">
            {{ $t("files.source.CONTROL_CENTER") }}
          </option>
          <option value="AGENT_RESULT">
            {{ $t("files.source.AGENT_RESULT") }}
          </option>
          <option value="INTEGRATION_RESULT">
            {{ $t("files.source.INTEGRATION_RESULT") }}
          </option>
          <option value="KNOWLEDGE_SOURCE">
            {{ $t("files.source.KNOWLEDGE_SOURCE") }}
          </option>
          <option value="INTERACTION_ATTACHMENT">
            {{ $t("files.source.INTERACTION_ATTACHMENT") }}
          </option>
        </select>
      </label>
      <span class="files-workspace__count mono">
        {{ custom.loaded }} {{ items.length }}
      </span>
      <ViewModeToggle
        v-model="viewMode"
        class="files-workspace__view-toggle"
        :ariaLabel="custom.view"
        :list-label="$t('files.list')"
        :grid-label="custom.grid"
      />
      <button
        v-if="canUpload"
        class="button button--primary"
        type="button"
        :disabled="uploadBusy"
        @click="fileInput?.click()"
      >
        <Upload :size="16" aria-hidden="true" />
        {{ uploadBusy ? $t("files.uploading") : $t("common.upload") }}
      </button>
    </div>

    <div
      class="files-workspace__tabs"
      role="tablist"
      :aria-label="$t('files.tabs')"
    >
      <button
        v-for="tab in ['FILES', 'KNOWLEDGE', 'RESULTS'] as FileTab[]"
        :key="tab"
        type="button"
        role="tab"
        :aria-selected="activeTab === tab"
        @click="activeTab = tab"
      >
        {{ $t(`files.tab.${tab}`) }}
      </button>
    </div>

    <ProblemNotice
      v-if="operationProblem"
      :problem="operationProblem"
      compact
    />
    <p v-if="validationMessage" class="field-error" role="alert">
      {{ validationMessage }}
    </p>
    <ProblemNotice
      v-if="listProblem && items.length > 0"
      :problem="listProblem"
      @retry="refresh"
    />

    <AsyncState
      :loading="initialLoading"
      :problem="items.length === 0 ? listProblem : undefined"
      :empty="items.length === 0 && !hasMore && !query.trim()"
      :empty-title="$t('files.emptyTitle')"
      :empty-text="$t('files.emptyText')"
      @retry="refresh"
    >
      <div class="files-workspace__layout">
        <div
          ref="scrollRoot"
          class="files-workspace__scroll"
          @scroll.passive="handleScroll"
        >
          <section
            v-if="filteredArtifacts.length === 0"
            class="empty-state files-workspace__filtered-empty"
          >
            <h2>{{ $t("files.noMatches") }}</h2>
            <p>{{ $t("files.noMatchesText") }}</p>
          </section>

          <div v-else-if="viewMode === 'grid'" class="files-grid" role="list">
            <button
              v-for="artifact in filteredArtifacts"
              :key="artifact.ref"
              class="file-tile"
              :class="{ 'file-tile--selected': selectedRef === artifact.ref }"
              type="button"
              role="listitem"
              @click="selectedRef = artifact.ref"
              @dblclick="openPreview(artifact)"
            >
              <span class="file-tile__preview">
                <FileTypeIcon :artifact="artifact" large />
              </span>
              <strong :title="artifact.fileName">{{
                artifact.fileName
              }}</strong>
              <span class="file-tile__meta">
                <span class="mono">{{ formatBytes(artifact.sizeBytes) }}</span>
                <span class="mono">v{{ artifact.revision }}</span>
              </span>
              <StatusBadge :state="artifact.scanState" />
            </button>
          </div>

          <div v-else class="files-list" role="list">
            <div class="files-list__head desktop-only" aria-hidden="true">
              <span>{{ $t("files.file") }}</span>
              <span>{{ $t("files.usedBy") }}</span>
              <span>{{ $t("files.revision") }}</span>
              <span>{{ $t("common.status") }}</span>
              <span></span>
            </div>
            <button
              v-for="artifact in filteredArtifacts"
              :key="artifact.ref"
              class="file-list-row"
              :class="{
                'file-list-row--selected': selectedRef === artifact.ref,
              }"
              type="button"
              role="listitem"
              @click="selectedRef = artifact.ref"
              @dblclick="openPreview(artifact)"
            >
              <span class="file-list-row__identity">
                <FileTypeIcon :artifact="artifact" />
                <span>
                  <strong :title="artifact.fileName">{{
                    artifact.fileName
                  }}</strong>
                  <small>
                    {{ formatBytes(artifact.sizeBytes) }} ·
                    {{ sourceLabel(artifact.source) }}
                  </small>
                </span>
              </span>
              <span class="file-list-row__binding">
                {{ bindingNames(artifact) || $t("files.notBound") }}
              </span>
              <span class="mono">v{{ artifact.revision }}</span>
              <StatusBadge :state="artifact.scanState" />
              <span class="file-list-row__date">{{
                formatDate(artifact.createdAt)
              }}</span>
            </button>
          </div>

          <div
            v-if="hasMore"
            ref="sentinel"
            class="files-workspace__sentinel"
            role="status"
          >
            <span v-if="loadingMore">{{ custom.loadingMore }}</span>
          </div>
        </div>

        <aside
          v-if="selectedArtifact"
          class="file-details"
          :aria-label="$t('files.details')"
        >
          <header>
            <FileTypeIcon :artifact="selectedArtifact" large />
            <div>
              <h2>{{ selectedArtifact.fileName }}</h2>
              <StatusBadge :state="selectedArtifact.scanState" />
            </div>
          </header>
          <dl>
            <div>
              <dt>{{ $t("files.size") }}</dt>
              <dd>{{ formatBytes(selectedArtifact.sizeBytes) }}</dd>
            </div>
            <div>
              <dt>{{ $t("files.revision") }}</dt>
              <dd class="mono">v{{ selectedArtifact.revision }}</dd>
            </div>
            <div>
              <dt>{{ $t("common.source") }}</dt>
              <dd>{{ sourceLabel(selectedArtifact.source) }}</dd>
            </div>
            <div>
              <dt>{{ $t("files.addedAt") }}</dt>
              <dd>{{ formatDate(selectedArtifact.createdAt) }}</dd>
            </div>
          </dl>
          <section class="file-details__preview">
            <h3>{{ $t("files.preview") }}</h3>
            <p>
              {{
                supportsInlinePreview(selectedArtifact)
                  ? $t("files.previewReady")
                  : $t("files.previewUnavailable")
              }}
            </p>
            <button
              class="button button--primary"
              type="button"
              :disabled="contentBusy"
              @click="openPreview(selectedArtifact)"
            >
              <Eye :size="16" aria-hidden="true" />
              {{ $t("files.openPreview") }}
            </button>
          </section>
          <section class="file-details__bindings">
            <h3>{{ $t("files.binding") }}</h3>
            <p>{{ $t("files.bindingHint") }}</p>
            <p v-if="agents.length === 0" class="muted-text">
              {{ $t("files.noAgents") }}
            </p>
            <label v-for="agent in agents" :key="agent.ref">
              <input
                type="checkbox"
                :checked="selectedArtifact.agentBindings.includes(agent.ref)"
                :disabled="
                  !selectedArtifact.nextActions.includes('BIND') ||
                  !agentSupportsFiles(agent.ref) ||
                  bindingBusy === `${selectedArtifact.ref}:${agent.ref}`
                "
                @change="
                  changeBinding(
                    selectedArtifact,
                    agent.ref,
                    ($event.target as HTMLInputElement).checked,
                  )
                "
              />
              <span
                ><strong>{{ agent.name }}</strong
                ><small>{{
                  agentSupportsFiles(agent.ref)
                    ? agent.purpose
                    : $t("files.agentFilesCapabilityRequired")
                }}</small></span
              >
            </label>
          </section>
          <button
            v-if="selectedArtifact.nextActions.includes('DOWNLOAD')"
            class="button file-details__download"
            type="button"
            :disabled="contentBusy"
            @click="download(selectedArtifact)"
          >
            <Download :size="16" aria-hidden="true" />
            {{ $t("common.download") }}
          </button>
        </aside>
      </div>
    </AsyncState>

    <FilePreviewDialog
      v-if="previewOpen && selectedArtifact"
      :artifact="selectedArtifact"
      :image-url="previewImage"
      :labels="custom.preview"
      :loading="contentBusy"
      :preview-text="previewText"
      :unavailable="previewUnavailable"
      :format-bytes="formatBytes"
      :format-date="formatDate"
      :source-label="sourceLabel"
      @close="closePreview"
      @download="download(selectedArtifact)"
    />
  </section>
</template>

<style scoped>
.files-workspace {
  display: grid;
  min-height: 640px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.files-workspace__toolbar {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.files-workspace__toolbar select {
  min-height: 36px;
  max-width: 180px;
}
.files-workspace__search {
  display: flex;
  width: 240px;
  flex: 0 1 240px;
  align-items: center;
  gap: 7px;
  padding: 0 9px;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}
.files-workspace__search input {
  width: 100%;
  min-width: 0;
  min-height: 34px;
  padding: 0;
  border: 0;
  outline: 0;
}
.files-workspace__search button {
  display: grid;
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  place-items: center;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
}
.files-workspace__count {
  margin-left: auto;
  color: var(--muted);
  font-size: 0.78rem;
  white-space: nowrap;
}
.files-workspace__tabs {
  display: flex;
  gap: 18px;
  padding: 0 16px;
  border-bottom: 1px solid var(--border);
}
.files-workspace__tabs button {
  min-height: 42px;
  padding: 0 2px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
}
.files-workspace__tabs button[aria-selected="true"] {
  border-color: var(--accent);
  color: var(--accent-strong);
  font-weight: 600;
}
.files-workspace > .problem-notice,
.files-workspace > .field-error {
  margin: 10px 14px 0;
}
.files-workspace__layout {
  display: grid;
  min-height: 540px;
  grid-template-columns: minmax(0, 1fr) 350px;
}
.files-workspace__scroll {
  min-width: 0;
  max-height: 68vh;
  overflow: auto;
  background: var(--canvas);
}
.files-workspace__filtered-empty {
  margin: 16px;
}
.files-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 12px;
  padding: 14px;
}
.file-tile {
  display: grid;
  min-width: 0;
  min-height: 196px;
  align-content: start;
  justify-items: start;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.file-tile:hover,
.file-tile--selected {
  border-color: var(--accent);
}
.file-tile--selected {
  box-shadow: 0 0 0 2px var(--accent-soft);
}
.file-tile__preview {
  display: grid;
  width: 100%;
  height: 82px;
  place-items: center;
  border-bottom: 1px solid var(--hairline);
}
.file-tile strong {
  display: -webkit-box;
  min-height: 2.6em;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow-wrap: anywhere;
  line-height: 1.3;
}
.file-tile__meta {
  display: flex;
  width: 100%;
  justify-content: space-between;
  color: var(--muted);
  font-size: 0.76rem;
}
.files-list {
  min-width: 720px;
  background: var(--surface);
}
.files-list__head,
.file-list-row {
  display: grid;
  grid-template-columns:
    minmax(260px, 1.5fr) minmax(150px, 1fr)
    64px 128px 132px;
  gap: 12px;
  align-items: center;
}
.files-list__head {
  position: sticky;
  z-index: 2;
  top: 0;
  min-height: 38px;
  padding: 0 14px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
  color: var(--subtle);
  font-size: 0.72rem;
  font-weight: 600;
}
.file-list-row {
  width: 100%;
  min-height: 64px;
  padding: 8px 14px;
  border: 0;
  border-bottom: 1px solid var(--hairline);
  background: var(--surface);
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.file-list-row:hover,
.file-list-row--selected {
  background: var(--accent-soft);
}
.file-list-row__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}
.file-list-row__identity > span:last-child,
.file-list-row__identity strong,
.file-list-row__identity small,
.file-list-row__binding {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.file-list-row__identity > span:last-child,
.file-list-row__identity strong,
.file-list-row__identity small {
  display: block;
}
.file-list-row__identity small,
.file-list-row__binding,
.file-list-row__date {
  color: var(--muted);
  font-size: 0.78rem;
}
.files-workspace__sentinel {
  display: grid;
  min-height: 52px;
  place-items: center;
  color: var(--muted);
  font-size: 0.8rem;
}
.file-details {
  min-width: 0;
  max-height: 68vh;
  overflow: auto;
  padding: 16px;
  border-left: 1px solid var(--border);
  background: var(--surface);
}
.file-details header {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.file-details header h2 {
  margin: 0 0 8px;
  overflow-wrap: anywhere;
  font-size: 1rem;
}
.file-details dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 16px 0;
}
.file-details dl div {
  min-width: 0;
  padding: 9px;
  background: var(--panel);
}
.file-details dt {
  color: var(--subtle);
  font-size: 0.72rem;
}
.file-details dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
}
.file-details__preview,
.file-details__bindings {
  padding: 14px 0;
  border-top: 1px solid var(--border);
}
.file-details h3 {
  margin: 0 0 6px;
  font-size: 0.86rem;
}
.file-details__preview p,
.file-details__bindings > p {
  color: var(--muted);
  font-size: 0.8rem;
}
.file-details__bindings label {
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  padding: 8px 0;
  border-top: 1px solid var(--hairline);
}
.file-details__bindings input {
  width: 18px;
  height: 18px;
  margin-top: 2px;
}
.file-details__bindings strong,
.file-details__bindings small {
  display: block;
}
.file-details__bindings small {
  margin-top: 2px;
  color: var(--muted);
}
.file-details__download {
  width: 100%;
  justify-content: center;
}
@media (max-width: 980px) {
  .files-workspace__layout {
    grid-template-columns: minmax(0, 1fr) 300px;
  }
  .files-workspace__toolbar .desktop-only {
    display: none;
  }
}
@media (max-width: 760px) {
  .files-workspace {
    min-height: 0;
    overflow: visible;
    border-right: 0;
    border-left: 0;
    border-radius: 0;
  }
  .files-workspace__toolbar {
    flex-wrap: wrap;
    padding: 10px 0;
  }
  .files-workspace__search {
    width: 100%;
    flex-basis: 100%;
  }
  .files-workspace__toolbar select {
    max-width: calc(50vw - 20px);
  }
  .files-workspace__count,
  .files-workspace__view-toggle {
    display: none;
  }
  .files-workspace__toolbar .button {
    min-height: 44px;
    margin-left: auto;
  }
  .files-workspace__tabs {
    gap: 12px;
    padding: 0;
    overflow-x: auto;
  }
  .files-workspace__tabs button {
    flex: 0 0 auto;
    min-height: 44px;
  }
  .files-workspace__layout {
    display: block;
    min-height: 0;
  }
  .files-workspace__scroll {
    max-height: none;
    overflow: visible;
  }
  .files-list {
    min-width: 0;
  }
  .file-list-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    min-height: 76px;
    gap: 6px 10px;
    padding: 10px 2px;
  }
  .file-list-row__identity {
    grid-row: 1 / 3;
  }
  .file-list-row__binding,
  .file-list-row__date,
  .file-list-row > .mono {
    display: none;
  }
  .file-list-row .status-badge {
    grid-column: 2;
  }
  .file-details {
    max-height: none;
    padding: 16px 0;
    border-top: 1px solid var(--border);
    border-left: 0;
  }
}
</style>
