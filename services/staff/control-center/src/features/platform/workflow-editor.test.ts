import { expect, it } from "vitest";
import type {
  Workflow,
  WorkflowStep,
} from "@/shared/api/generated/openapi/types.gen";
import {
  workflowEditorAfterAuthoritativeUpdate,
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
it("сохраняет отдельный срок согласования и не добавляет срок прежнему этапу", () => {
  expect(workflowEditorInput(workflow).steps[0]).not.toHaveProperty(
    "approvalTimeoutSeconds",
  );
  const changed = structuredClone(workflow);
  const changedStep = changed.draft?.steps[0];
  if (!changedStep) throw new Error("Этап оснастки отсутствует");
  changedStep.humanGate = true;
  changedStep.approvalTimeoutSeconds = 90;
  expect(workflowEditorInput(changed).steps[0]?.approvalTimeoutSeconds).toBe(
    90,
  );
  expect(workflowEditorInput(changed).steps[0]?.timeoutSeconds).toBe(600);
});
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
it("подхватывает новую серверную ревизию только для чистого редактора того же процесса", () => {
  const current = workflowEditorInput(workflow);
  const saved = JSON.stringify(current);
  const updated = { ...workflow, name: "Новое название", version: 8 };
  expect(
    workflowEditorAfterAuthoritativeUpdate(
      current,
      saved,
      "project",
      "workflow",
      updated,
    )?.name,
  ).toBe("Новое название");
  expect(
    workflowEditorAfterAuthoritativeUpdate(
      { ...current, name: "Локальный ввод" },
      saved,
      "project",
      "workflow",
      updated,
    ),
  ).toBeUndefined();
  expect(
    workflowEditorAfterAuthoritativeUpdate(
      current,
      saved,
      "other",
      "workflow",
      updated,
    ),
  ).toBeUndefined();
  expect(
    workflowEditorAfterAuthoritativeUpdate(
      current,
      saved,
      "project",
      "other",
      updated,
    ),
  ).toBeUndefined();
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

it("сохраняет DAG-поля отдельного черновика без изменения опубликованной версии", () => {
  const server = structuredClone(workflow);
  server.executionMode = "MANAGER_DRIVEN";
  const draft = server.draft;
  if (!draft) throw new Error("Черновик оснастки отсутствует");
  draft.executionMode = "SERVER_DAG_V1";
  const stage = draft.steps[0];
  if (!stage) throw new Error("Этап оснастки отсутствует");
  stage.ref = "sre";
  stage.dependsOn = ["merge", "qa"];
  stage.humanGate = true;
  stage.approvalTimeoutSeconds = 300;
  stage.resultRequirements = [
    {
      key: "deployment",
      kind: "DEPLOYMENT",
      operation: "ssh.compose.deploy",
      subjectStepKey: "merge",
      repository: "codex-k8s/test-repo",
      targetRef: "ssh_fixture",
    },
    {
      key: "runbook",
      kind: "ARTIFACT",
      fileName: "runbook.md",
      mediaType: "text/markdown",
    },
  ];
  const input = workflowEditorInput(server);
  expect(input.executionMode).toBe("SERVER_DAG_V1");
  expect(input.steps[0]?.ref).toBe("sre");
  expect(input.steps[0]?.dependsOn).toEqual(["merge", "qa"]);
  expect(input.steps[0]?.approvalTimeoutSeconds).toBe(300);
  expect(input.steps[0]?.resultRequirements).toEqual(stage.resultRequirements);
  expect(server.executionMode).toBe("MANAGER_DRIVEN");
  const dependencies = input.steps[0]?.dependsOn;
  const result = input.steps[0]?.resultRequirements?.[0];
  if (!dependencies || !result)
    throw new Error("Поля графа потеряны в редакторе");
  dependencies.push("other");
  result.targetRef = "ssh_changed";
  expect(stage.dependsOn).toEqual(["merge", "qa"]);
  expect(stage.resultRequirements[0]?.targetRef).toBe("ssh_fixture");
  expect(workflowEditorInput(workflow).executionMode).toBe("MANAGER_DRIVEN");
});
