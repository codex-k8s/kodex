import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AccessBinding } from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";

const fetchAccessSubjects = vi.hoisted(() => vi.fn());
const fetchAccessBindings = vi.hoisted(() => vi.fn());
const fetchPlatformMemberships = vi.hoisted(() => vi.fn());
const fetchProjectMemberships = vi.hoisted(() => vi.fn());
const fetchAccessRoles = vi.hoisted(() => vi.fn());
const fetchAccessRoleVersions = vi.hoisted(() => vi.fn());
const addAccessRole = vi.hoisted(() => vi.fn());
const addAccessBinding = vi.hoisted(() => vi.fn());

vi.mock("@/features/access/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/access/api")>()),
  fetchAccessSubjects,
  fetchAccessBindings,
  fetchPlatformMemberships,
  fetchProjectMemberships,
  fetchAccessRoles,
  fetchAccessRoleVersions,
  addAccessRole,
  addAccessBinding,
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

function accessRole(ref: string, name = ref) {
  return {
    ref,
    version: 1,
    kind: "CUSTOM" as const,
    state: "ACTIVE" as const,
    currentVersion: {
      ref: `${ref}_v1`,
      roleRef: ref,
      revision: 1,
      name,
      description: "Точечный запуск сотрудника",
      permissionKeys: ["agent.read", "agent.run"],
      allowedScopes: ["RESOURCE_INSTANCE" as const],
      changeComment: "Проверка RBAC",
      createdAt: "2026-09-03T00:00:00Z",
      createdBy: {
        ref: "subject_owner",
        displayName: "Владелец",
        emailMasked: "o***@kodex.local",
      },
    },
    bindingCount: 0,
    updatedAt: "2026-09-03T00:00:00Z",
  };
}

function accessBinding(ref: string): AccessBinding {
  const role = accessRole(`role_${ref}`);
  return {
    ref,
    version: 1,
    state: "ACTIVE",
    subject: subject("subject_sales"),
    roleVersion: role.currentVersion,
    scope: {
      kind: "RESOURCE_INSTANCE",
      projectRef: "project_sales",
      resourceKind: "AGENT",
      resourceRef: "agent_coordinator",
    },
    conditions: { requireOwner: false },
    createdAt: "2026-09-03T00:00:00Z",
    updatedAt: "2026-09-03T00:00:00Z",
  };
}

describe("access store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    fetchAccessSubjects.mockReset();
    fetchAccessBindings.mockReset();
    fetchPlatformMemberships.mockReset();
    fetchProjectMemberships.mockReset();
    fetchAccessRoles.mockReset();
    fetchAccessRoleVersions.mockReset();
    addAccessRole.mockReset();
    addAccessBinding.mockReset();
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

  it("оставляет созданную роль видимой, если первая страница readback её не содержит", async () => {
    const created = accessRole(
      "role_created",
      "e2e — точечный запуск сотрудника",
    );
    const firstPage = Array.from({ length: 50 }, (_, index) =>
      accessRole(`role_${String(index).padStart(2, "0")}`),
    );
    addAccessRole.mockResolvedValue(created);
    fetchAccessRoles.mockResolvedValue({
      items: firstPage,
      nextPageToken: "role_49",
    });
    fetchAccessRoleVersions.mockResolvedValue({ role: created, items: [] });
    const store = useAccessStore();

    const result = await store.saveRole({
      name: created.currentVersion.name,
      description: created.currentVersion.description,
      permissionKeys: created.currentVersion.permissionKeys,
      allowedScopes: created.currentVersion.allowedScopes,
      changeComment: created.currentVersion.changeComment,
    });

    expect(result).toEqual(created);
    expect(store.roles).toContainEqual(created);
    expect(store.roles).toHaveLength(51);
    expect(store.roleNextPageToken).toBe("role_49");
    expect(fetchAccessRoleVersions).toHaveBeenCalledWith(created.ref);
  });

  it("оставляет созданную привязку видимой, если первая страница readback её не содержит", async () => {
    const created = accessBinding("binding_created");
    const firstPage = Array.from({ length: 50 }, (_, index) =>
      accessBinding(`binding_${String(index).padStart(2, "0")}`),
    );
    addAccessBinding.mockResolvedValue(created);
    fetchAccessBindings.mockResolvedValue({
      items: firstPage,
      nextPageToken: "binding_49",
    });
    const store = useAccessStore();

    const result = await store.saveBinding({
      subjectKind: created.subject.kind,
      subjectRef: created.subject.ref,
      roleVersionRef: created.roleVersion.ref,
      scope: created.scope,
      conditions: created.conditions,
    });

    expect(result).toEqual(created);
    expect(store.bindings).toContainEqual(created);
    expect(store.bindings).toHaveLength(51);
    expect(store.bindingNextPageToken).toBe("binding_49");
  });

  it("собирает все страницы активных ролей для новой привязки", async () => {
    const firstPage = Array.from({ length: 50 }, (_, index) =>
      accessRole(`role_${String(index).padStart(2, "0")}`),
    );
    const expected = accessRole("role_target", "Точечный запуск сотрудника");
    fetchAccessRoles
      .mockResolvedValueOnce({
        items: firstPage,
        nextPageToken: "role_49",
      })
      .mockResolvedValueOnce({ items: [expected] });
    const store = useAccessStore();

    await store.loadBindingRoles();

    expect(fetchAccessRoles).toHaveBeenNthCalledWith(1, {
      includeArchived: false,
      pageToken: undefined,
    });
    expect(fetchAccessRoles).toHaveBeenNthCalledWith(2, {
      includeArchived: false,
      pageToken: "role_49",
    });
    expect(store.bindingRoles).toHaveLength(51);
    expect(store.bindingRoles).toContainEqual(expected);
    expect(store.roles).toEqual([]);
    expect(store.roleNextPageToken).toBeUndefined();
  });
});
