import { computed, ref } from "vue";

export const attachmentAggregateLimitBytes = 512 * 1024 * 1024;
export const attachmentUploadConcurrency = 2;

export type AttachmentUploadState =
  | "QUEUED"
  | "UPLOADING"
  | "UPLOADED"
  | "FAILED";

export interface AttachmentUploadQueueItem {
  key: string;
  file: File;
  name: string;
  mediaType: string;
  size: number;
  state: AttachmentUploadState;
  artifactRef?: string;
  error?: string;
  progress?: AttachmentUploadProgress;
}

export interface AttachmentUploadProgress {
  loadedBytes: number;
  totalBytes: number;
}

export interface AttachmentUploadRequest {
  signal: AbortSignal;
  onProgress: (progress: AttachmentUploadProgress) => void;
}

export interface AttachmentComposerState {
  refs: string[];
  count: number;
  uploadedCount: number;
  totalBytes: number;
  busy: boolean;
  hasErrors: boolean;
  overLimit: boolean;
  ready: boolean;
}

export interface ExistingAttachmentSelection {
  ref: string;
  name: string;
  mediaType: string;
  size: number;
}

export interface AttachmentComposerHandle {
  clear: () => void;
}

export interface AttachmentUploadQueueOptions {
  upload: (
    file: File,
    request: AttachmentUploadRequest,
  ) => Promise<{ ref: string }>;
  disabled: () => boolean;
  formatError: (error: unknown) => string;
  reservedBytes?: () => number;
  concurrency?: number;
}

function attachmentKey(file: File): string {
  return [file.name, file.size, file.type, file.lastModified].join(":");
}

export function stageAttachments(
  current: readonly AttachmentUploadQueueItem[],
  files: Iterable<File>,
): AttachmentUploadQueueItem[] {
  const staged = new Map(current.map((item) => [item.key, item]));
  for (const file of files) {
    const key = attachmentKey(file);
    if (staged.has(key)) continue;
    staged.set(key, {
      key,
      file,
      name: file.name,
      mediaType: file.type || "application/octet-stream",
      size: file.size,
      state: "QUEUED",
    });
  }
  return [...staged.values()];
}

export function attachmentQueueState(
  items: readonly AttachmentUploadQueueItem[],
  reservedBytes = 0,
): AttachmentComposerState {
  const totalBytes =
    reservedBytes + items.reduce((total, item) => total + item.size, 0);
  const refs = items.flatMap((item) =>
    item.state === "UPLOADED" && item.artifactRef ? [item.artifactRef] : [],
  );
  const busy = items.some(
    (item) => item.state === "QUEUED" || item.state === "UPLOADING",
  );
  const hasErrors = items.some((item) => item.state === "FAILED");
  const overLimit = totalBytes > attachmentAggregateLimitBytes;
  return {
    refs,
    count: items.length,
    uploadedCount: refs.length,
    totalBytes,
    busy,
    hasErrors,
    overLimit,
    ready: !busy && !hasErrors && !overLimit,
  };
}

export function attachmentComposerState(
  uploadState: AttachmentComposerState,
  existing: readonly ExistingAttachmentSelection[],
): AttachmentComposerState {
  const selected = new Map(existing.map((item) => [item.ref, item]));
  const refs = [
    ...selected.keys(),
    ...uploadState.refs.filter((reference) => !selected.has(reference)),
  ];
  const totalBytes =
    uploadState.totalBytes +
    [...selected.values()].reduce((total, item) => total + item.size, 0);
  const overLimit = totalBytes > attachmentAggregateLimitBytes;
  return {
    refs,
    count: uploadState.count + selected.size,
    uploadedCount: uploadState.uploadedCount + selected.size,
    totalBytes,
    busy: uploadState.busy,
    hasErrors: uploadState.hasErrors,
    overLimit,
    ready: !uploadState.busy && !uploadState.hasErrors && !overLimit,
  };
}

export function formatAttachmentSize(value: number, locale: string): string {
  const units: Array<[Intl.NumberFormatOptions["unit"], number]> = [
    ["gigabyte", 1024 ** 3],
    ["megabyte", 1024 ** 2],
    ["kilobyte", 1024],
  ];
  const [unit, divisor] = units.find(([, threshold]) => value >= threshold) ?? [
    "byte",
    1,
  ];
  return new Intl.NumberFormat(locale, {
    maximumFractionDigits: divisor === 1 ? 0 : 1,
    style: "unit",
    unit,
    unitDisplay: "short",
  }).format(value / divisor);
}

export function createAttachmentUploadQueue(
  options: AttachmentUploadQueueOptions,
) {
  const items = ref<AttachmentUploadQueueItem[]>([]);
  const activeUploads = ref(0);
  const controllers = new Map<string, AbortController>();
  const state = computed(() =>
    attachmentQueueState(items.value, options.reservedBytes?.() ?? 0),
  );
  const concurrency = options.concurrency ?? attachmentUploadConcurrency;

  function enqueue(files: Iterable<File>): void {
    items.value = stageAttachments(items.value, files);
    process();
  }

  function remove(key: string): void {
    controllers.get(key)?.abort();
    items.value = items.value.filter((item) => item.key !== key);
    process();
  }

  function retry(key: string): void {
    const item = items.value.find((candidate) => candidate.key === key);
    if (!item || item.state !== "FAILED") return;
    item.state = "QUEUED";
    item.error = undefined;
    item.progress = undefined;
    process();
  }

  function clear(): void {
    for (const controller of controllers.values()) controller.abort();
    items.value = [];
  }

  async function uploadItem(key: string): Promise<void> {
    const item = items.value.find((candidate) => candidate.key === key);
    if (!item || item.state !== "QUEUED") return;
    const controller = new AbortController();
    controllers.set(key, controller);
    item.state = "UPLOADING";
    item.progress = undefined;
    activeUploads.value += 1;
    try {
      const artifact = await options.upload(item.file, {
        signal: controller.signal,
        onProgress: (progress) => {
          const current = items.value.find(
            (candidate) => candidate.key === key,
          );
          if (
            current !== item ||
            current.state !== "UPLOADING" ||
            !Number.isSafeInteger(progress.loadedBytes) ||
            !Number.isSafeInteger(progress.totalBytes) ||
            progress.loadedBytes < 0 ||
            progress.totalBytes !== current.size ||
            progress.loadedBytes > progress.totalBytes
          )
            return;
          current.progress = {
            loadedBytes: progress.loadedBytes,
            totalBytes: progress.totalBytes,
          };
        },
      });
      const current = items.value.find((candidate) => candidate.key === key);
      if (current !== item) return;
      current.state = "UPLOADED";
      current.artifactRef = artifact.ref;
      current.error = undefined;
      current.progress = undefined;
    } catch (error) {
      const current = items.value.find((candidate) => candidate.key === key);
      if (current !== item) return;
      current.state = "FAILED";
      current.error = options.formatError(error);
      current.progress = undefined;
    } finally {
      if (controllers.get(key) === controller) controllers.delete(key);
      activeUploads.value = Math.max(0, activeUploads.value - 1);
      process();
    }
  }

  function process(): void {
    if (options.disabled() || state.value.overLimit) return;
    while (activeUploads.value < concurrency) {
      const next = items.value.find((item) => item.state === "QUEUED");
      if (!next) return;
      void uploadItem(next.key);
    }
  }

  return { clear, enqueue, items, process, remove, retry, state };
}
