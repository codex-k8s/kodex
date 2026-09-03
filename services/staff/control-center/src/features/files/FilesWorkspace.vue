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
import { RouterLink } from "vue-router";

import {
  deleteArtifactItem,
  loadArtifactImpact,
  loadArtifactPage,
  mutateArtifactsSequentially,
  purgeArtifactItem,
  restoreArtifactItem,
  uploadArtifactItem,
  type ArtifactBulkReceipt,
} from "@/features/files/api";
import FileLifecycleDialog from "@/features/files/FileLifecycleDialog.vue";
import FilePreviewDialog from "@/features/files/FilePreviewDialog.vue";
import FileTypeIcon from "@/features/files/FileTypeIcon.vue";
import TrashBulkDialog from "@/features/files/TrashBulkDialog.vue";
import {
  artifactLifecycleState,
  artifactLifecycleAnnounced,
  artifactSourceKinds,
  artifactSourcesForTab,
  createUploadQueueItems,
  nextUploadQueueItems,
  supportsInlinePreview,
  uploadProgressPercent,
  type ArtifactLifecycleAction,
  type ArtifactLifecycleState,
  type ArtifactTrashBulkAction,
  type FileCollectionMode,
  type FileKind,
  type FilePreviewLabels,
  type FileSource,
  type FileTab,
  type UploadQueueItem,
} from "@/features/files/model";
import { usePlatformStore } from "@/features/platform/store";
import type {
  Artifact,
  ArtifactImpact,
} from "@/shared/api/generated/openapi/types.gen";
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
  mode: FileCollectionMode;
  initialArtifactRef?: string;
}>();
const maximumUploadBytes = 512 << 20;
const maximumTextPreviewBytes = 256 << 10;
const viewPreferenceKey = "kodex.files.view";
const platform = usePlatformStore();
const { locale, t } = useI18n();
const fileInput = ref<HTMLInputElement>();
const scrollRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const activeTab = ref<Exclude<FileTab, "TRASH">>("FILES");
const kind = ref<FileKind>("ALL");
const scanState = ref<"ALL" | Artifact["scanState"]>("ALL");
const source = ref<FileSource>("ALL");
const viewMode = ref<ViewMode>("list");
const selectedRef = ref(props.initialArtifactRef ?? "");
const selectedRefs = ref<string[]>([]);
const uploadQueue = ref<UploadQueueItem[]>([]);
const activeUploadCount = ref(0);
const uploadControllers = new Map<string, AbortController>();
const uploadRefreshPending = ref(false);
const dragDepth = ref(0);
const dragActive = computed(() => dragDepth.value > 0);
const trashMode = computed(() => props.mode === "TRASH");
const filesPath = computed(
  () => `/projects/${encodeURIComponent(props.projectRef)}/files`,
);
const trashPath = computed(() => `${filesPath.value}/trash`);
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
const bulkDialog = ref<{
  action: ArtifactTrashBulkAction;
  artifacts: Artifact[];
  impacts: Record<string, ArtifactImpact>;
}>();
const bulkReceipts = ref<ArtifactBulkReceipt[]>([]);
let uploadSequence = 0;
let disposed = false;

const collectionTab = computed<FileTab>(() =>
  trashMode.value ? "TRASH" : activeTab.value,
);
const sourceOptions = computed(() =>
  artifactSourcesForTab(collectionTab.value),
);

const collection = useAsyncEntityCollection(
  (request) =>
    loadArtifactPage(props.projectRef, request, {
      lifecycleState: trashMode.value ? "DELETED" : "ACTIVE",
      ...(trashMode.value && source.value === "ALL"
        ? { allSources: true as const }
        : {
            sourceKinds: artifactSourceKinds(collectionTab.value, source.value),
          }),
      ...(kind.value === "ALL" ? {} : { type: kind.value }),
      ...(scanState.value === "ALL" ? {} : { scanState: scanState.value }),
    }),
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
const filteredArtifacts = computed(() => loadedArtifacts.value);
const selectedArtifact = computed(() =>
  loadedArtifacts.value.find((artifact) => artifact.ref === selectedRef.value),
);
const selectedArtifacts = computed(() => {
  const refs = new Set(selectedRefs.value);
  return loadedArtifacts.value.filter((artifact) => refs.has(artifact.ref));
});
const selectableArtifacts = computed(() =>
  filteredArtifacts.value.filter((artifact) =>
    artifactLifecycleAnnounced(
      artifact,
      trashMode.value ? "RESTORE" : "DELETE",
    ),
  ),
);
const allVisibleSelected = computed(
  () =>
    selectableArtifacts.value.length > 0 &&
    selectableArtifacts.value.every((artifact) =>
      selectedRefs.value.includes(artifact.ref),
    ),
);
const canEmptyTrash = computed(
  () =>
    trashMode.value &&
    loadedArtifacts.value.some((artifact) =>
      artifactLifecycleAnnounced(artifact, "PURGE"),
    ),
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
          "This action is unavailable for the file. No file was changed.",
        activeRunsTitle: "Affected active runs",
        activeRunsUnavailable:
          "The affected active runs cannot be checked right now, so destructive actions are disabled. Already materialized input is not silently revoked.",
        activeSelection: "Selected files",
        allFiles: "Files",
        bulkDeleteContract:
          "Selected active files will be moved to trash and remain recoverable for 30 days.",
        bulkFailed: "Failed",
        bulkResultTitle: "Operation results",
        bulkSucceeded: "Completed",
        cancel: "Cancel",
        cancelUpload: "Cancel upload",
        clearSearch: "Clear search",
        confirmationHint:
          "Type the confirmation phrase exactly. This operation cannot be undone.",
        confirmationPhrase: "DELETE PERMANENTLY",
        delete: "Move to trash",
        deleteDescription:
          "The file will stop being available to new runs and can be restored for 30 days.",
        dropFiles: "Drop files to upload",
        emptyTrash: "Empty trash",
        failed: "Upload failed",
        grid: "Grid",
        impactUnavailable:
          "Already materialized inputs remain available to an active session; new turns no longer receive this file.",
        impactBlocked:
          "Resolve the listed dependencies or cancel the affected runs before retrying.",
        impactPreflight:
          "Dependencies and active runs are checked immediately before a destructive action.",
        impact: {
          activeRuns: "Active runs",
          activeRunsTruncated: "Only the first affected runs are shown.",
          attachments: "Immutable attachments",
          bindings: "Employee bindings",
          openRun: "Open and cancel",
          summary: "Deletion impact",
        },
        loaded: "Loaded",
        loadingMore: "Loading more…",
        knowledgeSources: "Knowledge sources",
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
          "Files remain recoverable for 30 days. Permanent deletion removes the exact object version from storage.",
        trashEmpty: "No deleted files are available in this response.",
        purgeAt: "Permanent deletion",
        selectAll: "Select all loaded files",
        selected: "Selected",
        sequentialHint:
          "Files are processed one by one. A failure does not roll back successful operations and is reported separately.",
        uploadMore: "Upload",
        uploadTooLarge: "The file must not exceed 512 MB.",
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
              "This action is unavailable for the file in its current state.",
            IMPACT_BLOCKED:
              "The file is still used by active or immutable resources.",
            IMPACT_UNAVAILABLE:
              "The impact on active runs could not be checked, so the action is disabled.",
          },
          title: {
            DELETE: "Move file to trash?",
            PURGE: "Delete file permanently?",
            RESTORE: "Restore file?",
          },
        },
        bulk: {
          confirm: {
            DELETE: "Move to trash",
            EMPTY: "Empty trash",
            PURGE: "Delete permanently",
            RESTORE: "Restore selected",
          },
          description: {
            DELETE: "selected files will be moved to trash.",
            EMPTY: "files in the trash will be permanently deleted.",
            PURGE: "selected files will be permanently deleted.",
            RESTORE: "selected files will be restored to the Project.",
          },
          title: {
            DELETE: "Move selected files to trash?",
            EMPTY: "Empty the entire trash?",
            PURGE: "Delete selected files permanently?",
            RESTORE: "Restore selected files?",
          },
        },
      }
    : {
        actionUnavailable:
          "Это действие сейчас недоступно для файла. Файл не был изменён.",
        activeRunsTitle: "Затронутые активные запуски",
        activeRunsUnavailable:
          "Сейчас нельзя проверить затронутые активные запуски, поэтому необратимые действия отключены. Уже материализованный вход не отзывается скрыто.",
        activeSelection: "Выбранные файлы",
        allFiles: "Файлы",
        bulkDeleteContract:
          "Выбранные активные файлы будут перемещены в корзину и останутся доступными для восстановления 30 дней.",
        bulkFailed: "Ошибка",
        bulkResultTitle: "Результаты операции",
        bulkSucceeded: "Выполнено",
        cancel: "Отмена",
        cancelUpload: "Отменить загрузку",
        clearSearch: "Очистить поиск",
        confirmationHint:
          "Введите фразу подтверждения без изменений. Операцию нельзя отменить.",
        confirmationPhrase: "УДАЛИТЬ НАВСЕГДА",
        delete: "В корзину",
        deleteDescription:
          "Файл перестанет выдаваться новым запускам, но его можно будет восстановить в течение 30 дней.",
        dropFiles: "Перетащите файлы для загрузки",
        emptyTrash: "Очистить корзину",
        failed: "Не удалось загрузить",
        grid: "Сетка",
        impactUnavailable:
          "Уже материализованный вход останется у активной сессии; новые ходы этот файл больше не получат.",
        impactBlocked:
          "Устраните указанные зависимости или отмените затронутые запуски и повторите действие.",
        impactPreflight:
          "Зависимости и активные запуски проверяются непосредственно перед необратимым действием.",
        impact: {
          activeRuns: "Активные запуски",
          activeRunsTruncated: "Показаны только первые затронутые запуски.",
          attachments: "Неизменяемые вложения",
          bindings: "Привязки к сотрудникам",
          openRun: "Открыть и отменить",
          summary: "Влияние удаления",
        },
        loaded: "Загружено",
        loadingMore: "Загружаем ещё…",
        knowledgeSources: "Источники знаний",
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
          "Файлы можно восстановить в течение 30 дней. Необратимое удаление стирает точную версию объекта из хранилища.",
        trashEmpty: "В текущем ответе нет удалённых файлов.",
        purgeAt: "Необратимое удаление",
        selectAll: "Выбрать все загруженные файлы",
        selected: "Выбрано",
        sequentialHint:
          "Файлы обрабатываются последовательно. Ошибка одного файла не отменяет успешные операции и показывается отдельно.",
        uploadMore: "Загрузить",
        uploadTooLarge: "Размер файла не должен превышать 512 МБ.",
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
            ACTION_NOT_ALLOWED:
              "Действие недоступно для файла в его текущем состоянии.",
            IMPACT_BLOCKED:
              "Файл используется активными или неизменяемыми ресурсами.",
            IMPACT_UNAVAILABLE:
              "Не удалось проверить влияние на активные запуски, поэтому действие отключено.",
          },
          title: {
            DELETE: "Переместить файл в корзину?",
            PURGE: "Удалить файл навсегда?",
            RESTORE: "Восстановить файл?",
          },
        },
        bulk: {
          confirm: {
            DELETE: "Переместить в корзину",
            EMPTY: "Очистить корзину",
            PURGE: "Удалить навсегда",
            RESTORE: "Восстановить выбранные",
          },
          description: {
            DELETE: "выбранных файлов будут перемещены в корзину.",
            EMPTY: "файлов в корзине будут необратимо удалены.",
            PURGE: "выбранных файлов будут необратимо удалены.",
            RESTORE: "выбранных файлов будут восстановлены в Проекте.",
          },
          title: {
            DELETE: "Переместить выбранные файлы в корзину?",
            EMPTY: "Очистить всю корзину?",
            PURGE: "Удалить выбранные файлы навсегда?",
            RESTORE: "Восстановить выбранные файлы?",
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
  source.value = "ALL";
});
watch([activeTab, kind, scanState, source], () => {
  selectedRef.value = "";
  selectedRefs.value = [];
  refresh();
});
watch(
  () => props.mode,
  () => {
    selectedRef.value = "";
    selectedRefs.value = [];
    refresh();
  },
);

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
  update: Partial<Pick<UploadQueueItem, "problem" | "progress" | "state">>,
): void {
  uploadQueue.value = uploadQueue.value.map((item) =>
    item.id === id ? { ...item, ...update } : item,
  );
}

function enqueueFiles(files: readonly File[]): void {
  if (!canUpload.value || files.length === 0) return;
  if (!trashMode.value) activeTab.value = "FILES";
  operationProblem.value = undefined;
  validationMessage.value = "";
  const queued = createUploadQueueItems(files, () => {
    uploadSequence += 1;
    return `upload-${String(uploadSequence)}`;
  }).map((item) =>
    item.file.size > maximumUploadBytes
      ? {
          ...item,
          problem: custom.value.uploadTooLarge,
          state: "FAILED" as const,
        }
      : item,
  );
  uploadQueue.value = [...uploadQueue.value, ...queued];
  processUploadQueue();
}

async function uploadQueueItem(item: UploadQueueItem): Promise<void> {
  const current = uploadQueue.value.find(
    (candidate) => candidate.id === item.id,
  );
  if (!current || current.state !== "QUEUED") return;
  const controller = new AbortController();
  uploadControllers.set(item.id, controller);
  activeUploadCount.value += 1;
  updateUploadQueueItem(item.id, {
    problem: undefined,
    progress: { loadedBytes: 0, totalBytes: item.file.size },
    state: "UPLOADING",
  });
  try {
    const artifact = await uploadArtifactItem(props.projectRef, item.file, {
      signal: controller.signal,
      onProgress: (progress) => {
        const queued = uploadQueue.value.find(
          (candidate) => candidate.id === item.id,
        );
        if (queued?.state === "UPLOADING")
          updateUploadQueueItem(item.id, { progress });
      },
    });
    if (!uploadQueue.value.some((candidate) => candidate.id === item.id))
      return;
    selectedRef.value = artifact.ref;
    uploadRefreshPending.value = true;
    updateUploadQueueItem(item.id, {
      progress: { loadedBytes: item.file.size, totalBytes: item.file.size },
      state: "SUCCEEDED",
    });
  } catch (error) {
    if (
      !controller.signal.aborted &&
      uploadQueue.value.some((candidate) => candidate.id === item.id)
    ) {
      const problem = asProblem(error);
      updateUploadQueueItem(item.id, {
        problem: problem.detail || problem.title || custom.value.failed,
        progress: undefined,
        state: "FAILED",
      });
    }
  } finally {
    if (uploadControllers.get(item.id) === controller)
      uploadControllers.delete(item.id);
    activeUploadCount.value = Math.max(0, activeUploadCount.value - 1);
    processUploadQueue();
    if (
      !disposed &&
      activeUploadCount.value === 0 &&
      !uploadQueue.value.some((candidate) => candidate.state === "QUEUED") &&
      uploadRefreshPending.value
    ) {
      uploadRefreshPending.value = false;
      refresh();
    }
  }
}

function processUploadQueue(): void {
  if (disposed) return;
  for (const item of nextUploadQueueItems(uploadQueue.value))
    void uploadQueueItem(item);
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
  updateUploadQueueItem(id, {
    problem: undefined,
    progress: undefined,
    state: "QUEUED",
  });
  processUploadQueue();
}

function removeUpload(id: string): void {
  if (
    uploadQueue.value.some(
      (item) => item.id === id && item.state === "UPLOADING",
    )
  )
    uploadRefreshPending.value = true;
  uploadQueue.value = uploadQueue.value.filter((item) => item.id !== id);
  uploadControllers.get(id)?.abort();
  processUploadQueue();
}

function clearFinishedUploads(): void {
  uploadQueue.value = uploadQueue.value.filter(
    (item) => item.state === "UPLOADING" || item.state === "QUEUED",
  );
}

async function resolveLifecycleState(
  artifact: Artifact,
  action: ArtifactLifecycleAction,
): Promise<ArtifactLifecycleState> {
  if (action === "RESTORE") return artifactLifecycleState(artifact, action);
  try {
    return artifactLifecycleState(
      artifact,
      action,
      await loadArtifactImpact(artifact, action),
    );
  } catch {
    return artifactLifecycleState(artifact, action);
  }
}

async function openLifecycleDialog(
  artifact: Artifact,
  action: ArtifactLifecycleAction,
): Promise<void> {
  if (!artifactLifecycleAnnounced(artifact, action)) {
    validationMessage.value = custom.value.actionUnavailable;
    return;
  }
  contentBusy.value = true;
  validationMessage.value = "";
  try {
    lifecycleDialog.value = {
      action,
      artifact,
      state: await resolveLifecycleState(artifact, action),
    };
  } finally {
    contentBusy.value = false;
  }
}

function lifecycleBlockLabel(
  artifact: Artifact,
  action: ArtifactLifecycleAction,
): string | undefined {
  return artifactLifecycleAnnounced(artifact, action)
    ? undefined
    : custom.value.lifecycle.reason.ACTION_NOT_ALLOWED;
}

function toggleSelection(artifactRef: string, selected: boolean): void {
  const refs = new Set(selectedRefs.value);
  if (selected) refs.add(artifactRef);
  else refs.delete(artifactRef);
  selectedRefs.value = [...refs];
}

function toggleAllVisible(): void {
  const refs = new Set(selectedRefs.value);
  if (allVisibleSelected.value) {
    for (const artifact of selectableArtifacts.value) refs.delete(artifact.ref);
  } else {
    for (const artifact of selectableArtifacts.value) refs.add(artifact.ref);
  }
  selectedRefs.value = [...refs];
}

async function resolveBulkLifecycle(
  artifacts: readonly Artifact[],
  action: Extract<ArtifactTrashBulkAction, "DELETE" | "RESTORE" | "PURGE">,
): Promise<
  | {
      available: true;
      artifacts: Artifact[];
      impacts: Record<string, ArtifactImpact>;
    }
  | { available: false; artifact: Artifact; state: ArtifactLifecycleState }
> {
  const permitted: Artifact[] = [];
  const impacts: Record<string, ArtifactImpact> = {};
  for (const artifact of artifacts) {
    const state = await resolveLifecycleState(artifact, action);
    if (!state.available) return { artifact, available: false, state };
    permitted.push(artifact);
    if (state.impact) impacts[artifact.ref] = state.impact;
  }
  return { artifacts: permitted, available: true, impacts };
}

async function openSelectedBulk(
  action: Extract<ArtifactTrashBulkAction, "DELETE" | "RESTORE" | "PURGE">,
): Promise<void> {
  const artifacts = selectedArtifacts.value.filter((artifact) =>
    artifactLifecycleAnnounced(artifact, action),
  );
  if (artifacts.length === 0) return;
  contentBusy.value = true;
  try {
    const result = await resolveBulkLifecycle(artifacts, action);
    if (!result.available) {
      lifecycleDialog.value = {
        action,
        artifact: result.artifact,
        state: result.state,
      };
      return;
    }
    bulkReceipts.value = [];
    bulkDialog.value = {
      action,
      artifacts: result.artifacts,
      impacts: result.impacts,
    };
  } finally {
    contentBusy.value = false;
  }
}

async function loadEntireTrash(): Promise<Artifact[]> {
  const controller = new AbortController();
  const artifacts: Artifact[] = [];
  const seenCursors = new Set<string>();
  let cursor: string | undefined;
  do {
    if (cursor && seenCursors.has(cursor))
      throw new Error("Artifact trash cursor did not advance");
    if (cursor) seenCursors.add(cursor);
    const page = await loadArtifactPage(
      props.projectRef,
      { cursor, query: "", signal: controller.signal },
      {
        allSources: true,
        lifecycleState: "DELETED",
      },
    );
    artifacts.push(...page.items.map((item) => item.artifact));
    cursor = page.nextCursor ?? undefined;
  } while (cursor);
  return artifacts;
}

async function openEmptyTrash(): Promise<void> {
  contentBusy.value = true;
  operationProblem.value = undefined;
  try {
    const artifacts = (await loadEntireTrash()).filter((artifact) =>
      artifactLifecycleAnnounced(artifact, "PURGE"),
    );
    if (artifacts.length > 0) {
      const result = await resolveBulkLifecycle(artifacts, "PURGE");
      if (!result.available) {
        lifecycleDialog.value = {
          action: "PURGE",
          artifact: result.artifact,
          state: result.state,
        };
        return;
      }
      bulkReceipts.value = [];
      bulkDialog.value = {
        action: "EMPTY",
        artifacts: result.artifacts,
        impacts: result.impacts,
      };
    } else validationMessage.value = custom.value.actionUnavailable;
  } catch (error) {
    operationProblem.value = asProblem(error);
  } finally {
    contentBusy.value = false;
  }
}

async function confirmBulkOperation(): Promise<void> {
  const operation = bulkDialog.value;
  if (!operation) return;
  contentBusy.value = true;
  operationProblem.value = undefined;
  try {
    bulkReceipts.value = await mutateArtifactsSequentially(
      operation.artifacts,
      async (artifact) => {
        const impact = operation.impacts[artifact.ref];
        if (operation.action === "DELETE") {
          if (!impact) throw new Error("Artifact delete impact is unavailable");
          await deleteArtifactItem(artifact, impact);
        } else if (operation.action === "RESTORE")
          await restoreArtifactItem(artifact);
        else {
          if (!impact) throw new Error("Artifact purge impact is unavailable");
          await purgeArtifactItem(artifact, impact);
        }
      },
    );
    bulkDialog.value = undefined;
    selectedRefs.value = bulkReceipts.value
      .filter((receipt) => receipt.status === "FAILED")
      .map((receipt) => receipt.artifact.ref);
    selectedRef.value = "";
    refresh();
  } finally {
    contentBusy.value = false;
  }
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
    if (operation.action === "DELETE") {
      if (!operation.state.impact)
        throw new Error("Artifact delete impact is unavailable");
      replaceArtifact(
        await deleteArtifactItem(operation.artifact, operation.state.impact),
      );
    } else if (operation.action === "RESTORE")
      replaceArtifact(await restoreArtifactItem(operation.artifact));
    else {
      if (!operation.state.impact)
        throw new Error("Artifact purge impact is unavailable");
      await purgeArtifactItem(operation.artifact, operation.state.impact);
    }
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
onBeforeUnmount(() => {
  disposed = true;
  for (const controller of uploadControllers.values()) controller.abort();
  uploadControllers.clear();
  clearPreview();
});
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
      :disabled="!canUpload || trashMode"
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
      <label v-if="!trashMode">
        <span class="sr-only">{{ custom.viewFilter }}</span>
        <select v-model="activeTab" :aria-label="custom.viewFilter">
          <option value="FILES">{{ $t("files.tab.FILES") }}</option>
          <option value="KNOWLEDGE">{{ custom.knowledgeSources }}</option>
          <option value="RESULTS">{{ $t("files.tab.RESULTS") }}</option>
        </select>
      </label>
      <RouterLink
        class="button button--small"
        :to="trashMode ? filesPath : trashPath"
      >
        <RotateCcw v-if="trashMode" :size="16" aria-hidden="true" />
        <Trash2 v-else :size="16" aria-hidden="true" />
        {{ trashMode ? custom.allFiles : custom.trash }}
      </RouterLink>
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
          <option
            v-for="sourceOption in sourceOptions"
            :key="sourceOption"
            :value="sourceOption"
          >
            {{ $t(`files.source.${sourceOption}`) }}
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
        v-if="canUpload && !trashMode"
        class="button button--primary"
        type="button"
        @click="fileInput?.click()"
      >
        <Upload :size="16" aria-hidden="true" />
        {{ custom.uploadMore }}
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
                  ? `${custom.uploading}${
                      uploadProgressPercent(item.progress) === undefined
                        ? ""
                        : ` ${uploadProgressPercent(item.progress)}%`
                    }`
                  : item.state === "QUEUED"
                    ? custom.queued
                    : item.state === "FAILED"
                      ? item.problem || custom.failed
                      : $t("states.CLEAN")
              }}
            </small>
            <progress
              v-if="item.state === 'UPLOADING'"
              :value="item.progress?.loadedBytes"
              :max="Math.max(1, item.file.size)"
            ></progress>
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
            v-else
            class="icon-button"
            type="button"
            :title="
              item.state === 'UPLOADING'
                ? custom.cancelUpload
                : custom.removeFromQueue
            "
            :aria-label="`${
              item.state === 'UPLOADING'
                ? custom.cancelUpload
                : custom.removeFromQueue
            }: ${item.file.name}`"
            @click="removeUpload(item.id)"
          >
            <X :size="16" aria-hidden="true" />
          </button>
        </li>
      </ul>
    </section>

    <section class="trash-toolbar">
      <div>
        <strong>{{ trashMode ? custom.trash : custom.activeSelection }}</strong>
        <p>
          {{ trashMode ? custom.trashContract : custom.bulkDeleteContract }}
        </p>
      </div>
      <div class="trash-toolbar__actions">
        <label class="trash-toolbar__select-all">
          <input
            type="checkbox"
            :checked="allVisibleSelected"
            :disabled="selectableArtifacts.length === 0 || contentBusy"
            @change="toggleAllVisible"
          />
          <span>{{ custom.selectAll }}</span>
        </label>
        <span v-if="selectedArtifacts.length > 0" class="mono">
          {{ custom.selected }} {{ selectedArtifacts.length }}
        </span>
        <button
          v-if="!trashMode && selectedArtifacts.length > 0"
          class="button button--small button--danger"
          type="button"
          :disabled="contentBusy"
          @click="openSelectedBulk('DELETE')"
        >
          <Trash2 :size="16" aria-hidden="true" />
          {{ custom.bulk.confirm.DELETE }}
        </button>
        <button
          v-if="trashMode && selectedArtifacts.length > 0"
          class="button button--small"
          type="button"
          :disabled="contentBusy"
          @click="openSelectedBulk('RESTORE')"
        >
          <RotateCcw :size="16" aria-hidden="true" />
          {{ custom.bulk.confirm.RESTORE }}
        </button>
        <button
          v-if="trashMode && selectedArtifacts.length > 0"
          class="button button--small button--danger"
          type="button"
          :disabled="contentBusy"
          @click="openSelectedBulk('PURGE')"
        >
          <Trash2 :size="16" aria-hidden="true" />
          {{ custom.bulk.confirm.PURGE }}
        </button>
        <button
          v-if="trashMode"
          class="button button--danger"
          type="button"
          :disabled="contentBusy || !canEmptyTrash"
          :title="canEmptyTrash ? undefined : custom.actionUnavailable"
          @click="openEmptyTrash"
        >
          <Trash2 :size="16" aria-hidden="true" />
          {{ custom.emptyTrash }}
        </button>
      </div>
    </section>

    <section
      v-if="bulkReceipts.length > 0"
      class="bulk-operation-receipts"
      aria-live="polite"
    >
      <header>
        <strong>{{ custom.bulkResultTitle }}</strong>
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('common.close')"
          :title="$t('common.close')"
          @click="bulkReceipts = []"
        >
          <X :size="15" aria-hidden="true" />
        </button>
      </header>
      <ul>
        <li v-for="receipt in bulkReceipts" :key="receipt.artifact.ref">
          <strong>{{ receipt.artifact.fileName }}</strong>
          <span
            :class="
              receipt.status === 'SUCCEEDED'
                ? 'bulk-operation-receipts__success'
                : 'bulk-operation-receipts__failure'
            "
          >
            {{
              receipt.status === "SUCCEEDED"
                ? custom.bulkSucceeded
                : custom.bulkFailed
            }}
          </span>
          <small v-if="receipt.problem">
            {{
              receipt.problem.detail ||
              receipt.problem.title ||
              custom.bulkFailed
            }}
          </small>
        </li>
      </ul>
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
      <div
        class="files-workspace__layout"
        :class="{
          'files-workspace__layout--details': Boolean(selectedArtifact),
        }"
      >
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
              {{ trashMode ? custom.trashEmpty : $t("files.noMatches") }}
            </h2>
            <p>
              {{ trashMode ? custom.trashContract : $t("files.noMatchesText") }}
            </p>
          </section>

          <div v-else-if="viewMode === 'grid'" class="files-grid" role="list">
            <div
              v-for="artifact in filteredArtifacts"
              :key="artifact.ref"
              class="file-collection-item file-collection-item--tile"
              role="listitem"
              :data-artifact-ref="artifact.ref"
            >
              <label
                class="file-collection-item__select"
                :aria-label="`${custom.selected}: ${artifact.fileName}`"
              >
                <input
                  type="checkbox"
                  :checked="selectedRefs.includes(artifact.ref)"
                  :disabled="
                    !artifactLifecycleState(
                      artifact,
                      trashMode ? 'RESTORE' : 'DELETE',
                    ).available
                  "
                  @change="
                    toggleSelection(
                      artifact.ref,
                      ($event.target as HTMLInputElement).checked,
                    )
                  "
                />
              </label>
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
              <div class="file-collection-item__actions">
                <button
                  class="icon-button"
                  type="button"
                  :title="$t('files.openPreview')"
                  :aria-label="`${$t('files.openPreview')}: ${artifact.fileName}`"
                  @click="openPreview(artifact)"
                >
                  <Eye :size="16" aria-hidden="true" />
                </button>
                <button
                  class="icon-button"
                  type="button"
                  :disabled="
                    contentBusy || !artifact.nextActions.includes('DOWNLOAD')
                  "
                  :title="
                    artifact.nextActions.includes('DOWNLOAD')
                      ? $t('common.download')
                      : custom.actionUnavailable
                  "
                  :aria-label="`${$t('common.download')}: ${artifact.fileName}`"
                  @click="download(artifact)"
                >
                  <Download :size="16" aria-hidden="true" />
                </button>
                <button
                  class="icon-button"
                  :class="{
                    'file-collection-item__lifecycle--danger': !trashMode,
                  }"
                  type="button"
                  :disabled="
                    contentBusy ||
                    Boolean(
                      lifecycleBlockLabel(
                        artifact,
                        trashMode ? 'RESTORE' : 'DELETE',
                      ),
                    )
                  "
                  :title="
                    lifecycleBlockLabel(
                      artifact,
                      trashMode ? 'RESTORE' : 'DELETE',
                    ) || (trashMode ? custom.restore : custom.delete)
                  "
                  :aria-label="`${
                    trashMode ? custom.restore : custom.delete
                  }: ${artifact.fileName}`"
                  @click="
                    openLifecycleDialog(
                      artifact,
                      trashMode ? 'RESTORE' : 'DELETE',
                    )
                  "
                >
                  <RotateCcw v-if="trashMode" :size="16" aria-hidden="true" />
                  <Trash2 v-else :size="16" aria-hidden="true" />
                </button>
                <button
                  v-if="trashMode"
                  class="icon-button file-collection-item__lifecycle--danger"
                  type="button"
                  :disabled="
                    contentBusy ||
                    Boolean(lifecycleBlockLabel(artifact, 'PURGE'))
                  "
                  :title="
                    lifecycleBlockLabel(artifact, 'PURGE') || custom.purge
                  "
                  :aria-label="`${custom.purge}: ${artifact.fileName}`"
                  @click="openLifecycleDialog(artifact, 'PURGE')"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                </button>
              </div>
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
              :class="{
                'file-collection-item--selectable': true,
              }"
              role="listitem"
              :data-artifact-ref="artifact.ref"
            >
              <label
                class="file-collection-item__select"
                :aria-label="`${custom.selected}: ${artifact.fileName}`"
              >
                <input
                  type="checkbox"
                  :checked="selectedRefs.includes(artifact.ref)"
                  :disabled="
                    !artifactLifecycleState(
                      artifact,
                      trashMode ? 'RESTORE' : 'DELETE',
                    ).available
                  "
                  @change="
                    toggleSelection(
                      artifact.ref,
                      ($event.target as HTMLInputElement).checked,
                    )
                  "
                />
              </label>
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
              <div class="file-collection-item__actions">
                <button
                  class="icon-button"
                  type="button"
                  :title="$t('files.openPreview')"
                  :aria-label="`${$t('files.openPreview')}: ${artifact.fileName}`"
                  @click="openPreview(artifact)"
                >
                  <Eye :size="16" aria-hidden="true" />
                </button>
                <button
                  class="icon-button"
                  type="button"
                  :disabled="
                    contentBusy || !artifact.nextActions.includes('DOWNLOAD')
                  "
                  :title="
                    artifact.nextActions.includes('DOWNLOAD')
                      ? $t('common.download')
                      : custom.actionUnavailable
                  "
                  :aria-label="`${$t('common.download')}: ${artifact.fileName}`"
                  @click="download(artifact)"
                >
                  <Download :size="16" aria-hidden="true" />
                </button>
                <button
                  class="icon-button"
                  :class="{
                    'file-collection-item__lifecycle--danger': !trashMode,
                  }"
                  type="button"
                  :disabled="
                    contentBusy ||
                    Boolean(
                      lifecycleBlockLabel(
                        artifact,
                        trashMode ? 'RESTORE' : 'DELETE',
                      ),
                    )
                  "
                  :title="
                    lifecycleBlockLabel(
                      artifact,
                      trashMode ? 'RESTORE' : 'DELETE',
                    ) || (trashMode ? custom.restore : custom.delete)
                  "
                  :aria-label="`${
                    trashMode ? custom.restore : custom.delete
                  }: ${artifact.fileName}`"
                  @click="
                    openLifecycleDialog(
                      artifact,
                      trashMode ? 'RESTORE' : 'DELETE',
                    )
                  "
                >
                  <RotateCcw v-if="trashMode" :size="16" aria-hidden="true" />
                  <Trash2 v-else :size="16" aria-hidden="true" />
                </button>
                <button
                  v-if="trashMode"
                  class="icon-button file-collection-item__lifecycle--danger"
                  type="button"
                  :disabled="
                    contentBusy ||
                    Boolean(lifecycleBlockLabel(artifact, 'PURGE'))
                  "
                  :title="
                    lifecycleBlockLabel(artifact, 'PURGE') || custom.purge
                  "
                  :aria-label="`${custom.purge}: ${artifact.fileName}`"
                  @click="openLifecycleDialog(artifact, 'PURGE')"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                </button>
              </div>
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
            <div v-if="selectedArtifact.deletedAt">
              <dt>{{ custom.trash }}</dt>
              <dd>{{ formatDate(selectedArtifact.deletedAt) }}</dd>
            </div>
            <div v-if="selectedArtifact.purgeAfter">
              <dt>{{ custom.purgeAt }}</dt>
              <dd>{{ formatDate(selectedArtifact.purgeAfter) }}</dd>
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
          <section class="file-details__impact">
            <h3>{{ custom.activeRunsTitle }}</h3>
            <p>{{ custom.impactPreflight }}</p>
          </section>
          <button
            class="button file-details__download"
            type="button"
            :disabled="
              contentBusy || !selectedArtifact.nextActions.includes('DOWNLOAD')
            "
            :title="
              selectedArtifact.nextActions.includes('DOWNLOAD')
                ? $t('common.download')
                : custom.actionUnavailable
            "
            @click="download(selectedArtifact)"
          >
            <Download :size="16" aria-hidden="true" />
            {{ $t("common.download") }}
          </button>
          <button
            class="button file-details__lifecycle"
            :class="trashMode ? '' : 'button--danger'"
            type="button"
            :disabled="
              contentBusy ||
              Boolean(
                lifecycleBlockLabel(
                  selectedArtifact,
                  trashMode ? 'RESTORE' : 'DELETE',
                ),
              )
            "
            :title="
              lifecycleBlockLabel(
                selectedArtifact,
                trashMode ? 'RESTORE' : 'DELETE',
              ) || (trashMode ? custom.restore : custom.delete)
            "
            @click="
              openLifecycleDialog(
                selectedArtifact,
                trashMode ? 'RESTORE' : 'DELETE',
              )
            "
          >
            <RotateCcw v-if="trashMode" :size="16" aria-hidden="true" />
            <Trash2 v-else :size="16" aria-hidden="true" />
            {{ trashMode ? custom.restore : custom.delete }}
          </button>
          <button
            v-if="trashMode"
            class="button button--danger file-details__lifecycle"
            type="button"
            :disabled="
              contentBusy ||
              Boolean(lifecycleBlockLabel(selectedArtifact, 'PURGE'))
            "
            :title="
              lifecycleBlockLabel(selectedArtifact, 'PURGE') || custom.purge
            "
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
      :delete-label="trashMode ? custom.restore : custom.delete"
      :lifecycle-action="trashMode ? 'RESTORE' : 'DELETE'"
      :lifecycle-available="
        !lifecycleBlockLabel(selectedArtifact, trashMode ? 'RESTORE' : 'DELETE')
      "
      :action-unavailable-label="custom.actionUnavailable"
      :loading="contentBusy"
      :preview-text="previewText"
      :unavailable="previewUnavailable"
      :format-bytes="formatBytes"
      :format-date="formatDate"
      :source-label="sourceLabel"
      @close="closePreview"
      @download="download(selectedArtifact)"
      @request-delete="
        openLifecycleDialog(selectedArtifact, trashMode ? 'RESTORE' : 'DELETE');
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
        impact: custom.impact,
        impactBlocked: custom.impactBlocked,
        impactUnavailable: custom.impactUnavailable,
        reason: custom.lifecycle.reason,
        title: custom.lifecycle.title,
      }"
      :state="lifecycleDialog.state"
      @close="lifecycleDialog = undefined"
      @confirm="confirmLifecycleOperation"
    />

    <TrashBulkDialog
      v-if="bulkDialog"
      :action="bulkDialog.action"
      :busy="contentBusy"
      :count="bulkDialog.artifacts.length"
      :labels="{
        cancel: custom.cancel,
        confirm: custom.bulk.confirm,
        confirmationHint: custom.confirmationHint,
        confirmationPhrase: custom.confirmationPhrase,
        description: custom.bulk.description,
        executionHint: custom.sequentialHint,
        title: custom.bulk.title,
      }"
      @close="bulkDialog = undefined"
      @confirm="confirmBulkOperation"
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
  width: 148px;
  min-height: 36px;
  max-width: 168px;
}
.files-workspace__toolbar > label:not(.files-workspace__search) {
  flex: 0 0 auto;
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
  grid-template-columns: minmax(0, 1fr);
}
.files-workspace__layout--details {
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
.upload-queue progress {
  display: block;
  width: 100%;
  height: 4px;
  margin-top: 5px;
  accent-color: var(--accent);
}
.trash-toolbar {
  padding: 10px 14px;
}
.trash-toolbar p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 0.8rem;
}
.trash-toolbar__actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
.trash-toolbar__select-all {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 0.8rem;
}
.bulk-operation-receipts {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.bulk-operation-receipts header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.bulk-operation-receipts ul {
  display: grid;
  gap: 6px;
  padding: 0;
  margin: 8px 0 0;
  list-style: none;
}
.bulk-operation-receipts li {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 3px 12px;
  align-items: center;
  padding: 7px 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
}
.bulk-operation-receipts li strong,
.bulk-operation-receipts li small {
  min-width: 0;
  overflow-wrap: anywhere;
}
.bulk-operation-receipts li small {
  grid-column: 1 / -1;
  color: var(--danger, #b42318);
}
.bulk-operation-receipts__success {
  color: var(--success, #067647);
}
.bulk-operation-receipts__failure {
  color: var(--danger, #b42318);
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
.file-collection-item__actions {
  position: absolute;
  z-index: 1;
  top: 9px;
  right: 9px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.file-collection-item__actions .icon-button {
  border: 1px solid var(--border);
  background: var(--surface);
}
.file-collection-item__select {
  position: absolute;
  z-index: 2;
  top: 12px;
  left: 12px;
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  border-radius: 5px;
  background: var(--surface);
  box-shadow: 0 0 0 1px var(--border);
}
.file-collection-item__select input {
  margin: 0;
}
.file-collection-item--selectable .file-list-row {
  padding-left: 48px;
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
  padding: 8px 152px 8px 14px;
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
.files-list .file-collection-item__actions {
  top: 50%;
  flex-direction: row;
  transform: translateY(-50%);
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
.file-details__bindings,
.file-details__impact {
  padding: 14px 0;
  border-top: 1px solid var(--border);
}
.file-details h3 {
  margin: 0 0 6px;
  font-size: 0.86rem;
}
.file-details__preview p,
.file-details__bindings > p,
.file-details__impact p {
  color: var(--muted);
  font-size: 0.8rem;
  line-height: 1.45;
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
@media (max-width: 1500px) {
  .files-workspace__toolbar {
    flex-wrap: wrap;
  }
  .files-workspace__search {
    flex-basis: 100%;
  }
}
@media (max-width: 980px) {
  .files-workspace__layout--details {
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
    width: auto;
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
    padding: 10px 154px 10px 2px;
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
  .files-list .file-collection-item__actions {
    right: 0;
  }
}
</style>
