import { describe, expect, it } from "vitest";

import {
  fileExtension,
  fileVisualKind,
  formatFileSize,
  isResumableRun,
  uniqueResumableRuns,
} from "@/features/new-run/model";
import type { Run } from "@/shared/api/generated/openapi/types.gen";

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
      displayName: "Проверка договора",
      version: 2,
    },
    title: "Проверить договор",
    titleSource: "USER_EDITED",
    activitySummary: "Проверка договора завершена",
    state: "SUCCEEDED",
    source: "CONTROL_CENTER",
    initiator: { ref: "user_1", displayName: "Анна" },
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: 4,
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

describe("new-run file model", () => {
  it("определяет тип по имени и MIME без зависимости от регистра", () => {
    expect(fileExtension("Отчёт.XLSX", "application/octet-stream")).toBe(
      "xlsx",
    );
    expect(fileVisualKind("Отчёт.XLSX", "application/octet-stream")).toBe(
      "spreadsheet",
    );
    expect(fileVisualKind("photo", "image/webp")).toBe("image");
    expect(fileVisualKind("contract.pdf", "application/pdf")).toBe("pdf");
  });

  it("форматирует размер через локализованный Intl unit formatter", () => {
    expect(formatFileSize(1536, "en-US")).toBe("1.5 kB");
    expect(formatFileSize(2 * 1024 * 1024, "en-US")).toBe("2 MB");
  });
});

describe("new-run session model", () => {
  const scope = {
    projectRef: "project_1",
    targetRef: "workflow_1",
    targetType: "WORKFLOW" as const,
  };

  it("допускает только terminal run точной цели и Проекта", () => {
    expect(isResumableRun(run(), scope)).toBe(true);
    expect(isResumableRun(run({ state: "RUNNING" }), scope)).toBe(false);
    expect(isResumableRun(run({ projectRef: "project_2" }), scope)).toBe(false);
    expect(
      isResumableRun(
        run({
          target: {
            type: "AGENT",
            ref: "agent_1",
            displayName: "Аналитик",
            version: 1,
          },
        }),
        scope,
      ),
    ).toBe(false);
  });

  it("оставляет только новейший run каждой сессии", () => {
    const items = uniqueResumableRuns(
      [
        run({ ref: "run_latest" }),
        run({ ref: "run_old" }),
        run({ ref: "run_other", sessionRef: "session_2" }),
      ],
      scope,
    );

    expect(items.map((item) => item.ref)).toEqual(["run_latest", "run_other"]);
  });
});
