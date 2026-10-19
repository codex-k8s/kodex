import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import { AppProblem } from "@/shared/api/problem";
import type { ProjectPurgeRecovery } from "@/shared/api/generated/openapi/types.gen";
import { useProjectPurgeRecoveryStore } from "./purge-recovery-store";
import {
  canResolvePurgeRecovery,
  purgeRecoveryStatusKey,
} from "./purge-recovery";

const api = vi.hoisted(() => ({
  loadProjectPurgeRecovery: vi.fn(),
  resolveProjectPurgeRecovery: vi.fn(),
}));
vi.mock("./purge-recovery-api", () => api);

function fixture(
  overrides: Partial<ProjectPurgeRecovery> = {},
): ProjectPurgeRecovery {
  return {
    projectRef: "prj_synthetic",
    version: 3,
    snapshotDigest: "a".repeat(64),
    state: "WAITING_OWNER",
    unknownContentCount: 2,
    cleanupPendingCount: 0,
    projectPresent: true,
    canResolve: true,
    resolutionRef: "",
    projectVersion: 6,
    ...overrides,
  };
}
function failure(status: number): AppProblem {
  return new AppProblem({
    status,
    code: "SYNTHETIC_FAILURE",
    kind: status === 412 ? "conflict" : "unavailable",
    retryable: status === 0,
  });
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
beforeEach(() => {
  setActivePinia(createPinia());
  resetOwnerRequests();
  vi.resetAllMocks();
});

describe("Подтверждаемое продолжение удаления проекта", () => {
  it("не отправляет решение до отдельного согласия и не повторяет автоматически", async () => {
    const store = useProjectPurgeRecoveryStore();
    api.loadProjectPurgeRecovery.mockResolvedValue(fixture());
    api.resolveProjectPurgeRecovery.mockResolvedValue(
      fixture({
        version: 4,
        state: "QUEUED",
        canResolve: false,
        cleanupPendingCount: 2,
      }),
    );
    await store.load("prj_synthetic");
    expect(store.acknowledged).toBe(false);
    expect(await store.resolve()).toBe(false);
    expect(api.resolveProjectPurgeRecovery).not.toHaveBeenCalled();
    store.acknowledged = true;
    expect(await store.resolve()).toBe(true);
    expect(store.acknowledged).toBe(false);
    expect(store.preview?.cleanupPendingCount).toBe(2);
    expect(purgeRecoveryStatusKey(store.preview!)).toBe(
      "projects.recovery.cleanupPending",
    );
    await store.resolve();
    expect(api.resolveProjectPurgeRecovery).toHaveBeenCalledTimes(1);
  });

  it("stale ответ обновляет preview и сбрасывает согласие без второго POST", async () => {
    const store = useProjectPurgeRecoveryStore();
    api.loadProjectPurgeRecovery
      .mockResolvedValueOnce(fixture())
      .mockResolvedValueOnce(
        fixture({ version: 4, snapshotDigest: "b".repeat(64) }),
      );
    api.resolveProjectPurgeRecovery.mockRejectedValue(failure(412));
    await store.load("prj_synthetic");
    store.acknowledged = true;
    await store.resolve();
    expect(store.preview?.version).toBe(4);
    expect(store.stale).toBe(true);
    expect(store.acknowledged).toBe(false);
    expect(store.canSubmit).toBe(false);
    expect(api.resolveProjectPurgeRecovery).toHaveBeenCalledTimes(1);
    expect(api.loadProjectPurgeRecovery).toHaveBeenCalledTimes(2);
  });

  it("неопределённый ответ требует readback и явного повторного согласия с тем же ключом", async () => {
    const store = useProjectPurgeRecoveryStore();
    api.loadProjectPurgeRecovery.mockResolvedValue(fixture());
    api.resolveProjectPurgeRecovery
      .mockRejectedValueOnce(failure(0))
      .mockResolvedValueOnce(fixture({ state: "QUEUED", canResolve: false }));
    await store.load("prj_synthetic");
    store.acknowledged = true;
    await store.resolve();
    const key: unknown = api.resolveProjectPurgeRecovery.mock.calls[0]?.[1];
    expect(store.preview).toBeUndefined();
    expect(store.uncertain).toBe(true);
    await store.resolve();
    store.close();
    await store.load("prj_synthetic");
    expect(store.acknowledged).toBe(false);
    expect(api.resolveProjectPurgeRecovery).toHaveBeenCalledTimes(1);
    store.acknowledged = true;
    await store.resolve();
    expect(api.resolveProjectPurgeRecovery.mock.calls[1]?.[1]).toBe(key);
  });

  it("позднее чтение закрытой модалки/другого проекта не меняет новый preview", async () => {
    const store = useProjectPurgeRecoveryStore();
    const old = deferred<ProjectPurgeRecovery>();
    api.loadProjectPurgeRecovery
      .mockReturnValueOnce(old.promise)
      .mockResolvedValueOnce(fixture({ projectRef: "prj_next" }));
    const loading = store.load("prj_synthetic");
    store.close();
    await store.load("prj_next");
    old.resolve(fixture());
    await loading;
    expect(store.projectRef).toBe("prj_next");
    expect(store.preview?.projectRef).toBe("prj_next");
    expect(api.loadProjectPurgeRecovery.mock.calls[0]?.[1].aborted).toBe(true);
  });

  it("выход владельца закрывает старый ACK и старое согласие", async () => {
    const store = useProjectPurgeRecoveryStore();
    const old = deferred<ProjectPurgeRecovery>();
    api.loadProjectPurgeRecovery.mockResolvedValue(fixture());
    api.resolveProjectPurgeRecovery.mockReturnValueOnce(old.promise);
    await store.load("prj_synthetic");
    store.acknowledged = true;
    const submitting = store.resolve();
    resetOwnerRequests();
    store.clearOwner();
    await store.load("prj_synthetic");
    old.resolve(fixture({ state: "QUEUED", canResolve: false }));
    await submitting;
    expect(store.preview?.state).toBe("WAITING_OWNER");
    expect(store.acknowledged).toBe(false);
  });

  it("повтор очистки разрешает только server canResolve, даже после удаления проекта", () => {
    expect(
      canResolvePurgeRecovery(
        fixture({
          projectPresent: false,
          unknownContentCount: 0,
          cleanupPendingCount: 1,
        }),
      ),
    ).toBe(true);
    expect(canResolvePurgeRecovery(fixture({ canResolve: false }))).toBe(false);
    expect(canResolvePurgeRecovery(fixture({ state: "BLOCKED" }))).toBe(false);
    expect(canResolvePurgeRecovery(fixture({ version: 0 }))).toBe(false);
    expect(
      canResolvePurgeRecovery(fixture({ snapshotDigest: "invalid" })),
    ).toBe(false);
  });

  it("во время отправки один double click не создаёт второе решение", async () => {
    const store = useProjectPurgeRecoveryStore();
    const pending = deferred<ProjectPurgeRecovery>();
    api.loadProjectPurgeRecovery.mockResolvedValue(fixture());
    api.resolveProjectPurgeRecovery.mockReturnValue(pending.promise);
    await store.load("prj_synthetic");
    store.acknowledged = true;
    const saving = store.resolve();
    store.acknowledged = true;
    await store.resolve();
    expect(api.resolveProjectPurgeRecovery).toHaveBeenCalledTimes(1);
    pending.resolve(fixture({ canResolve: false }));
    await saving;
  });
});
