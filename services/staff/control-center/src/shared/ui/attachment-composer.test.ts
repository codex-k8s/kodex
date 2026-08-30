import { nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";

import {
  attachmentAggregateLimitBytes,
  attachmentComposerState,
  attachmentQueueState,
  createAttachmentUploadQueue,
  stageAttachments,
  type AttachmentUploadRequest,
  type AttachmentUploadQueueItem,
} from "@/shared/ui/attachment-composer";

function file(
  name: string,
  size: number,
  type = "text/plain",
  lastModified = 1,
): File {
  return { name, size, type, lastModified } as File;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Expected test fixture value");
  return value;
}

async function flushQueue(): Promise<void> {
  await Promise.resolve();
  await nextTick();
}

describe("attachment composer model", () => {
  it("объединяет загруженные и выбранные Project-файлы без count-limit", () => {
    const uploaded = attachmentQueueState([
      {
        ...required(stageAttachments([], [file("upload.txt", 5)])[0]),
        artifactRef: "artifact_upload",
        state: "UPLOADED",
      },
    ]);
    const existing = Array.from({ length: 300 }, (_, index) => ({
      ref: `artifact_${String(index)}`,
      name: `existing-${String(index)}.txt`,
      mediaType: "text/plain",
      size: 1,
    }));

    const state = attachmentComposerState(uploaded, existing);

    expect(state.count).toBe(301);
    expect(state.uploadedCount).toBe(301);
    expect(state.refs).toHaveLength(301);
    expect(state.ready).toBe(true);
  });

  it("detach сохранённого файла меняет только draft state", () => {
    const base = attachmentQueueState([]);
    const artifact = {
      ref: "artifact_existing",
      name: "existing.txt",
      mediaType: "text/plain",
      size: 10,
    };

    expect(attachmentComposerState(base, [artifact]).refs).toEqual([
      "artifact_existing",
    ]);
    expect(attachmentComposerState(base, []).refs).toEqual([]);
  });

  it("добавляет произвольное число файлов без count-limit и дедуплицирует browser descriptor", () => {
    const files = Array.from({ length: 256 }, (_, index) =>
      file(`input-${String(index)}.txt`, index + 1, "text/plain", index),
    );

    const staged = stageAttachments([], [...files, files[0] as File]);

    expect(staged).toHaveLength(256);
  });

  it("блокирует очередь только по aggregate 512 MiB", () => {
    const staged = stageAttachments(
      [],
      [
        file("part-a.bin", attachmentAggregateLimitBytes),
        file("part-b.bin", 1),
      ],
    );

    expect(attachmentQueueState(staged)).toMatchObject({
      count: 2,
      overLimit: true,
      ready: false,
    });
  });

  it("учитывает размер уже выбранных сохранённых файлов", () => {
    const staged = stageAttachments([], [file("new.bin", 2)]);

    expect(
      attachmentQueueState(staged, attachmentAggregateLimitBytes - 1),
    ).toMatchObject({ overLimit: true, ready: false });
  });

  it("сохраняет порядок готовых refs и отличает ошибку от загрузки", () => {
    const uploaded = required(stageAttachments([], [file("a.txt", 1)])[0]);
    const failed = required(
      stageAttachments([], [file("b.txt", 2, "text/plain", 2)])[0],
    );
    const items: AttachmentUploadQueueItem[] = [
      {
        ...uploaded,
        state: "UPLOADED",
        artifactRef: "art_a",
      },
      {
        ...failed,
        state: "FAILED",
        error: "upload failed",
      },
    ];

    expect(attachmentQueueState(items)).toMatchObject({
      refs: ["art_a"],
      uploadedCount: 1,
      busy: false,
      hasErrors: true,
      ready: false,
    });
  });
});

describe("attachment upload queue", () => {
  it("загружает не более двух файлов одновременно и запускает следующий после завершения", async () => {
    const firstUpload = deferred<{ ref: string }>();
    const secondUpload = deferred<{ ref: string }>();
    const upload = vi
      .fn<
        (
          source: File,
          request: AttachmentUploadRequest,
        ) => Promise<{ ref: string }>
      >()
      .mockReturnValueOnce(firstUpload.promise)
      .mockReturnValueOnce(secondUpload.promise)
      .mockResolvedValueOnce({ ref: "art_c" });
    const queue = createAttachmentUploadQueue({
      upload,
      disabled: () => false,
      formatError: () => "upload failed",
    });

    queue.enqueue([
      file("a.txt", 1),
      file("b.txt", 2, "text/plain", 2),
      file("c.txt", 3, "text/plain", 3),
    ]);
    expect(upload).toHaveBeenCalledTimes(2);

    firstUpload.resolve({ ref: "art_a" });
    await flushQueue();
    expect(upload).toHaveBeenCalledTimes(3);

    secondUpload.resolve({ ref: "art_b" });
    await flushQueue();
    await flushQueue();
    expect(queue.state.value.refs).toEqual(["art_a", "art_b", "art_c"]);
    expect(queue.state.value.ready).toBe(true);
  });

  it("показывает ошибку, позволяет retry и не включает удалённый ref в command", async () => {
    const upload = vi
      .fn<
        (
          source: File,
          request: AttachmentUploadRequest,
        ) => Promise<{ ref: string }>
      >()
      .mockRejectedValueOnce(new Error("temporary"))
      .mockResolvedValueOnce({ ref: "art_retry" });
    const queue = createAttachmentUploadQueue({
      upload,
      disabled: () => false,
      formatError: () => "Безопасная ошибка загрузки",
    });

    queue.enqueue([file("retry.txt", 10)]);
    await flushQueue();
    expect(queue.items.value[0]).toMatchObject({
      state: "FAILED",
      error: "Безопасная ошибка загрузки",
    });

    const item = required(queue.items.value[0]);
    queue.retry(item.key);
    await flushQueue();
    expect(queue.state.value.refs).toEqual(["art_retry"]);

    queue.remove(required(queue.items.value[0]).key);
    expect(queue.state.value.refs).toEqual([]);
    expect(queue.state.value.ready).toBe(true);
  });

  it("не начинает upload, пока aggregate превышает лимит", () => {
    const upload =
      vi.fn<
        (
          source: File,
          request: AttachmentUploadRequest,
        ) => Promise<{ ref: string }>
      >();
    const queue = createAttachmentUploadQueue({
      upload,
      disabled: () => false,
      formatError: () => "upload failed",
    });

    queue.enqueue([file("too-large.bin", attachmentAggregateLimitBytes + 1)]);

    expect(upload).not.toHaveBeenCalled();
    expect(queue.state.value.overLimit).toBe(true);
  });

  it("прерывает активный upload при remove и игнорирует его позднее завершение", async () => {
    const upload = deferred<{ ref: string }>();
    let request: AttachmentUploadRequest | undefined;
    const queue = createAttachmentUploadQueue({
      upload: (_file, current) => {
        request = current;
        return upload.promise;
      },
      disabled: () => false,
      formatError: () => "upload failed",
    });

    queue.enqueue([file("cancel.txt", 10)]);
    const item = required(queue.items.value[0]);
    expect(request?.signal.aborted).toBe(false);

    queue.remove(item.key);
    expect(request?.signal.aborted).toBe(true);
    expect(queue.items.value).toEqual([]);

    upload.resolve({ ref: "artifact_hidden" });
    await flushQueue();
    expect(queue.state.value.refs).toEqual([]);
    expect(queue.state.value.ready).toBe(true);
  });

  it("показывает determinate progress только после реального byte callback", async () => {
    const upload = deferred<{ ref: string }>();
    let request: AttachmentUploadRequest | undefined;
    const queue = createAttachmentUploadQueue({
      upload: (_file, current) => {
        request = current;
        return upload.promise;
      },
      disabled: () => false,
      formatError: () => "upload failed",
    });

    queue.enqueue([file("progress.txt", 10)]);
    expect(queue.items.value[0]?.progress).toBeUndefined();

    request?.onProgress({ loadedBytes: 4, totalBytes: 10 });
    expect(queue.items.value[0]?.progress).toEqual({
      loadedBytes: 4,
      totalBytes: 10,
    });

    upload.resolve({ ref: "artifact_progress" });
    await flushQueue();
    expect(queue.items.value[0]?.progress).toBeUndefined();
  });

  it("не применяет поздний abort старой попытки к повторно добавленному файлу", async () => {
    const firstUpload = deferred<{ ref: string }>();
    const secondUpload = deferred<{ ref: string }>();
    const upload = vi
      .fn<
        (
          source: File,
          request: AttachmentUploadRequest,
        ) => Promise<{ ref: string }>
      >()
      .mockReturnValueOnce(firstUpload.promise)
      .mockReturnValueOnce(secondUpload.promise);
    const queue = createAttachmentUploadQueue({
      upload,
      disabled: () => false,
      formatError: () => "upload failed",
    });
    const source = file("same.txt", 10);

    queue.enqueue([source]);
    queue.remove(required(queue.items.value[0]).key);
    queue.enqueue([source]);
    firstUpload.reject(new DOMException("Aborted", "AbortError"));
    await flushQueue();

    expect(queue.items.value[0]).toMatchObject({ state: "UPLOADING" });
    secondUpload.resolve({ ref: "artifact_replacement" });
    await flushQueue();
    expect(queue.state.value.refs).toEqual(["artifact_replacement"]);
  });
});
