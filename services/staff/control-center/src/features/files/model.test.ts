import { describe, expect, it } from "vitest";

import {
  artifactLifecycleState,
  artifactKind,
  createUploadQueueItems,
  fileVisual,
  matchesArtifactFilters,
  supportsInlinePreview,
} from "@/features/files/model";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

function artifact(options: Partial<Artifact> = {}): Artifact {
  return {
    ref: "artifact_file",
    version: 1,
    projectRef: "project_sales",
    fileName: "report.pdf",
    mediaType: "application/pdf",
    sizeBytes: 2048,
    digest: "sha256:test",
    scanState: "CLEAN",
    source: "CONTROL_CENTER",
    revision: 2,
    lifecycleState: "ACTIVE",
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-08-28T09:00:00Z",
    nextActions: ["DOWNLOAD", "BIND"],
    ...options,
  };
}

describe("files model", () => {
  it("выбирает различимые иконки по media type и расширению", () => {
    expect(fileVisual(artifact()).icon).toBe("pdf");
    expect(
      fileVisual(
        artifact({
          fileName: "data.xlsx",
          mediaType:
            "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        }),
      ).icon,
    ).toBe("spreadsheet");
    expect(
      fileVisual(artifact({ fileName: "photo.webp", mediaType: "image/webp" }))
        .icon,
    ).toBe("image");
    expect(
      fileVisual(
        artifact({ fileName: "bundle.zip", mediaType: "application/zip" }),
      ).icon,
    ).toBe("archive");
  });

  it("фильтрует вкладки, источник и состояние без мутации artifact", () => {
    const value = artifact({
      source: "AGENT_RESULT",
      agentBindings: ["agent_sales"],
    });
    expect(
      matchesArtifactFilters(value, {
        kind: "DOCUMENT",
        scanState: "CLEAN",
        source: "AGENT_RESULT",
        tab: "RESULTS",
      }),
    ).toBe(true);
    expect(artifactKind(value)).toBe("DOCUMENT");
    expect(
      matchesArtifactFilters(value, {
        kind: "ALL",
        scanState: "QUARANTINED",
        source: "ALL",
        tab: "FILES",
      }),
    ).toBe(false);
  });

  it("отделяет удалённые artifacts от рабочих разделов", () => {
    const deleted = artifact({
      lifecycleState: "DELETED",
      nextActions: ["RESTORE"],
    });

    expect(
      matchesArtifactFilters(deleted, {
        kind: "ALL",
        scanState: "ALL",
        source: "ALL",
        tab: "FILES",
      }),
    ).toBe(false);
    expect(
      matchesArtifactFilters(deleted, {
        kind: "ALL",
        scanState: "ALL",
        source: "ALL",
        tab: "TRASH",
      }),
    ).toBe(true);
  });

  it("не обещает lifecycle mutation без generated команды", () => {
    expect(artifactLifecycleState(artifact(), "DELETE")).toEqual({
      action: "DELETE",
      available: false,
      reason: "ACTION_NOT_ALLOWED",
    });
    expect(
      artifactLifecycleState(artifact({ nextActions: ["RESTORE"] }), "RESTORE"),
    ).toEqual({
      action: "RESTORE",
      available: true,
    });
    expect(
      artifactLifecycleState(
        artifact({ nextActions: ["DELETE" as never] }),
        "DELETE",
      ),
    ).toEqual({
      action: "DELETE",
      available: false,
      reason: "CONTRACT_UNAVAILABLE",
    });
  });

  it("создаёт неограниченную по количеству очередь без потери порядка", () => {
    let sequence = 0;
    const files = [
      new File(["one"], "one.txt", { type: "text/plain" }),
      new File(["two"], "two.txt", { type: "text/plain" }),
      new File(["three"], "three.txt", { type: "text/plain" }),
    ];

    expect(
      createUploadQueueItems(
        files,
        () => `item-${String((sequence += 1))}`,
      ).map((item) => ({
        id: item.id,
        name: item.file.name,
        state: item.state,
      })),
    ).toEqual([
      { id: "item-1", name: "one.txt", state: "QUEUED" },
      { id: "item-2", name: "two.txt", state: "QUEUED" },
      { id: "item-3", name: "three.txt", state: "QUEUED" },
    ]);
  });

  it("не обещает inline preview для PDF без rendered-preview API", () => {
    expect(supportsInlinePreview(artifact())).toBe(false);
    expect(
      supportsInlinePreview(
        artifact({ fileName: "notes.txt", mediaType: "text/plain" }),
      ),
    ).toBe(true);
  });
});
