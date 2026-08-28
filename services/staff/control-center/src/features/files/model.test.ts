import { describe, expect, it } from "vitest";

import {
  artifactKind,
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

  it("не обещает inline preview для PDF без rendered-preview API", () => {
    expect(supportsInlinePreview(artifact())).toBe(false);
    expect(
      supportsInlinePreview(
        artifact({ fileName: "notes.txt", mediaType: "text/plain" }),
      ),
    ).toBe(true);
  });
});
