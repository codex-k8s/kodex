import { describe, expect, it, vi } from "vitest";

import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

const listArtifactsMock = vi.hoisted(() => vi.fn());
const deleteArtifactMock = vi.hoisted(() => vi.fn());
const purgeArtifactMock = vi.hoisted(() => vi.fn());
const restoreArtifactMock = vi.hoisted(() => vi.fn());
const mutateMock = vi.hoisted(() =>
  vi.fn(
    async (
      request: (headers: Record<string, string>) => Promise<{ data: unknown }>,
      version?: number,
    ) => ({
      data: (
        await request({
          "Idempotency-Key": "idempotency-key",
          "If-Match": `"${String(version ?? 1)}"`,
          "X-CSRF-Token": "csrf-token",
        })
      ).data,
    }),
  ),
);
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  deleteArtifact: deleteArtifactMock,
  listArtifacts: listArtifactsMock,
  purgeArtifact: purgeArtifactMock,
  restoreArtifact: restoreArtifactMock,
}));
vi.mock("@/shared/api/client", () => ({ requestSignal: () => undefined }));
vi.mock("@/shared/api/mutation", () => ({ mutate: mutateMock }));

import {
  deleteArtifactItem,
  loadArtifactPage,
  mutateArtifactsSequentially,
  purgeArtifactItem,
  restoreArtifactItem,
} from "@/features/files/api";

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
    lifecycleState: "ACTIVE",
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
        lifecycleState: "ACTIVE",
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

  it("выполняет lifecycle-команды с OCC и mutation headers", async () => {
    const active = artifact("artifact_one");
    const deleted = {
      ...active,
      lifecycleState: "DELETED" as const,
      nextActions: ["RESTORE"] as Artifact["nextActions"],
      version: 2,
    };
    deleteArtifactMock.mockResolvedValue({ data: deleted });
    restoreArtifactMock.mockResolvedValue({ data: active });
    purgeArtifactMock.mockResolvedValue({
      data: { artifactRef: active.ref, lifecycleState: "PURGED" },
    });

    await expect(deleteArtifactItem(active)).resolves.toEqual(deleted);
    await expect(restoreArtifactItem(deleted)).resolves.toEqual(active);
    await expect(purgeArtifactItem(deleted)).resolves.toEqual({
      artifactRef: active.ref,
      lifecycleState: "PURGED",
    });

    expect(deleteArtifactMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { artifactRef: active.ref },
        headers: {
          "Idempotency-Key": "idempotency-key",
          "If-Match": '"1"',
          "X-CSRF-Token": "csrf-token",
        },
      }),
    );
    expect(restoreArtifactMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { artifactRef: active.ref },
        headers: {
          "Idempotency-Key": "idempotency-key",
          "If-Match": '"2"',
          "X-CSRF-Token": "csrf-token",
        },
      }),
    );
    expect(purgeArtifactMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { artifactRef: active.ref },
        headers: {
          "Idempotency-Key": "idempotency-key",
          "If-Match": '"2"',
          "X-CSRF-Token": "csrf-token",
        },
      }),
    );
  });

  it("последовательно обрабатывает каждый файл и возвращает отдельный receipt", async () => {
    const first = artifact("artifact_one");
    const second = artifact("artifact_two");
    const third = artifact("artifact_three");
    const command = vi.fn((current: Artifact) =>
      current.ref === second.ref
        ? Promise.reject(new Error("second failed"))
        : Promise.resolve(),
    );

    const receipts = await mutateArtifactsSequentially(
      [first, second, third],
      command,
    );

    expect(command.mock.calls.map(([current]) => current.ref)).toEqual([
      first.ref,
      second.ref,
      third.ref,
    ]);
    expect(receipts).toMatchObject([
      { artifact: first, status: "SUCCEEDED" },
      {
        artifact: second,
        problem: { code: "UNKNOWN", status: 0 },
        status: "FAILED",
      },
      { artifact: third, status: "SUCCEEDED" },
    ]);
  });
});
