import { beforeEach, describe, expect, it, vi } from "vitest";

import { createAttachmentSetDraftStore } from "@/shared/api/attachment-set-store";
import type { AttachmentSet } from "@/shared/api/generated/openapi/types.gen";

const createDraftMock = vi.hoisted(() => vi.fn());
const addItemsMock = vi.hoisted(() =>
  vi.fn<
    (
      current: AttachmentSet,
      artifactRefs: string[],
      insertAfterPosition: number,
      signal?: AbortSignal,
    ) => Promise<AttachmentSet>
  >(),
);
const removeItemsMock = vi.hoisted(() =>
  vi.fn<
    (
      current: AttachmentSet,
      artifactRefs: string[],
      signal?: AbortSignal,
    ) => Promise<AttachmentSet>
  >(),
);
const finalizeDraftMock = vi.hoisted(() =>
  vi.fn<
    (current: AttachmentSet, signal?: AbortSignal) => Promise<AttachmentSet>
  >(),
);

vi.mock("@/shared/api/attachment-sets", () => ({
  createAttachmentDraft: createDraftMock,
  addAttachmentItems: addItemsMock,
  removeAttachmentItems: removeItemsMock,
  finalizeAttachmentDraft: finalizeDraftMock,
  attachmentMutationBatches: (references: readonly string[]) => {
    const batches: string[][] = [];
    for (let offset = 0; offset < references.length; offset += 100)
      batches.push(references.slice(offset, offset + 100));
    return batches;
  },
}));

function attachmentSet(
  version: number,
  state: AttachmentSet["state"] = "DRAFT",
): AttachmentSet {
  return {
    ref: `aset_revision_${String(version).padStart(2, "0")}`,
    familyRef: "asetf_project_run_input",
    revision: version,
    version,
    projectRef: "prj_sales",
    state,
    purpose: "RUN_INPUT",
    source: "CONTROL_CENTER",
    itemCount: 0,
    totalSizeBytes: 0,
    items: [],
    createdAt: "2026-08-30T07:00:00Z",
    superseded: false,
    ...(state === "FINALIZED"
      ? {
          manifestDigest: "a".repeat(64),
          finalizedAt: "2026-08-30T07:01:00Z",
        }
      : {}),
  };
}

describe("AttachmentSet draft store", () => {
  beforeEach(() => {
    createDraftMock.mockReset();
    addItemsMock.mockReset();
    removeItemsMock.mockReset();
    finalizeDraftMock.mockReset();
    createDraftMock.mockResolvedValue(attachmentSet(1));
    addItemsMock.mockImplementation((current: AttachmentSet) =>
      Promise.resolve(attachmentSet(current.version + 1)),
    );
    removeItemsMock.mockImplementation((current: AttachmentSet) =>
      Promise.resolve(attachmentSet(current.version + 1)),
    );
    finalizeDraftMock.mockImplementation((current: AttachmentSet) =>
      Promise.resolve(attachmentSet(current.version + 1, "FINALIZED")),
    );
  });

  it("создаёт empty draft, добавляет неограниченный список bounded batches и finalizes exact revision", async () => {
    const references = Array.from(
      { length: 205 },
      (_, index) => `art_${String(index).padStart(4, "0")}`,
    );
    const changed = vi.fn();
    const store = createAttachmentSetDraftStore({
      projectRef: () => "prj_sales",
      purpose: () => "RUN_INPUT",
      changed,
    });

    await store.reconcile(references);

    expect(createDraftMock).toHaveBeenCalledWith(
      "prj_sales",
      "RUN_INPUT",
      expect.any(AbortSignal),
    );
    expect(addItemsMock).toHaveBeenCalledTimes(3);
    expect(addItemsMock.mock.calls.map((call) => call[1].length)).toEqual([
      100, 100, 5,
    ]);
    expect(addItemsMock.mock.calls.map((call) => call[2])).toEqual([
      0, 100, 200,
    ]);
    expect(store.state().references).toEqual(references);

    await expect(store.finalize()).resolves.toBe("aset_revision_05");
    await expect(store.finalize()).resolves.toBe("aset_revision_05");
    expect(finalizeDraftMock).toHaveBeenCalledTimes(1);
    expect(store.state().attachmentSet?.state).toBe("FINALIZED");
    expect(changed).toHaveBeenCalled();
  });

  it("удаляет и добавляет items последовательными OCC-ревизиями", async () => {
    const store = createAttachmentSetDraftStore({
      projectRef: () => "prj_sales",
      purpose: () => "RUN_INPUT",
      changed: vi.fn(),
    });

    await store.reconcile(["art_a", "art_b"]);
    await store.reconcile(["art_b", "art_c"]);

    expect(removeItemsMock).toHaveBeenCalledWith(
      expect.objectContaining({ version: 2 }),
      ["art_a"],
      expect.any(AbortSignal),
    );
    expect(addItemsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ version: 3 }),
      ["art_c"],
      1,
      expect.any(AbortSignal),
    );
    expect(store.state().references).toEqual(["art_b", "art_c"]);
  });

  it("закрыто блокирует finalize после ошибки и позволяет повторить sync", async () => {
    addItemsMock.mockRejectedValueOnce(new Error("temporary conflict"));
    const store = createAttachmentSetDraftStore({
      projectRef: () => "prj_sales",
      purpose: () => "RUN_INPUT",
      changed: vi.fn(),
    });

    await store.reconcile(["art_a"]);
    expect(store.state().error).toBeInstanceOf(Error);

    await store.retry();
    expect(store.state().error).toBeUndefined();
    await expect(store.finalize()).resolves.toBe("aset_revision_03");
  });

  it("создаёт organization-scoped набор для глобального assistant без фиктивного Project", async () => {
    const store = createAttachmentSetDraftStore({
      projectRef: () => undefined,
      purpose: () => "ASSISTANT_MESSAGE",
      changed: vi.fn(),
    });

    await store.reconcile(["art_organization"]);

    expect(createDraftMock).toHaveBeenCalledWith(
      undefined,
      "ASSISTANT_MESSAGE",
      expect.any(AbortSignal),
    );
    await expect(store.finalize()).resolves.toBe("aset_revision_03");
  });

  it("не создаёт пустой набор и закрыто отклоняет project purpose без Project", async () => {
    const empty = createAttachmentSetDraftStore({
      projectRef: () => undefined,
      purpose: () => "ASSISTANT_MESSAGE",
      changed: vi.fn(),
    });

    await empty.reconcile([]);
    await expect(empty.finalize()).resolves.toBeUndefined();
    expect(createDraftMock).not.toHaveBeenCalled();

    const invalid = createAttachmentSetDraftStore({
      projectRef: () => undefined,
      purpose: () => "RUN_INPUT",
      changed: vi.fn(),
    });
    await invalid.reconcile(["art_a"]);
    expect(invalid.state().error).toEqual(
      new Error("AttachmentSet purpose requires a Project context"),
    );
    expect(createDraftMock).not.toHaveBeenCalled();
  });
});
