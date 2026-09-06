import { expect, it } from "vitest";
import type {
  Workflow,
  WorkflowStep,
} from "@/shared/api/generated/openapi/types.gen";
import {
  workflowEditorInput,
  workflowStagePromptTarget,
} from "./workflow-editor";
const step: WorkflowStep = {
  ref: "published-stage",
  position: 1,
  name: "Этап",
  purpose: "Опубликованное назначение",
  expectedResult: "Опубликованный результат",
  agentRef: "agent",
  parallel: false,
  parallelGroup: 0,
  humanGate: false,
  timeoutSeconds: 600,
  gateDecisions: [],
  requiredCapabilityKeys: [],
};
const workflow: Workflow = {
  ref: "workflow",
  version: 7,
  projectRef: "project",
  name: "Процесс",
  purpose: "Назначение",
  state: "PUBLISHED",
  cardSummary: {
    stageCount: 0,
    uniqueAgentCount: 0,
    parallelGroupCount: 0,
    hasHumanGate: false,
    activeRunCount: 0,
    pendingGateCount: 0,
  },
  revisionRef: "published",
  publishedRevisionRef: "published",
  launchReadiness: {
    allowedToSubmit: true,
    reason: "READY",
    workflowVersion: 7,
    revisionRef: "published",
    operationalState: "UNKNOWN",
    contextDigest: "a".repeat(64),
  },
  draftRevisionRef: "draft",
  inputFields: [],
  steps: [step],
  validationMessages: [],
  updatedAt: "2026-09-06T00:00:00Z",
  nextActions: ["EDIT"],
  draft: {
    ref: "draft",
    version: 3,
    revision: 2,
    state: "DRAFT",
    inputFields: [],
    steps: [
      {
        ...step,
        ref: "draft-stage",
        purpose: "Сохранённое новое назначение",
        expectedResult: "Новый результат",
      },
    ],
    validationMessages: [],
  },
};
it("после save и нового GET сохраняет Draft body и exact stage/ref, не подменяет опубликованным", () => {
  const saved = workflowEditorInput(workflow);
  expect(saved.steps[0]?.purpose).toBe("Сохранённое новое назначение");
  const refetched = structuredClone(workflow);
  expect(workflowEditorInput(refetched)).toEqual(saved);
  expect(workflowStagePromptTarget(refetched, 1)).toEqual({
    projectRef: "project",
    targetKind: "WORKFLOW_STAGE",
    targetRef: "workflow",
    context: {
      expectedWorkflowVersion: 7,
      workflowRevisionRef: "draft",
      workflowStageKey: "draft-stage",
    },
  });
  expect(workflow.steps[0]?.purpose).toBe("Опубликованное назначение");
});
it("published-only preview использует только exact displayed revision и не выводит ref из ordinal", () => {
  const published = {
    ...workflow,
    draft: undefined,
    draftRevisionRef: undefined,
  };
  expect(workflowEditorInput(published).steps[0]?.purpose).toBe(step.purpose);
  expect(
    workflowStagePromptTarget(published, 1)?.context?.workflowRevisionRef,
  ).toBe("published");
  expect(
    workflowStagePromptTarget(
      { ...published, revisionRef: undefined, revision: 42 },
      1,
    ),
  ).toBeUndefined();
  expect(workflowStagePromptTarget(workflow, 9)).toBeUndefined();
});
