<script setup lang="ts">
import {
  Download,
  Eye,
  RefreshCw,
  RotateCcw,
  Search,
  Trash2,
  Upload,
  X,
} from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  deleteArtifactItem,
  loadArtifactPage,
  purgeArtifactItem,
  restoreArtifactItem,
} from "@/features/files/api";
import FileLifecycleDialog from "@/features/files/FileLifecycleDialog.vue";
import FilePreviewDialog from "@/features/files/FilePreviewDialog.vue";
import FileTypeIcon from "@/features/files/FileTypeIcon.vue";
import {
  artifactLifecycleState,
  createUploadQueueItems,
  matchesArtifactFilters,
  supportsInlinePreview,
  type ArtifactLifecycleAction,
  type ArtifactLifecycleState,
  type FileKind,
  type FilePreviewLabels,
  type FileSource,
  type FileTab,
  type UploadQueueItem,
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
const uploadQueue = ref<UploadQueueItem[]>([]);
const dragDepth = ref(0);
const dragActive = computed(() => dragDepth.value > 0);
const bindingBusy = ref("");
const contentBusy = ref(false);
const operationProblem = ref<AppProblem>();
const validationMessage = ref("");
const previewOpen = ref(false);
const previewText = ref("");
const previewImage = ref("");
const previewUnavailable = ref(false);
const lifecycleDialog = ref<{
  action: ArtifactLifecycleAction;
  artifact: Artifact;
  state: ArtifactLifecycleState;
}>();
let uploadSequence = 0;

const collection = useAsyncEntityCollection(
  (request) =>
    loadArtifactPage(
      props.projectRef,
      request,
      activeTab.value === "TRASH" ? "DELETED" : "ACTIVE",
    ),
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
        actionUnavailable:
          "The current API does not expose this operation. No file was changed.",
        allFiles: "Files",
        cancel: "Cancel",
        clearSearch: "Clear search",
        contractUnavailable:
          "The lifecycle contract has not been generated for this client yet.",
        delete: "Move to trash",
        deleteDescription:
          "The file will stop being available to new runs and can be restored for 30 days.",
        dropFiles: "Drop files to upload",
        emptyTrash: "Empty trash",
        failed: "Upload failed",
        grid: "Grid",
        impactUnavailable:
          "Affected active runs cannot be calculated until the lifecycle API is available.",
        loaded: "Loaded",
        loadingMore: "Loading more…",
        purge: "Delete permanently",
        purgeDescription:
          "The exact object version will be removed from storage without recovery.",
        preview: previewLabels.value,
        queued: "Queued",
        removeFromQueue: "Remove from upload queue",
        restore: "Restore",
        restoreDescription:
          "The file will return to the Project with its previous revision and bindings.",
        retry: "Retry",
        trash: "Trash",
        trashContract:
          "Trash listing, restore and purge are shown as unavailable until the server contract is implemented.",
        trashEmpty: "No deleted files are available in this response.",
        uploadMore: "Add files",
        uploading: "Uploading",
        uploadQueue: "Upload queue",
        viewFilter: "Collection",
        view: "File view",
        lifecycle: {
          confirm: {
            DELETE: "Move to trash",
            PURGE: "Delete permanently",
            RESTORE: "Restore",
          },
          description: {
            DELETE:
              "The file will be hidden from future work and retained for 30 days.",
            PURGE:
              "The exact object version will be permanently removed from storage.",
            RESTORE: "The file will return to its previous Project scope.",
          },
          reason: {
            ACTION_NOT_ALLOWED:
              "Your current permissions do not include this action.",
            CONTRACT_UNAVAILABLE:
              "The server announced the action, but this client has no generated command for it.",
          },
          title: {
            DELETE: "Move file to trash?",
            PURGE: "Delete file permanently?",
            RESTORE: "Restore file?",
          },
        },
      }
    : {
        actionUnavailable:
          "Текущий API не предоставляет эту операцию. Файл не был изменён.",
        allFiles: "Файлы",
        cancel: "Отмена",
        clearSearch: "Очистить поиск",
        contractUnavailable:
          "Контракт lifecycle ещё не сгенерирован для этого клиента.",
        delete: "В корзину",
        deleteDescription:
          "Файл перестанет выдаваться новым запускам, но его можно будет восстановить в течение 30 дней.",
        dropFiles: "Перетащите файлы для загрузки",
        emptyTrash: "Очистить корзину",
        failed: "Не удалось загрузить",
        grid: "Сетка",
        impactUnavailable:
          "Список затронутых активных запусков нельзя вычислить до появления lifecycle API.",
        loaded: "Загружено",
        loadingMore: "Загружаем ещё…",
        purge: "Удалить навсегда",
        purgeDescription:
          "Точная версия объекта будет удалена из хранилища без возможности восстановления.",
        preview: previewLabels.value,
        queued: "В очереди",
        removeFromQueue: "Убрать из очереди загрузки",
        restore: "Восстановить",
        restoreDescription:
          "Файл вернётся в Проект с прежней ревизией и привязками.",
        retry: "Повторить",
        trash: "Корзина",
        trashContract:
          "Список корзины, восстановление и очистка честно недоступны до реализации серверного контракта.",
        trashEmpty: "В текущем ответе нет удалённых файлов.",
        uploadMore: "Добавить файлы",
        uploading: "Загружается",
        uploadQueue: "Очередь загрузки",
        viewFilter: "Раздел",
        view: "Вид файлов",
        lifecycle: {
          confirm: {
            DELETE: "Переместить в корзину",
            PURGE: "Удалить навсегда",
            RESTORE: "Восстановить",
          },
          description: {
            DELETE: "Файл будет скрыт от будущей работы и сохранён на 30 дней.",
            PURGE:
              "Точная версия объекта будет необратимо удалена из хранилища.",
            RESTORE: "Файл вернётся в прежнюю область Проекта.",
          },
          reason: {
            ACTION_NOT_ALLOWED: "Текущие полномочия не содержат эту операцию.",
            CONTRACT_UNAVAILABLE:
              "Сервер объявил операцию, но в клиенте нет сгенерированной команды.",
          },
          title: {
            DELETE: "Переместить файл в корзину?",
            PURGE: "Удалить файл навсегда?",
            RESTORE: "Восстановить файл?",
          },
        },
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
watch(activeTab, () => {
  selectedRef.value = "";
  refresh();
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

function uploadPreviewArtifact(item: UploadQueueItem): Artifact {
  return {
    ref: item.id,
    version: 1,
    projectRef: props.projectRef,
    fileName: item.file.name,
    mediaType: item.file.type || "application/octet-stream",
    sizeBytes: item.file.size,
    digest: "",
    scanState: "PENDING",
    source: "CONTROL_CENTER",
    revision: 1,
    lifecycleState: "ACTIVE",
    agentBindings: [],
    previewAvailable: false,
    createdAt: "1970-01-01T00:00:00.000Z",
    nextActions: [],
  };
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

function updateUploadQueueItem(
  id: string,
  update: Partial<Pick<UploadQueueItem, "problem" | "state">>,
): void {
  uploadQueue.value = uploadQueue.value.map((item) =>
    item.id === id ? { ...item, ...update } : item,
  );
}

function enqueueFiles(files: readonly File[]): void {
  if (!canUpload.value || files.length === 0) return;
  operationProblem.value = undefined;
  validationMessage.value = "";
  const queued = createUploadQueueItems(files, () => {
    uploadSequence += 1;
    return `upload-${String(uploadSequence)}`;
  }).map((item) =>
    item.file.size > maximumUploadBytes
      ? {
          ...item,
          problem: t("files.uploadTooLarge"),
          state: "FAILED" as const,
        }
      : item,
  );
  uploadQueue.value = [...uploadQueue.value, ...queued];
  void processUploadQueue();
}

async function processUploadQueue(): Promise<void> {
  if (uploadBusy.value) return;
  uploadBusy.value = true;
  let uploaded = false;
  try {
    let next = uploadQueue.value.find((item) => item.state === "QUEUED");
    while (next) {
      updateUploadQueueItem(next.id, {
        problem: undefined,
        state: "UPLOADING",
      });
      try {
        const artifact = await platform.uploadProjectArtifact(
          props.projectRef,
          next.file,
        );
        selectedRef.value = artifact.ref;
        activeTab.value = "FILES";
        uploaded = true;
        updateUploadQueueItem(next.id, { state: "SUCCEEDED" });
      } catch (error) {
        const problem = asProblem(error);
        updateUploadQueueItem(next.id, {
          problem: problem.detail || problem.title,
          state: "FAILED",
        });
      }
      next = uploadQueue.value.find((item) => item.state === "QUEUED");
    }
  } finally {
    uploadBusy.value = false;
    if (uploaded) refresh();
  }
}

function upload(event: Event): void {
  const input = event.target as HTMLInputElement;
  const files = Array.from(input.files ?? []);
  input.value = "";
  enqueueFiles(files);
}

function handleDragEnter(event: DragEvent): void {
  if (!canUpload.value || !event.dataTransfer?.types.includes("Files")) return;
  event.preventDefault();
  dragDepth.value += 1;
}

function handleDragOver(event: DragEvent): void {
  if (!canUpload.value || !event.dataTransfer?.types.includes("Files")) return;
  event.preventDefault();
  event.dataTransfer.dropEffect = "copy";
}

function handleDragLeave(event: DragEvent): void {
  if (!canUpload.value || !event.dataTransfer?.types.includes("Files")) return;
  event.preventDefault();
  dragDepth.value = Math.max(0, dragDepth.value - 1);
}

function handleDrop(event: DragEvent): void {
  if (!canUpload.value) return;
  event.preventDefault();
  dragDepth.value = 0;
  enqueueFiles(Array.from(event.dataTransfer?.files ?? []));
}

function retryUpload(id: string): void {
  updateUploadQueueItem(id, { problem: undefined, state: "QUEUED" });
  void processUploadQueue();
}

function removeUpload(id: string): void {
  uploadQueue.value = uploadQueue.value.filter(
    (item) => item.id !== id || item.state === "UPLOADING",
  );
}

function clearFinishedUploads(): void {
  uploadQueue.value = uploadQueue.value.filter(
    (item) => item.state === "UPLOADING" || item.state === "QUEUED",
  );
}

function openLifecycleDialog(
  artifact: Artifact,
  action: ArtifactLifecycleAction,
): void {
  lifecycleDialog.value = {
    action,
    artifact,
    state: artifactLifecycleState(artifact, action),
  };
}

async function confirmLifecycleOperation(): Promise<void> {
  const operation = lifecycleDialog.value;
  if (!operation?.state.available) {
    validationMessage.value = custom.value.actionUnavailable;
    lifecycleDialog.value = undefined;
    return;
  }
  contentBusy.value = true;
  operationProblem.value = undefined;
  validationMessage.value = "";
  try {
    if (operation.action === "DELETE")
      replaceArtifact(await deleteArtifactItem(operation.artifact));
    else if (operation.action === "RESTORE")
      replaceArtifact(await restoreArtifactItem(operation.artifact));
    else await purgeArtifactItem(operation.artifact);
    selectedRef.value = "";
    lifecycleDialog.value = undefined;
    refresh();
  } catch (error) {
    operationProblem.value = asProblem(error);
  } finally {
    contentBusy.value = false;
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
  <section
    class="files-workspace"
    :class="{ 'files-workspace--drag-active': dragActive }"
    aria-label="files"
    @dragenter="handleDragEnter"
    @dragover="handleDragOver"
    @dragleave="handleDragLeave"
    @drop="handleDrop"
  >
    <input
      ref="fileInput"
      class="sr-only"
      type="file"
      multiple
      accept=".txt,.md,.markdown,.csv,.json,.pdf,.png,.jpg,.jpeg,.gif,.webp,.docx,.xlsx,.pptx"
      :aria-label="$t('common.upload')"
      @change="upload"
    />
    <div v-if="dragActive" class="files-workspace__drop-overlay">
      <Upload :size="32" aria-hidden="true" />
      <strong>{{ custom.dropFiles }}</strong>
    </div>
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
        <span class="sr-only">{{ custom.viewFilter }}</span>
        <select v-model="activeTab" :aria-label="custom.viewFilter">
          <option value="FILES">{{ $t("files.tab.FILES") }}</option>
          <option value="KNOWLEDGE">{{ $t("files.tab.KNOWLEDGE") }}</option>
          <option value="RESULTS">{{ $t("files.tab.RESULTS") }}</option>
          <option value="TRASH">{{ custom.trash }}</option>
        </select>
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
        v-if="canUpload && activeTab !== 'TRASH'"
        class="button button--primary"
        type="button"
        :disabled="uploadBusy"
        @click="fileInput?.click()"
      >
        <Upload :size="16" aria-hidden="true" />
        {{ uploadBusy ? $t("files.uploading") : custom.uploadMore }}
      </button>
    </div>

    <section v-if="uploadQueue.length > 0" class="upload-queue">
      <header>
        <div>
          <strong>{{ custom.uploadQueue }}</strong>
          <span class="mono">{{ uploadQueue.length }}</span>
        </div>
        <button
          class="button button--small"
          type="button"
          :disabled="uploadQueue.every((item) => item.state === 'UPLOADING')"
          @click="clearFinishedUploads"
        >
          {{ $t("common.close") }}
        </button>
      </header>
      <ul>
        <li v-for="item in uploadQueue" :key="item.id">
          <FileTypeIcon :artifact="uploadPreviewArtifact(item)" />
          <span>
            <strong>{{ item.file.name }}</strong>
            <small>
              {{ formatBytes(item.file.size) }} ·
              {{
                item.state === "UPLOADING"
                  ? custom.uploading
                  : item.state === "QUEUED"
                    ? custom.queued
                    : item.state === "FAILED"
                      ? item.problem || custom.failed
                      : $t("states.CLEAN")
              }}
            </small>
          </span>
          <button
            v-if="item.state === 'FAILED'"
            class="icon-button"
            type="button"
            :title="custom.retry"
            :aria-label="`${custom.retry}: ${item.file.name}`"
            @click="retryUpload(item.id)"
          >
            <RefreshCw :size="16" aria-hidden="true" />
          </button>
          <button
            v-else-if="item.state !== 'UPLOADING'"
            class="icon-button"
            type="button"
            :title="custom.removeFromQueue"
            :aria-label="`${custom.removeFromQueue}: ${item.file.name}`"
            @click="removeUpload(item.id)"
          >
            <X :size="16" aria-hidden="true" />
          </button>
          <span v-else class="upload-queue__spinner" aria-hidden="true"></span>
        </li>
      </ul>
    </section>

    <section v-if="activeTab === 'TRASH'" class="trash-toolbar">
      <div>
        <strong>{{ custom.trash }}</strong>
        <p>{{ custom.trashContract }}</p>
      </div>
      <button
        class="button button--danger"
        type="button"
        disabled
        :title="custom.contractUnavailable"
      >
        <Trash2 :size="16" aria-hidden="true" />
        {{ custom.emptyTrash }}
      </button>
    </section>

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
            <h2>
              {{
                activeTab === "TRASH"
                  ? custom.trashEmpty
                  : $t("files.noMatches")
              }}
            </h2>
            <p>
              {{
                activeTab === "TRASH"
                  ? custom.trashContract
                  : $t("files.noMatchesText")
              }}
            </p>
          </section>

          <div v-else-if="viewMode === 'grid'" class="files-grid" role="list">
            <div
              v-for="artifact in filteredArtifacts"
              :key="artifact.ref"
              class="file-collection-item file-collection-item--tile"
              role="listitem"
            >
              <button
                class="file-tile"
                :class="{
                  'file-tile--selected': selectedRef === artifact.ref,
                }"
                type="button"
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
                  <span class="mono">{{
                    formatBytes(artifact.sizeBytes)
                  }}</span>
                  <span class="mono">v{{ artifact.revision }}</span>
                </span>
                <StatusBadge :state="artifact.scanState" />
              </button>
              <button
                class="icon-button file-collection-item__lifecycle"
                :class="{
                  'file-collection-item__lifecycle--danger':
                    activeTab !== 'TRASH',
                }"
                type="button"
                :title="activeTab === 'TRASH' ? custom.restore : custom.delete"
                :aria-label="`${
                  activeTab === 'TRASH' ? custom.restore : custom.delete
                }: ${artifact.fileName}`"
                @click="
                  openLifecycleDialog(
                    artifact,
                    activeTab === 'TRASH' ? 'RESTORE' : 'DELETE',
                  )
                "
              >
                <RotateCcw
                  v-if="activeTab === 'TRASH'"
                  :size="16"
                  aria-hidden="true"
                />
                <Trash2 v-else :size="16" aria-hidden="true" />
              </button>
            </div>
          </div>

          <div v-else class="files-list" role="list">
            <div class="files-list__head desktop-only" aria-hidden="true">
              <span>{{ $t("files.file") }}</span>
              <span>{{ $t("files.usedBy") }}</span>
              <span>{{ $t("files.revision") }}</span>
              <span>{{ $t("common.status") }}</span>
              <span></span>
            </div>
            <div
              v-for="artifact in filteredArtifacts"
              :key="artifact.ref"
              class="file-collection-item"
              role="listitem"
            >
              <button
                class="file-list-row"
                :class="{
                  'file-list-row--selected': selectedRef === artifact.ref,
                }"
                type="button"
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
              <button
                class="icon-button file-collection-item__lifecycle"
                :class="{
                  'file-collection-item__lifecycle--danger':
                    activeTab !== 'TRASH',
                }"
                type="button"
                :title="activeTab === 'TRASH' ? custom.restore : custom.delete"
                :aria-label="`${
                  activeTab === 'TRASH' ? custom.restore : custom.delete
                }: ${artifact.fileName}`"
                @click="
                  openLifecycleDialog(
                    artifact,
                    activeTab === 'TRASH' ? 'RESTORE' : 'DELETE',
                  )
                "
              >
                <RotateCcw
                  v-if="activeTab === 'TRASH'"
                  :size="16"
                  aria-hidden="true"
                />
                <Trash2 v-else :size="16" aria-hidden="true" />
              </button>
            </div>
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
          <button
            class="button file-details__lifecycle"
            :class="activeTab === 'TRASH' ? '' : 'button--danger'"
            type="button"
            @click="
              openLifecycleDialog(
                selectedArtifact,
                activeTab === 'TRASH' ? 'RESTORE' : 'DELETE',
              )
            "
          >
            <RotateCcw
              v-if="activeTab === 'TRASH'"
              :size="16"
              aria-hidden="true"
            />
            <Trash2 v-else :size="16" aria-hidden="true" />
            {{ activeTab === "TRASH" ? custom.restore : custom.delete }}
          </button>
          <button
            v-if="activeTab === 'TRASH'"
            class="button button--danger file-details__lifecycle"
            type="button"
            @click="openLifecycleDialog(selectedArtifact, 'PURGE')"
          >
            <Trash2 :size="16" aria-hidden="true" />
            {{ custom.purge }}
          </button>
        </aside>
      </div>
    </AsyncState>

    <FilePreviewDialog
      v-if="previewOpen && selectedArtifact"
      :artifact="selectedArtifact"
      :image-url="previewImage"
      :labels="custom.preview"
      :delete-label="activeTab === 'TRASH' ? custom.restore : custom.delete"
      :lifecycle-action="activeTab === 'TRASH' ? 'RESTORE' : 'DELETE'"
      :loading="contentBusy"
      :preview-text="previewText"
      :unavailable="previewUnavailable"
      :format-bytes="formatBytes"
      :format-date="formatDate"
      :source-label="sourceLabel"
      @close="closePreview"
      @download="download(selectedArtifact)"
      @request-delete="
        openLifecycleDialog(
          selectedArtifact,
          activeTab === 'TRASH' ? 'RESTORE' : 'DELETE',
        );
        closePreview();
      "
    />

    <FileLifecycleDialog
      v-if="lifecycleDialog"
      :action="lifecycleDialog.action"
      :artifact="lifecycleDialog.artifact"
      :busy="contentBusy"
      :labels="{
        cancel: custom.cancel,
        confirm: custom.lifecycle.confirm,
        description: custom.lifecycle.description,
        impactUnavailable: custom.impactUnavailable,
        reason: custom.lifecycle.reason,
        title: custom.lifecycle.title,
      }"
      :state="lifecycleDialog.state"
      @close="lifecycleDialog = undefined"
      @confirm="confirmLifecycleOperation"
    />
  </section>
</template>

<style scoped>
.files-workspace {
  position: relative;
  display: grid;
  min-height: 640px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.files-workspace--drag-active {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.files-workspace__drop-overlay {
  position: absolute;
  z-index: 20;
  inset: 8px;
  display: grid;
  place-items: center;
  align-content: center;
  gap: 10px;
  border: 2px dashed var(--accent);
  border-radius: 8px;
  background: color-mix(in srgb, var(--surface) 90%, transparent);
  color: var(--accent-strong);
  pointer-events: none;
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
  max-width: 168px;
}
.files-workspace__search {
  display: flex;
  min-width: 210px;
  flex: 1 1 320px;
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
.files-workspace > .problem-notice,
.files-workspace > .field-error {
  margin: 10px 14px 0;
}
.files-workspace__layout {
  display: grid;
  min-height: 540px;
  grid-template-columns: minmax(0, 1fr) minmax(240px, 280px);
}
.upload-queue,
.trash-toolbar {
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.upload-queue {
  padding: 10px 14px;
}
.upload-queue header,
.trash-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}
.upload-queue header > div {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.upload-queue ul {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 8px;
  padding: 0;
  margin: 10px 0 0;
  list-style: none;
}
.upload-queue li {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 9px;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
}
.upload-queue li > span:nth-child(2),
.upload-queue li strong,
.upload-queue li small {
  min-width: 0;
  display: block;
}
.upload-queue li strong,
.upload-queue li small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.upload-queue li small {
  margin-top: 2px;
  color: var(--muted);
  font-size: 0.74rem;
}
.upload-queue__spinner {
  width: 17px;
  height: 17px;
  border: 2px solid var(--border-strong);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: upload-spin 0.8s linear infinite;
}
.trash-toolbar {
  padding: 10px 14px;
}
.trash-toolbar p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 0.8rem;
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
.file-collection-item {
  position: relative;
  min-width: 0;
}
.file-collection-item > .file-tile,
.file-collection-item > .file-list-row {
  width: 100%;
}
.file-tile {
  display: grid;
  min-width: 0;
  min-height: 196px;
  align-content: start;
  justify-items: start;
  gap: 8px;
  padding: 12px 44px 12px 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.file-collection-item__lifecycle {
  position: absolute;
  z-index: 1;
  top: 9px;
  right: 9px;
  border: 1px solid var(--border);
  background: var(--surface);
}
.file-collection-item__lifecycle--danger {
  color: var(--danger, #b42318);
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
  padding: 8px 52px 8px 14px;
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
.file-details__lifecycle {
  width: 100%;
  justify-content: center;
  margin-top: 8px;
}
@keyframes upload-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 980px) {
  .files-workspace__layout {
    grid-template-columns: minmax(0, 1fr) minmax(220px, 250px);
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
  .upload-queue,
  .trash-toolbar {
    margin: 0 -16px;
  }
  .trash-toolbar {
    align-items: stretch;
    flex-direction: column;
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
    padding: 10px 48px 10px 2px;
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
