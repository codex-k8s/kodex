import { describe, expect, it, vi } from "vitest";
import type {
  VfsNode,
  VfsNodePage,
} from "@/shared/api/generated/openapi/types.gen";
const calls = vi.hoisted(() => ({ list: vi.fn(), search: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listVfsNodes: calls.list,
  searchVfs: calls.search,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
import { loadVfsPage, validateVfsPage, vfsEntityRoute } from "./vfs";
const node: VfsNode = {
  ref: "node_one",
  name: "Проект",
  path: "/projects/project_one",
  parentPath: "/projects",
  kind: "PROJECT",
  directory: true,
  projectRef: "project_one",
  entityRef: "project_one",
  runRef: "",
  sizeBytes: 0,
  digest: "",
};
const page: VfsNodePage = { items: [node], total: 1, nextPageToken: "" };
const scope = { path: "/projects", query: "" };
describe("VFS", () => {
  it("передаёт path/projectRef/cursor владельцу, поиск использует отдельную операцию", async () => {
    calls.list.mockResolvedValue({
      data: page,
      response: new Response(null, { status: 200 }),
    });
    calls.search.mockResolvedValue({
      data: page,
      response: new Response(null, { status: 200 }),
    });
    const controller = new AbortController();
    await loadVfsPage({
      ...scope,
      projectRef: "project_one",
      pageToken: "cursor",
      signal: controller.signal,
    });
    expect(calls.list).toHaveBeenCalledWith(
      expect.objectContaining({
        query: {
          path: "/projects",
          projectRef: "project_one",
          pageToken: "cursor",
          pageSize: 30,
        },
      }),
    );
    await loadVfsPage({
      ...scope,
      query: "  Проект  ",
      projectRef: "project_one",
      signal: controller.signal,
    });
    expect(calls.search).toHaveBeenCalledWith(
      expect.objectContaining({
        query: { query: "Проект", projectRef: "project_one", pageSize: 30 },
      }),
    );
  });
  it("отклоняет чужой scope, неизвестный вид, неправильный путь, дубликаты и oversized cursor", () => {
    expect(() =>
      validateVfsPage(page, { ...scope, projectRef: "other" }),
    ).toThrow();
    expect(() =>
      validateVfsPage(page, { ...scope, path: "/projects/other" }),
    ).toThrow();
    expect(() =>
      validateVfsPage({ ...page, items: [node, node] }, scope),
    ).toThrow();
    expect(() =>
      validateVfsPage({ ...page, nextPageToken: "x".repeat(2049) }, scope),
    ).toThrow();
    for (const invalid of [
      { kind: "TOOL" },
      { path: "https://external.invalid" },
      { sizeBytes: -1 },
      { name: null },
    ]) {
      expect(() =>
        validateVfsPage(
          { ...page, items: [{ ...node, ...invalid } as VfsNode] },
          scope,
        ),
      ).toThrow();
    }
  });
  it("не превращает SKILL/MEMORY в tool, environment или artifact route", () => {
    expect(vfsEntityRoute({ ...node, kind: "SKILL" })).toBe(
      `/projects/${node.projectRef}/context/skills/${node.entityRef}`,
    );
    expect(vfsEntityRoute({ ...node, kind: "MEMORY" })).toBe(
      `/projects/${node.projectRef}/context/memory/${node.entityRef}`,
    );
    expect(
      vfsEntityRoute({ ...node, kind: "INPUT", entityRef: "artifact/safe" }),
    ).toBe("/projects/project_one/files?artifactRef=artifact%2Fsafe");
    expect(
      vfsEntityRoute({ ...node, kind: "AGENT", entityRef: "agent/safe" }),
    ).toBe("/projects/project_one/agents/agent%2Fsafe");
  });
  it("принимает AUTOMATION/ENVIRONMENT/AVATAR и открывает их точные ресурсы", () => {
    for (const kind of ["AUTOMATION", "ENVIRONMENT", "AVATAR"] as const)
      expect(() =>
        validateVfsPage({ ...page, items: [{ ...node, kind }] }, scope),
      ).not.toThrow();
    expect(
      vfsEntityRoute({
        ...node,
        kind: "AUTOMATION",
        entityRef: "schedule_one",
      }),
    ).toBe("/projects/project_one/automations?scheduleRef=schedule_one");
    expect(
      vfsEntityRoute({
        ...node,
        kind: "ENVIRONMENT",
        entityRef: "environment_one",
      }),
    ).toBe("/projects/project_one/environments/environment_one");
    expect(
      vfsEntityRoute({ ...node, kind: "AVATAR", entityRef: "artifact_one" }),
    ).toBe("/projects/project_one/files?artifactRef=artifact_one");
  });
});
