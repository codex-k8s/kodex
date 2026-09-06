import { reactive } from "vue";
import { beforeEach, expect, it, vi } from "vitest";
import type {
  ArtifactImpact,
  VfsNode,
} from "@/shared/api/generated/openapi/types.gen";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
const calls = vi.hoisted(() => ({
  impact: vi.fn(),
  remove: vi.fn(),
  restore: vi.fn(),
  purge: vi.fn(),
  skillArchive: vi.fn(),
  skillRestore: vi.fn(),
  skillPurge: vi.fn(),
  memoryArchive: vi.fn(),
  memoryRestore: vi.fn(),
  memoryPurge: vi.fn(),
}));
vi.mock("./api", () => ({
  loadArtifactImpact: calls.impact,
  deleteArtifactItem: calls.remove,
  restoreArtifactItem: calls.restore,
  purgeArtifactItem: calls.purge,
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  archiveSkillBundle: calls.skillArchive,
  restoreSkillBundle: calls.skillRestore,
  purgeSkillBundle: calls.skillPurge,
  archiveMemoryRecord: calls.memoryArchive,
  restoreMemoryRecord: calls.memoryRestore,
  purgeMemoryRecord: calls.memoryPurge,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: async (
    request: (headers: Record<string, string>) => Promise<{ data: unknown }>,
    version: number,
  ) =>
    request({
      "If-Match": `"${String(version)}"`,
      "Idempotency-Key": "synthetic",
      "X-CSRF-Token": "synthetic",
    }),
}));
import {
  applyVfsAction,
  prepareVfsAction,
  vfsAction,
  vfsActionController,
} from "./vfs-actions";

function node(
  resourceKind: VfsNode["resourceKind"] = "ARTIFACT",
  patch: Partial<VfsNode> = {},
): VfsNode {
  return {
    ref: `vfs:${resourceKind}`,
    entityRef: resourceKind,
    projectRef: "project",
    runRef: "",
    name: resourceKind,
    kind: "INPUT",
    directory: false,
    path: `/projects/project/${resourceKind}`,
    parentPath: "/projects/project",
    version: 7,
    revisionRef: "revision",
    revision: 2,
    digest: "a".repeat(64),
    sizeBytes: 3,
    lifecycleState: "ACTIVE",
    scanState: "CLEAN",
    resourceKind,
    selectable: true,
    selectionReason: "AVAILABLE",
    nextActions: resourceKind === "ARTIFACT" ? ["DELETE"] : ["ARCHIVE"],
    ...patch,
  };
}
const impact: ArtifactImpact = {
  artifactRef: "ARTIFACT",
  artifactVersion: 7,
  action: "DELETE",
  impactDigest: "b".repeat(64),
  attachmentCount: 0,
  bindingCount: 0,
  activeRuntimeCount: 0,
  activeRuns: [],
  activeRunsTruncated: false,
  blockers: [],
  permitted: true,
};
beforeEach(() => {
  vi.resetAllMocks();
  resetOwnerRequests();
  calls.impact.mockResolvedValue(impact);
});
it("запрет выбора и отсутствие server action не обходятся видом или lifecycle", async () => {
  for (const item of [
    node("ARTIFACT", { selectable: false }),
    node("ARTIFACT", { nextActions: ["DOWNLOAD"] }),
    node("", { nextActions: ["DELETE"] }),
  ]) {
    expect(vfsAction(item, "REMOVE")).toBeUndefined();
    await expect(
      prepareVfsAction([item], "REMOVE", new AbortController().signal),
    ).rejects.toThrow();
  }
  expect(calls.impact).not.toHaveBeenCalled();
});
it("подготавливает reactive snapshots и не изменяет их после редактирования списка", async () => {
  const source = reactive(node());
  const prepared = await prepareVfsAction(
    [source],
    "REMOVE",
    new AbortController().signal,
  );
  source.version = 10;
  source.nextActions.length = 0;
  expect(prepared[0]?.node.version).toBe(7);
  expect(prepared[0]?.node.nextActions).toEqual(["DELETE"]);
  expect(calls.remove).not.toHaveBeenCalled();
});
it("смешанная группа сохраняет отдельные команды, exact версии и результаты", async () => {
  calls.remove.mockResolvedValue({
    ref: "ARTIFACT",
    projectRef: "project",
    version: 8,
    lifecycleState: "DELETED",
  });
  calls.skillArchive.mockRejectedValue(new Error("Synthetic stale version"));
  calls.memoryArchive.mockResolvedValue({
    data: {
      ref: "MEMORY_RECORD",
      projectRef: "project",
      version: 8,
      state: "ARCHIVED",
    },
  });
  const signal = new AbortController().signal;
  const prepared = await prepareVfsAction(
    [node(), node("SKILL_BUNDLE"), node("MEMORY_RECORD")],
    "REMOVE",
    signal,
  );
  const receipts = await applyVfsAction(prepared, signal);
  expect(receipts.map((item) => item.status)).toEqual([
    "SUCCEEDED",
    "FAILED",
    "SUCCEEDED",
  ]);
  expect(calls.remove).toHaveBeenCalledExactlyOnceWith(
    { ref: "ARTIFACT", version: 7 },
    impact,
    signal,
  );
  expect(calls.skillArchive).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({
      path: { bundleRef: "SKILL_BUNDLE" },
      headers: expect.objectContaining({ "If-Match": '"7"' }) as unknown,
    }),
  );
  expect(calls.memoryArchive).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({
      path: { recordRef: "MEMORY_RECORD" },
      headers: expect.objectContaining({ "If-Match": '"7"' }) as unknown,
    }),
  );
});
it.each(["RESTORE", "PURGE"] as const)(
  "%s использует специализированные Skill/Memory команды",
  async (action) => {
    const expectedState = action === "PURGE" ? "PURGED" : "ACTIVE";
    const selected = [
      node("SKILL_BUNDLE", { nextActions: [action] }),
      node("MEMORY_RECORD", { nextActions: [action] }),
    ];
    const skill = action === "PURGE" ? calls.skillPurge : calls.skillRestore;
    const memory = action === "PURGE" ? calls.memoryPurge : calls.memoryRestore;
    skill.mockResolvedValue({
      data: {
        ref: "SKILL_BUNDLE",
        projectRef: "project",
        version: 8,
        state: expectedState,
      },
    });
    memory.mockResolvedValue({
      data: {
        ref: "MEMORY_RECORD",
        projectRef: "project",
        version: 8,
        state: expectedState,
      },
    });
    const signal = new AbortController().signal;
    const receipts = await applyVfsAction(
      await prepareVfsAction(selected, action, signal),
      signal,
    );
    expect(receipts.every((item) => item.status === "SUCCEEDED")).toBe(true);
    expect(skill).toHaveBeenCalledTimes(1);
    expect(memory).toHaveBeenCalledTimes(1);
  },
);
it("отзывает подготовленную группу при смене владельца без отправки мутаций", async () => {
  const controller = vfsActionController();
  const prepared = await prepareVfsAction(
    [node()],
    "REMOVE",
    controller.signal,
  );
  resetOwnerRequests();
  await expect(applyVfsAction(prepared, controller.signal)).rejects.toThrow();
  expect(calls.remove).not.toHaveBeenCalled();
});
it("чужой receipt не становится успешным результатом", async () => {
  calls.remove.mockResolvedValue({
    ref: "foreign",
    projectRef: "project",
    version: 8,
  });
  const signal = new AbortController().signal;
  const receipts = await applyVfsAction(
    await prepareVfsAction([node()], "REMOVE", signal),
    signal,
  );
  expect(receipts[0]?.status).toBe("FAILED");
});
it("запрещает повтор одного resource через разные виртуальные пути", async () => {
  await expect(
    prepareVfsAction(
      [node(), node("ARTIFACT", { ref: "other-path" })],
      "REMOVE",
      new AbortController().signal,
    ),
  ).rejects.toThrow();
  expect(calls.impact).not.toHaveBeenCalled();
});
