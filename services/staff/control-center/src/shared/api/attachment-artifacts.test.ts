import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

const listArtifactsMock = vi.hoisted(() => vi.fn());
const listOrganizationArtifactsMock = vi.hoisted(() => vi.fn());
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listArtifacts: listArtifactsMock,
  listOrganizationArtifacts: listOrganizationArtifactsMock,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

import { createAttachmentArtifactLoader } from "@/shared/api/attachment-artifacts";

function response(items: Artifact[], nextPageToken?: string) {
  return Promise.resolve({
    data: { items, nextPageToken },
    response: new Response(null, { status: 200 }),
  });
}

function artifact(overrides: Partial<Artifact> = {}): Artifact {
  return {
    ref: "artifact_1",
    version: 1,
    projectRef: "project_1",
    fileName: "brief.pdf",
    mediaType: "application/pdf",
    sizeBytes: 1024,
    digest: "sha256:test",
    scanState: "CLEAN",
    lifecycleState: "ACTIVE",
    source: "CONTROL_CENTER",
    revision: 2,
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-08-29T10:00:00Z",
    nextActions: ["DOWNLOAD"],
    ...overrides,
  };
}

describe("createAttachmentArtifactLoader", () => {
  beforeEach(() => {
    listArtifactsMock.mockReset();
    listOrganizationArtifactsMock.mockReset();
  });

  it("передаёт Project, серверный поиск и cursor без storage credentials", async () => {
    listArtifactsMock.mockReturnValueOnce(
      response([artifact()], "artifact-page-3"),
    );
    const loader = createAttachmentArtifactLoader("project_1");
    const signal = new AbortController().signal;

    const page = await loader({
      cursor: "artifact-page-2",
      query: "  договор  ",
      signal,
    });

    expect(listArtifactsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_1" },
        query: {
          lifecycleState: "ACTIVE",
          pageSize: 40,
          pageToken: "artifact-page-2",
          query: "договор",
        },
      }),
    );
    expect(page).toMatchObject({
      items: [
        {
          id: "artifact_1",
          label: "brief.pdf",
          artifact: { ref: "artifact_1", scanState: "CLEAN" },
        },
      ],
      nextCursor: "artifact-page-3",
    });
    expect(JSON.stringify(page)).not.toContain("credential");
  });

  it("использует organization-scoped список для глобального assistant", async () => {
    const organizationArtifact = artifact({ ref: "artifact_organization" });
    delete organizationArtifact.projectRef;
    listOrganizationArtifactsMock.mockReturnValueOnce(
      response([organizationArtifact]),
    );
    const loader = createAttachmentArtifactLoader();

    const page = await loader({
      query: "  context  ",
      signal: new AbortController().signal,
    });

    expect(listOrganizationArtifactsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        query: {
          lifecycleState: "ACTIVE",
          pageSize: 40,
          query: "context",
        },
      }),
    );
    expect(listArtifactsMock).not.toHaveBeenCalled();
    expect(page.items.map((item) => item.id)).toEqual([
      "artifact_organization",
    ]);
  });

  it("пропускает страницу без CLEAN файлов и продолжает cursor-pagination", async () => {
    listArtifactsMock
      .mockReturnValueOnce(
        response(
          [artifact({ ref: "pending", scanState: "SCANNING" })],
          "artifact-page-2",
        ),
      )
      .mockReturnValueOnce(
        response([artifact({ ref: "clean", fileName: "clean.txt" })]),
      );
    const loader = createAttachmentArtifactLoader("project_1");

    const page = await loader({
      query: "",
      signal: new AbortController().signal,
    });

    expect(listArtifactsMock).toHaveBeenCalledTimes(2);
    const lastCall = listArtifactsMock.mock.lastCall?.[0] as
      | { query?: { pageToken?: string } }
      | undefined;
    expect(lastCall?.query?.pageToken).toBe("artifact-page-2");
    expect(page.items.map((item) => item.id)).toEqual(["clean"]);
    expect(page.nextCursor).toBeNull();
  });
});
