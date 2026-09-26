import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Project } from "@/shared/api/generated/openapi/types.gen";

const sdk = vi.hoisted(() => ({
  getProject:
    vi.fn<(request: { path: { projectRef: string } }) => Promise<unknown>>(),
}));
const lifetime = vi.hoisted(() => ({ controller: new AbortController() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/owner-lifetime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/api/owner-lifetime")>()),
  ownerRequestSignal: () => lifetime.controller.signal,
}));

import { useGateProjects } from "./gate-projects";

function response(ref: string) {
  return {
    data: { ref, name: `Проект ${ref}` } as Project,
    response: new Response(null, { status: 200 }),
  };
}

describe("адресные названия проектов решений", () => {
  beforeEach(() => {
    sdk.getProject.mockReset();
    lifetime.controller = new AbortController();
  });

  it("читает только уникальные ссылки решений и повторно использует результат", async () => {
    sdk.getProject.mockImplementation((request) =>
      Promise.resolve(response(request.path.projectRef)),
    );
    const catalog = useGateProjects();
    await catalog.ensure(["project_one", "project_two", "project_one"]);
    await catalog.ensure(["project_two"]);
    expect(
      sdk.getProject.mock.calls.map(([request]) => request.path.projectRef),
    ).toEqual(["project_one", "project_two"]);
    expect(Object.keys(catalog.projects.value)).toEqual([
      "project_one",
      "project_two",
    ]);
    catalog.dispose();
  });

  it("не связывает решение с проектом из ответа на другую ссылку", async () => {
    sdk.getProject.mockResolvedValue(response("foreign_project"));
    const catalog = useGateProjects();
    await catalog.ensure(["requested_project"]);
    expect(catalog.projects.value).toEqual({});
    catalog.dispose();
  });

  it("не сохраняет имя проекта после отказа authority", async () => {
    sdk.getProject.mockResolvedValue({
      error: { status: 403, code: "PERMISSION_DENIED" },
      response: new Response(null, { status: 403 }),
    });
    const catalog = useGateProjects();
    await catalog.ensure(["restricted_project"]);
    expect(catalog.projects.value).toEqual({});
    catalog.dispose();
  });

  it("отбрасывает старый ответ после realtime-сброса и принимает новое чтение", async () => {
    let finish!: (value: ReturnType<typeof response>) => void;
    sdk.getProject.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const catalog = useGateProjects();
    const stale = catalog.ensure(["project_one"]);
    catalog.invalidate();
    sdk.getProject.mockResolvedValue(response("project_one"));
    await catalog.ensure(["project_one"]);
    const fresh = catalog.projects.value.project_one;
    finish(response("project_one"));
    await stale;
    expect(catalog.projects.value.project_one).toBe(fresh);
    expect(sdk.getProject).toHaveBeenCalledTimes(2);
    catalog.dispose();
  });

  it("не принимает поздний ответ после завершения страницы или owner-сессии", async () => {
    let finish!: (value: ReturnType<typeof response>) => void;
    sdk.getProject.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const catalog = useGateProjects();
    const pending = catalog.ensure(["project_one"]);
    catalog.dispose();
    finish(response("project_one"));
    await pending;
    expect(catalog.projects.value).toEqual({});

    sdk.getProject.mockResolvedValue(response("project_two"));
    const next = useGateProjects();
    lifetime.controller.abort();
    await next.ensure(["project_two"]);
    expect(next.projects.value).toEqual({});
  });
});
