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

export interface AttachmentComposerHandle {
  clear: () => void;
}

export interface AttachmentUploadQueueOptions {
  upload: (file: File) => Promise<{ ref: string }>;
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
  const state = computed(() =>
    attachmentQueueState(items.value, options.reservedBytes?.() ?? 0),
  );
  const concurrency = options.concurrency ?? attachmentUploadConcurrency;

  function enqueue(files: Iterable<File>): void {
    items.value = stageAttachments(items.value, files);
    process();
  }

  function remove(key: string): void {
    items.value = items.value.filter((item) => item.key !== key);
    process();
  }

  function retry(key: string): void {
    const item = items.value.find((candidate) => candidate.key === key);
    if (!item || item.state !== "FAILED") return;
    item.state = "QUEUED";
    item.error = undefined;
    process();
  }

  function clear(): void {
    items.value = [];
  }

  async function uploadItem(key: string): Promise<void> {
    const item = items.value.find((candidate) => candidate.key === key);
    if (!item || item.state !== "QUEUED") return;
    item.state = "UPLOADING";
    activeUploads.value += 1;
    try {
      const artifact = await options.upload(item.file);
      const current = items.value.find((candidate) => candidate.key === key);
      if (!current) return;
      current.state = "UPLOADED";
      current.artifactRef = artifact.ref;
      current.error = undefined;
    } catch (error) {
      const current = items.value.find((candidate) => candidate.key === key);
      if (!current) return;
      current.state = "FAILED";
      current.error = options.formatError(error);
    } finally {
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
