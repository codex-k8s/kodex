import { describe, expect, it } from "vitest";

import {
  artifactLifecycleState,
  artifactLifecycleAnnounced,
  artifactKind,
  artifactSourceKinds,
  createUploadQueueItems,
  fileVisual,
  matchesArtifactFilters,
  nextUploadQueueItems,
  supportsInlinePreview,
  trashBulkConfirmed,
  uploadProgressPercent,
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

  it("строит разделы только из существующего provenance", () => {
    expect(artifactSourceKinds("FILES", "ALL")).toEqual([
      "CONTROL_CENTER",
      "INTERACTION_ATTACHMENT",
    ]);
    expect(artifactSourceKinds("KNOWLEDGE", "ALL")).toEqual([
      "KNOWLEDGE_SOURCE",
    ]);
    expect(artifactSourceKinds("RESULTS", "ALL")).toEqual([
      "AGENT_RESULT",
      "INTEGRATION_RESULT",
    ]);
    expect(artifactSourceKinds("RESULTS", "CONTROL_CENTER")).toEqual([]);
    expect(
      matchesArtifactFilters(artifact({ source: "KNOWLEDGE_SOURCE" }), {
        kind: "ALL",
        scanState: "ALL",
        source: "ALL",
        tab: "KNOWLEDGE",
      }),
    ).toBe(true);
  });

  it("разрешает только объявленные сервером lifecycle mutations", () => {
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
      reason: "IMPACT_UNAVAILABLE",
    });
    expect(
      artifactLifecycleState(
        artifact({ nextActions: ["DELETE" as never] }),
        "DELETE",
        {
          action: "DELETE",
          activeRuns: [],
          activeRunsTruncated: false,
          activeRuntimeCount: 0,
          artifactRef: "artifact_file",
          artifactVersion: 1,
          attachmentCount: 0,
          bindingCount: 0,
          blockers: [],
          impactDigest: "a".repeat(64),
          permitted: true,
        },
      ),
    ).toMatchObject({
      action: "DELETE",
      available: true,
    });
    expect(
      artifactLifecycleState(
        artifact({ nextActions: ["DELETE" as never] }),
        "DELETE",
        {
          action: "DELETE",
          activeRuns: [
            {
              projectRef: "project_sales",
              runRef: "run_active",
              state: "RUNNING",
              title: "Активный запуск",
            },
          ],
          activeRunsTruncated: false,
          activeRuntimeCount: 1,
          artifactRef: "artifact_file",
          artifactVersion: 1,
          attachmentCount: 1,
          bindingCount: 1,
          blockers: ["ACTIVE_RUN_USES_ARTIFACT"],
          impactDigest: "b".repeat(64),
          permitted: false,
        },
      ),
    ).toMatchObject({
      action: "DELETE",
      available: false,
      reason: "IMPACT_BLOCKED",
    });
    expect(
      artifactLifecycleAnnounced(
        artifact({ nextActions: ["DELETE" as never] }),
        "DELETE",
      ),
    ).toBe(true);
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

  it("ограничивает число одновременных upload и проверяет progress", () => {
    let sequence = 0;
    const queued = createUploadQueueItems(
      [
        new File(["one"], "one.txt"),
        new File(["two"], "two.txt"),
        new File(["three"], "three.txt"),
        new File(["four"], "four.txt"),
      ],
      () => `upload-${String((sequence += 1))}`,
    );
    const first = queued.shift();
    if (!first) throw new Error("Upload queue fixture is empty");
    const activeQueue = [{ ...first, state: "UPLOADING" as const }, ...queued];

    expect(nextUploadQueueItems(activeQueue, 3).map((item) => item.id)).toEqual(
      ["upload-2", "upload-3"],
    );
    expect(uploadProgressPercent({ loadedBytes: 3, totalBytes: 4 })).toBe(75);
    expect(
      uploadProgressPercent({ loadedBytes: 5, totalBytes: 4 }),
    ).toBeUndefined();
  });

  it("не обещает inline preview для PDF без rendered-preview API", () => {
    expect(supportsInlinePreview(artifact())).toBe(false);
    expect(
      supportsInlinePreview(
        artifact({ fileName: "notes.txt", mediaType: "text/plain" }),
      ),
    ).toBe(true);
  });

  it("требует точную фразу для необратимых массовых операций", () => {
    expect(trashBulkConfirmed("DELETE", "", "УДАЛИТЬ НАВСЕГДА")).toBe(true);
    expect(trashBulkConfirmed("RESTORE", "", "УДАЛИТЬ НАВСЕГДА")).toBe(true);
    expect(
      trashBulkConfirmed("PURGE", " удалить навсегда ", "УДАЛИТЬ НАВСЕГДА"),
    ).toBe(false);
    expect(
      trashBulkConfirmed("EMPTY", " УДАЛИТЬ НАВСЕГДА ", "УДАЛИТЬ НАВСЕГДА"),
    ).toBe(true);
  });
});
