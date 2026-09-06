import { describe, expect, it } from "vitest";
import type { Workflow } from "@/shared/api/generated/openapi/types.gen";
import { workflowLaunchReadiness } from "./workflow-launch";
const workflow: Workflow = {
  ref: "workflow",
  version: 4,
  projectRef: "project",
  name: "Процесс",
  purpose: "Проверка",
  state: "PUBLISHED",
  cardSummary: {
    stageCount: 0,
    uniqueAgentCount: 0,
    parallelGroupCount: 0,
    hasHumanGate: false,
    activeRunCount: 0,
    pendingGateCount: 0,
  },
  publishedRevisionRef: "revision",
  inputFields: [],
  steps: [],
  validationMessages: [],
  updatedAt: "2026-09-06T00:00:00Z",
  nextActions: [],
  launchReadiness: {
    workflowVersion: 4,
    revisionRef: "revision",
    contextDigest: "a".repeat(64),
    reason: "READY",
    allowedToSubmit: true,
    operationalState: "UNKNOWN",
  },
};
describe("Workflow launch admission", () => {
  it.each(["UNKNOWN", "BLOCKED"] as const)(
    "не запрещает разрешённую отправку при operational %s",
    (operationalState) => {
      expect(
        workflowLaunchReadiness({
          ...workflow,
          launchReadiness: { ...workflow.launchReadiness, operationalState },
        })?.allowedToSubmit,
      ).toBe(true);
    },
  );
  it("показывает UNPUBLISHED после сохранения Draft, несмотря на published body", () => {
    expect(
      workflowLaunchReadiness({
        ...workflow,
        state: "DRAFT",
        launchReadiness: {
          ...workflow.launchReadiness,
          reason: "UNPUBLISHED",
          allowedToSubmit: false,
        },
      })?.reason,
    ).toBe("UNPUBLISHED");
  });
  it("не принимает старую проверку версии", () => {
    expect(
      workflowLaunchReadiness({ ...workflow, version: 5 }),
    ).toBeUndefined();
  });
  it("не разрешает DRAFT по противоречивой проекции", () => {
    expect(
      workflowLaunchReadiness({ ...workflow, state: "DRAFT" }),
    ).toBeUndefined();
  });
});
