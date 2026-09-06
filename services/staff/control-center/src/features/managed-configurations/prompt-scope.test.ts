import { expect, it } from "vitest";
import { promptScopeInput } from "./prompt-scope";

it("сохраняет exact declared scope при редактировании содержимого без output-only полей", () => {
  expect(
    promptScopeInput({
      targetKind: "WORKFLOW_STAGE",
      targetRef: "workflow",
      templateKind: "CONTINUATION",
      contextPin: {
        digest: "a".repeat(64),
        workflowRevisionRef: "draft",
        workflowStageKey: "stage",
        agentRef: "agent",
        agentVersion: 7,
        workflowVersion: 9,
      },
    }),
  ).toEqual({
    targetKind: "WORKFLOW_STAGE",
    targetRef: "workflow",
    templateKind: "CONTINUATION",
    workflowRevisionRef: "draft",
    workflowStageKey: "stage",
    agentRef: "agent",
    expectedContextDigest: "a".repeat(64),
  });
  expect(promptScopeInput()).toBeUndefined();
});
