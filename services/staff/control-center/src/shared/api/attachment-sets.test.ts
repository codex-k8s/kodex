import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  Artifact,
  AttachmentSet,
} from "@/shared/api/generated/openapi/types.gen";

interface OrganizationUploadOptions {
  body: Blob | File;
  headers: Record<string, string>;
  signal?: AbortSignal;
}

interface AttachmentMutationOptions {
  headers: Record<string, string>;
}

const createProjectDraftMock = vi.hoisted(() => vi.fn());
const createOrganizationDraftMock = vi.hoisted(() => vi.fn());
const uploadProjectArtifactMock = vi.hoisted(() => vi.fn());
const uploadOrganizationArtifactMock = vi.hoisted(() =>
  vi.fn<(options: OrganizationUploadOptions) => Promise<{ data: Artifact }>>(),
);
const mutationMock = vi.hoisted(() => vi.fn());
const retryingMutationMock = vi.hoisted(() => vi.fn());
const addItemsMock = vi.hoisted(() =>
  vi.fn<
    (options: AttachmentMutationOptions) => Promise<{ data: AttachmentSet }>
  >(),
);
const removeItemsMock = vi.hoisted(() => vi.fn());
const finalizeDraftMock = vi.hoisted(() => vi.fn());

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  addAttachmentSetItems: addItemsMock,
  createAttachmentSetDraft: createProjectDraftMock,
  createOrganizationAttachmentSetDraft: createOrganizationDraftMock,
  finalizeAttachmentSet: finalizeDraftMock,
  getArtifact: vi.fn(),
  getAttachmentSet: vi.fn(),
  removeAttachmentSetItems: removeItemsMock,
  uploadArtifact: uploadProjectArtifactMock,
  uploadOrganizationArtifact: uploadOrganizationArtifactMock,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: mutationMock,
  mutateWithRetry: retryingMutationMock,
}));

async function invokeMutation(
  request: (headers: Record<string, string>) => Promise<{ data: unknown }>,
  _version?: number,
  idempotencyKey = "idem_test",
): Promise<{ data: unknown }> {
  return request({
    "Idempotency-Key": idempotencyKey,
    "X-CSRF-Token": "csrf_test",
    "If-Match": '"1"',
  });
}

import {
  AttachmentTerminalScanError,
  addAttachmentItems,
  createAttachmentDraft,
  finalizeAttachmentDraft,
  removeAttachmentItems,
  uploadCleanAttachmentArtifact,
  waitForCleanAttachmentArtifact,
} from "@/shared/api/attachment-sets";

function attachmentSet(purpose: AttachmentSet["purpose"]): AttachmentSet {
  return {
    ref: "aset_revision_1",
    familyRef: "aset_family_1",
    revision: 1,
    version: 1,
    state: "DRAFT",
    purpose,
    source: "CONTROL_CENTER",
    itemCount: 0,
    totalSizeBytes: 0,
    items: [],
    createdAt: "2026-08-30T07:00:00Z",
    superseded: false,
  };
}

function artifact(): Artifact {
  return {
    ref: "artifact_organization",
    version: 1,
    fileName: "context.txt",
    mediaType: "text/plain",
    sizeBytes: 7,
    digest: "sha256:test",
    scanState: "CLEAN",
    lifecycleState: "ACTIVE",
    source: "CONTROL_CENTER",
    revision: 1,
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-08-30T07:00:00Z",
    nextActions: ["DOWNLOAD"],
  };
}

describe("AttachmentSet endpoint routing", () => {
  beforeEach(() => {
    createProjectDraftMock.mockReset();
    createOrganizationDraftMock.mockReset();
    uploadProjectArtifactMock.mockReset();
    uploadOrganizationArtifactMock.mockReset();
    mutationMock.mockReset().mockImplementation(invokeMutation);
    retryingMutationMock.mockReset().mockImplementation(invokeMutation);
    addItemsMock.mockReset();
    removeItemsMock.mockReset();
    finalizeDraftMock.mockReset();
  });

  it("создаёт organization draft только для глобального assistant", async () => {
    createOrganizationDraftMock.mockResolvedValue({
      data: attachmentSet("ASSISTANT_MESSAGE"),
    });

    await expect(
      createAttachmentDraft(undefined, "ASSISTANT_MESSAGE"),
    ).resolves.toMatchObject({ purpose: "ASSISTANT_MESSAGE" });

    expect(createOrganizationDraftMock).toHaveBeenCalledWith(
      expect.objectContaining({
        body: { purpose: "ASSISTANT_MESSAGE" },
      }),
    );
    expect(createProjectDraftMock).not.toHaveBeenCalled();
  });

  it("сохраняет project endpoint и закрыто отклоняет другой purpose без Project", async () => {
    createProjectDraftMock.mockResolvedValue({
      data: attachmentSet("RUN_INPUT"),
    });

    await createAttachmentDraft("project_1", "RUN_INPUT");

    expect(createProjectDraftMock).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_1" },
        body: { purpose: "RUN_INPUT" },
      }),
    );
    await expect(createAttachmentDraft(undefined, "RUN_INPUT")).rejects.toThrow(
      "AttachmentSet purpose requires a Project context",
    );
    expect(createOrganizationDraftMock).not.toHaveBeenCalled();
    expect(retryingMutationMock).toHaveBeenCalledTimes(1);
    expect(mutationMock).not.toHaveBeenCalled();
  });

  it("загружает файл глобального assistant через organization endpoint", async () => {
    uploadOrganizationArtifactMock.mockResolvedValue({ data: artifact() });
    const file = new File(["context"], "context.txt", {
      type: "text/plain",
    });
    const onProgress = vi.fn();
    const onScanning = vi.fn();

    await expect(
      uploadCleanAttachmentArtifact(undefined, "ASSISTANT_MESSAGE", file, {
        idempotencyKey: "stable-upload-key",
        signal: new AbortController().signal,
        onProgress,
        onScanning,
      }),
    ).resolves.toMatchObject({ ref: "artifact_organization" });

    const uploadOptions = uploadOrganizationArtifactMock.mock.calls[0]?.[0];
    expect(uploadOptions?.body).toBe(file);
    expect(uploadOptions?.headers["Idempotency-Key"]).toBe("stable-upload-key");
    expect(uploadOptions?.headers["X-File-Name"]).toBe("context.txt");
    expect(uploadProjectArtifactMock).not.toHaveBeenCalled();
    expect(mutationMock).toHaveBeenCalledTimes(1);
    expect(retryingMutationMock).not.toHaveBeenCalled();
    expect(onProgress).toHaveBeenCalledWith({
      loadedBytes: 7,
      totalBytes: 7,
    });
    expect(onScanning).not.toHaveBeenCalled();
  });

  it("выполняет create/add/remove/finalize через bounded retry primitive", async () => {
    const draft = attachmentSet("RUN_INPUT");
    createProjectDraftMock.mockResolvedValue({ data: draft });
    addItemsMock.mockResolvedValue({ data: { ...draft, version: 2 } });
    removeItemsMock.mockResolvedValue({ data: { ...draft, version: 2 } });
    finalizeDraftMock.mockResolvedValue({
      data: { ...draft, version: 2, state: "FINALIZED" },
    });

    await createAttachmentDraft("project_1", "RUN_INPUT");
    await addAttachmentItems(draft, ["artifact_1"], 0);
    await removeAttachmentItems(draft, ["artifact_1"]);
    await finalizeAttachmentDraft(draft);

    expect(retryingMutationMock).toHaveBeenCalledTimes(4);
    expect(mutationMock).not.toHaveBeenCalled();
    const addOptions = addItemsMock.mock.calls[0]?.[0];
    expect(addOptions?.headers["Idempotency-Key"]).toBe("idem_test");
    expect(addOptions?.headers["If-Match"]).toBe('"1"');
  });

  it.each(["FAILED", "QUARANTINED"] as const)(
    "возвращает типизированный конечный результат сканирования %s",
    async (scanState) => {
      const terminal = { ...artifact(), scanState };

      await expect(
        waitForCleanAttachmentArtifact(terminal, new AbortController().signal),
      ).rejects.toEqual(
        expect.objectContaining<Partial<AttachmentTerminalScanError>>({
          name: "AttachmentTerminalScanError",
          scanState,
        }),
      );
    },
  );
});
