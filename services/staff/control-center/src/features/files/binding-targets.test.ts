import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ArtifactBindingTarget,
  ArtifactBindingTargetPage,
} from "@/shared/api/generated/openapi/types.gen";
const calls = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  listArtifactBindingTargets: calls.list,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import { bindingTargetEditable, loadBindingTargets } from "./binding-targets";

const scope = { ref: "artifact_one", version: 4, projectRef: "project_one" };
const target: ArtifactBindingTarget = {
  agentRef: "agent_one",
  agentVersion: 2,
  name: "Архивный сотрудник",
  state: "ARCHIVED",
  bound: true,
  canBind: false,
  canUnbind: true,
  bindReason: "AGENT_ARCHIVED",
  unbindReason: "AVAILABLE",
};
const page: ArtifactBindingTargetPage = {
  artifactRef: scope.ref,
  artifactVersion: scope.version,
  projectRef: scope.projectRef,
  items: [target],
  total: 41,
  nextPageToken: "next",
  digest: "d".repeat(64),
  evaluatedAt: "2026-09-05T12:00:00Z",
};
function respond(value: unknown) {
  calls.list.mockResolvedValue({
    data: value,
    response: new Response(null, { status: 200 }),
  });
}
beforeEach(() => calls.list.mockReset());
describe("серверный каталог связей файла", () => {
  it("сохраняет total, query, cursor и право снятия архивной связи", async () => {
    respond(page);
    const signal = new AbortController().signal;
    const result = await loadBindingTargets(
      scope,
      "Архив",
      "previous",
      page.digest,
      signal,
    );
    expect(result).toEqual(page);
    expect(bindingTargetEditable(result.items[0])).toBe(true);
    expect(calls.list).toHaveBeenCalledExactlyOnceWith({
      path: { artifactRef: scope.ref },
      query: { query: "Архив", pageSize: 30, pageToken: "previous" },
      signal,
    });
  });
  it("принимает configured target без runtimeReady и не выводит право из состояния", async () => {
    const configured = {
      ...target,
      state: "DISABLED" as const,
      bound: false,
      canBind: true,
      canUnbind: false,
      bindReason: "AVAILABLE" as const,
      unbindReason: "NOT_BOUND" as const,
    };
    respond({ ...page, items: [configured] });
    expect(
      bindingTargetEditable(
        (
          await loadBindingTargets(
            scope,
            "",
            undefined,
            undefined,
            new AbortController().signal,
          )
        ).items[0],
      ),
    ).toBe(true);
    expect(
      bindingTargetEditable({
        ...configured,
        canBind: false,
        bindReason: "AGENT_CAPABILITY_REQUIRED",
      }),
    ).toBe(false);
  });
  it.each([{ artifactVersion: 5 }, { digest: "changed" }])(
    "отклоняет устаревший snapshot %j",
    async (change) => {
      respond({ ...page, ...change });
      await expect(
        loadBindingTargets(
          scope,
          "",
          "cursor",
          page.digest,
          new AbortController().signal,
        ),
      ).rejects.toMatchObject({ status: 412 });
    },
  );
  it.each([
    { artifactRef: "foreign" },
    { projectRef: "foreign" },
    { total: 0 },
    { total: 1.2 },
    { digest: "" },
    { items: [target, target] },
    { items: [{ ...target, canBind: true }] },
    { items: [{ ...target, canUnbind: false }] },
    { items: [{ ...target, state: "UNKNOWN" }] },
    { items: [{ ...target, unbindReason: "UNKNOWN" }] },
  ])("закрыто отклоняет повреждённый ответ %j", async (change) => {
    respond({ ...page, ...change });
    await expect(
      loadBindingTargets(
        scope,
        "",
        undefined,
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("Invalid artifact binding target page");
  });
  it("не принимает поздний ответ после abort", async () => {
    respond(page);
    const controller = new AbortController();
    controller.abort();
    await expect(
      loadBindingTargets(scope, "", undefined, undefined, controller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
  });
});
