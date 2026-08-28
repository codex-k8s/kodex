import { describe, expect, it, vi } from "vitest";

import {
  createArtifactPickerLoader,
  createSessionPickerLoader,
} from "@/features/new-run/api";
import type { Artifact, Run } from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({
  listArtifacts: vi.fn(),
  listRuns: vi.fn(),
}));

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

function response<T>(data: T) {
  return Promise.resolve({
    data,
    response: new Response(null, { status: 200 }),
  });
}

function artifact(overrides: Partial<Artifact> = {}): Artifact {
  return {
    ref: "artifact_1",
    version: 1,
    projectRef: "project_1",
    fileName: "brief.pdf",
    mediaType: "application/pdf",
    sizeBytes: 1024,
    digest: "sha256:test",
    scanState: "CLEAN",
    source: "CONTROL_CENTER",
    revision: 2,
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-08-28T10:00:00Z",
    nextActions: ["DOWNLOAD"],
    ...overrides,
  };
}

function run(overrides: Partial<Run> = {}): Run {
  return {
    ref: "run_1",
    version: 1,
    projectRef: "project_1",
    sessionRef: "session_1",
    rootRunRef: "run_1",
    target: {
      type: "WORKFLOW",
      ref: "workflow_1",
      displayName: "Процесс",
      version: 1,
    },
    title: "Итоговый запуск",
    titleSource: "USER_EDITED",
    activitySummary: "Итоговый запуск успешно завершён",
    state: "SUCCEEDED",
    source: "CONTROL_CENTER",
    initiator: { ref: "user_1", displayName: "Анна" },
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: 2,
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
    inputArtifactRefs: [],
    artifactRefs: [],
    gateRefs: [],
    createdAt: "2026-08-28T10:00:00Z",
    nextActions: ["OPEN", "ADD_TURN"],
    ...overrides,
  };
}

describe("new-run cursor API adapters", () => {
  it("передаёт project, поиск и cursor в listArtifacts", async () => {
    api.listArtifacts.mockReturnValueOnce(
      response({ items: [artifact()], nextPageToken: "artifact-page-2" }),
    );
    const loader = createArtifactPickerLoader("project_1");

    const page = await loader({
      query: " brief ",
      cursor: "artifact-page-1",
      signal: new AbortController().signal,
    });

    expect(api.listArtifacts).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_1" },
        query: {
          pageSize: 40,
          pageToken: "artifact-page-1",
          query: "brief",
        },
      }),
    );
    expect(page.items[0]).toMatchObject({
      id: "artifact_1",
      disabled: false,
    });
    expect(page.nextCursor).toBe("artifact-page-2");
  });

  it("пропускает нерелевантную страницу runs и дедуплицирует session", async () => {
    api.listRuns
      .mockReturnValueOnce(
        response({
          items: [run({ state: "RUNNING" })],
          nextPageToken: "run-page-2",
        }),
      )
      .mockReturnValueOnce(
        response({
          items: [
            run({ ref: "run_latest" }),
            run({ ref: "run_old", createdAt: "2026-08-27T10:00:00Z" }),
          ],
          nextPageToken: "run-page-3",
        }),
      );
    const loader = createSessionPickerLoader({
      projectRef: "project_1",
      targetRef: "workflow_1",
      targetType: "WORKFLOW",
    });

    const page = await loader({
      query: "итог",
      signal: new AbortController().signal,
    });

    expect(api.listRuns).toHaveBeenCalledTimes(2);
    expect(page.items.map((item) => item.run.ref)).toEqual(["run_latest"]);
    expect(page.nextCursor).toBe("run-page-3");
    expect(api.listRuns).toHaveBeenLastCalledWith(
      expect.objectContaining({
        query: {
          projectRef: "project_1",
          pageSize: 100,
          pageToken: "run-page-2",
          query: "итог",
        },
      }),
    );
  });
});
