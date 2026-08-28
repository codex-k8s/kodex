import { describe, expect, it, vi } from "vitest";

import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

const listArtifactsMock = vi.hoisted(() => vi.fn());
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listArtifacts: listArtifactsMock,
}));

import { loadArtifactPage } from "@/features/files/api";

function artifact(ref: string): Artifact {
  return {
    ref,
    version: 1,
    projectRef: "project_sales",
    fileName: `${ref}.pdf`,
    mediaType: "application/pdf",
    sizeBytes: 1024,
    digest: "sha256:test",
    scanState: "CLEAN",
    source: "CONTROL_CENTER",
    revision: 1,
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-08-28T09:00:00Z",
    nextActions: ["DOWNLOAD"],
  };
}

describe("loadArtifactPage", () => {
  it("передаёт серверный поиск и cursor, затем сохраняет следующий cursor", async () => {
    listArtifactsMock.mockResolvedValue({
      data: {
        items: [artifact("artifact_one")],
        nextPageToken: "cursor-next",
      },
      response: new Response(null, { status: 200 }),
    });
    const controller = new AbortController();

    const page = await loadArtifactPage("project_sales", {
      query: "  договор  ",
      cursor: "cursor-current",
      signal: controller.signal,
    });

    expect(listArtifactsMock).toHaveBeenCalledWith({
      path: { projectRef: "project_sales" },
      query: {
        pageSize: 40,
        pageToken: "cursor-current",
        query: "договор",
      },
      signal: controller.signal,
    });
    expect(page.items[0]).toMatchObject({
      id: "artifact_one",
      label: "artifact_one.pdf",
    });
    expect(page.nextCursor).toBe("cursor-next");
  });
});
