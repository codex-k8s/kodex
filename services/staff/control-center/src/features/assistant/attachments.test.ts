import { describe, expect, it, vi } from "vitest";

import { waitForCleanArtifact } from "@/features/assistant/attachments";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

function artifact(
  scanState: Artifact["scanState"],
  lifecycleState: Artifact["lifecycleState"] = "ACTIVE",
): Artifact {
  return {
    ref: "art_contract",
    version: 1,
    projectRef: "prj_sales",
    fileName: "contract.pdf",
    mediaType: "application/pdf",
    sizeBytes: 42,
    digest: "sha256:test",
    scanState,
    source: "INTERACTION_ATTACHMENT",
    revision: 1,
    lifecycleState,
    agentBindings: [],
    previewAvailable: false,
    createdAt: "2026-08-30T00:00:00Z",
    nextActions: [],
  };
}

describe("assistant attachment scan gate", () => {
  it("разблокирует вложение только после авторитетного CLEAN", async () => {
    vi.useFakeTimers();
    const read = vi.fn().mockResolvedValue(artifact("CLEAN"));
    const promise = waitForCleanArtifact(artifact("SCANNING"), {
      signal: new AbortController().signal,
      read,
      intervalMs: 10,
      maxAttempts: 2,
    });
    const result = expect(promise).resolves.toMatchObject({
      scanState: "CLEAN",
    });

    await vi.advanceTimersByTimeAsync(10);

    await result;
    expect(read).toHaveBeenCalledWith("art_contract");
    vi.useRealTimers();
  });

  it("закрыто отклоняет quarantined файл", async () => {
    await expect(
      waitForCleanArtifact(artifact("QUARANTINED"), {
        signal: new AbortController().signal,
        read: vi.fn(),
      }),
    ).rejects.toThrow("Attachment is not safe to use: QUARANTINED");
  });

  it("отменяет ожидание без позднего read и unhandled rejection", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const read = vi.fn();
    const promise = waitForCleanArtifact(artifact("SCANNING"), {
      signal: controller.signal,
      read,
      intervalMs: 10,
    });
    const result = expect(promise).rejects.toMatchObject({
      name: "AbortError",
    });

    controller.abort();
    await result;
    await vi.runAllTimersAsync();

    expect(read).not.toHaveBeenCalled();
    vi.useRealTimers();
  });
});
