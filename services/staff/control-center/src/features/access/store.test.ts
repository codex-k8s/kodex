import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AppProblem } from "@/shared/api/problem";

const fetchAccessSubjects = vi.hoisted(() => vi.fn());
const fetchAccessBindings = vi.hoisted(() => vi.fn());
const fetchPlatformMemberships = vi.hoisted(() => vi.fn());
const fetchProjectMemberships = vi.hoisted(() => vi.fn());

vi.mock("@/features/access/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/access/api")>()),
  fetchAccessSubjects,
  fetchAccessBindings,
  fetchPlatformMemberships,
  fetchProjectMemberships,
}));

import { useAccessStore } from "@/features/access/store";

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((ready) => {
    resolve = ready;
  });
  return { promise, resolve };
}

function subject(ref: string) {
  return {
    ref,
    kind: "USER" as const,
    displayName: ref,
    active: true,
    oidcGroupRefs: [],
  };
}

describe("access store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    fetchAccessSubjects.mockReset();
    fetchAccessBindings.mockReset();
    fetchPlatformMemberships.mockReset();
    fetchProjectMemberships.mockReset();
  });

  it("не позволяет старому поиску участников заменить новый", async () => {
    const oldRequest = deferred<{ items: ReturnType<typeof subject>[] }>();
    const newRequest = deferred<{ items: ReturnType<typeof subject>[] }>();
    fetchAccessSubjects
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(newRequest.promise);
    const store = useAccessStore();

    const oldLoad = store.loadSubjects("старый");
    const newLoad = store.loadSubjects("новый");
    newRequest.resolve({ items: [subject("subject_new")] });
    await newLoad;
    oldRequest.resolve({ items: [subject("subject_old")] });
    await oldLoad;

    expect(store.subjects.map((item) => item.ref)).toEqual(["subject_new"]);
    expect(store.loading.subjects).toBe(false);
  });

  it("сохраняет forbidden как явное состояние сценария", async () => {
    fetchAccessSubjects.mockRejectedValue(
      new AppProblem({
        status: 403,
        code: "FORBIDDEN",
        retryable: false,
        kind: "forbidden",
      }),
    );
    const store = useAccessStore();

    await store.loadSubjects();

    expect(store.problems.subjects?.kind).toBe("forbidden");
    expect(store.subjects).toEqual([]);
  });

  it("передаёт project filter авторитетному API bindings", async () => {
    fetchAccessBindings.mockResolvedValue({ items: [] });
    const store = useAccessStore();

    await store.loadBindings({
      projectRef: "project_sales",
      includeRevoked: true,
    });

    expect(fetchAccessBindings).toHaveBeenCalledWith({
      projectRef: "project_sales",
      includeRevoked: true,
      pageToken: undefined,
    });
  });

  it("загружает platform role и Project membership отдельными read paths", async () => {
    const platformMembership = {
      ref: "membership_platform",
      version: 1,
      user: { ref: "subject_owner", displayName: "Владелец" },
      platformRole: "OWNER" as const,
      permissions: [],
      active: true,
      nextActions: [],
    };
    const projectMembership = {
      ...platformMembership,
      ref: "membership_project",
      permissions: ["MANAGE_AGENTS" as const],
    };
    fetchPlatformMemberships.mockResolvedValue([platformMembership]);
    fetchProjectMemberships.mockResolvedValue([projectMembership]);
    const store = useAccessStore();

    await store.loadMembershipPresentation("project_sales");

    expect(fetchPlatformMemberships).toHaveBeenCalledOnce();
    expect(fetchProjectMemberships).toHaveBeenCalledWith("project_sales");
    expect(store.platformMemberships).toEqual([platformMembership]);
    expect(store.projectMemberships).toEqual([projectMembership]);
  });
});
