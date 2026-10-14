import { describe, expect, it } from "vitest";
import type {
  AssistantPlan,
  AssistantPlanOperation,
} from "@/shared/api/generated/openapi/types.gen";
import {
  appliedProjectFileRevision,
  projectFileRevisionSource,
} from "./project-file-plan";
import {
  editableOperations,
  friendlyPlanOperationType,
  operationInputs,
  updateOperationParameter,
} from "./model";

import {
  appliedFilePlanFixture,
  fileOperationFixture,
  fileRevisionFixture,
  requireFixture,
} from "./project-file-plan.fixtures";

describe("typed project file revision plan", () => {
  it("friendly UPDATE адресует тот же stable Artifact и точный source", () => {
    const operation = fileOperationFixture();
    expect(projectFileRevisionSource(operation)).toEqual({
      artifactRef: "art_fixture",
      revisionRef: "arv_previous",
      revision: 1,
    });
    expect(
      friendlyPlanOperationType(
        requireFixture(editableOperations([operation])[0]),
      ),
    ).toBe("CREATE_PROJECT_FILE_REVISION");
  });
  it.each([
    (operation: AssistantPlanOperation) => {
      operation.action = "CREATE";
    },
    (operation: AssistantPlanOperation) => {
      operation.expectedVersion = 6;
    },
    (operation: AssistantPlanOperation) => {
      operation.parameters.artifactRef = "art_foreign";
    },
    (operation: AssistantPlanOperation) => {
      operation.before.currentRevisionRef = "arv_foreign";
    },
    (operation: AssistantPlanOperation) => {
      operation.before.fileName = "another.md";
    },
    (operation: AssistantPlanOperation) => {
      operation.before.lifecycleState = "DELETED";
    },
    (operation: AssistantPlanOperation) => {
      operation.after.createsImmutableRevision = false;
    },
  ])("не подменяет source/action/OCC", (change) => {
    const operation = fileOperationFixture();
    change(operation);
    expect(projectFileRevisionSource(operation)).toBeUndefined();
  });
  it("replacement сохраняет before/after/target/OCC, удаляет staged pins только из parameters", () => {
    const original = fileOperationFixture();
    const edited = requireFixture(editableOperations([original])[0]);
    updateOperationParameter(edited, "content", "Новый текст");
    updateOperationParameter(edited, "mediaType", "text/plain");
    const input = requireFixture(operationInputs([edited])[0]);
    expect(input.parameters).toEqual({
      artifactRef: "art_fixture",
      contentEncoding: "UTF8",
      content: "Новый текст",
      mediaType: "text/plain",
    });
    expect(input.before).toEqual(original.before);
    expect(input.after).toEqual(original.after);
    expect(input.target).toEqual(original.target);
    expect(input.expectedVersion).toBe(original.expectedVersion);
  });
  it("receipt требует exact plan/revision/selected operation и immutable pins", () => {
    const plan = appliedFilePlanFixture();
    expect(appliedProjectFileRevision(plan, "op_file")).toEqual({
      projectRef: "prj_file",
      revision: fileRevisionFixture(),
    });
    requireFixture(plan.receipt).planRevision--;
    expect(appliedProjectFileRevision(plan, "op_file")).toBeUndefined();
  });
  it.each([
    (plan: AssistantPlan) => {
      plan.state = "VALID";
    },
    (plan: AssistantPlan) => {
      requireFixture(plan.operations[0]).selected = false;
    },
    (plan: AssistantPlan) => {
      Object.assign(
        requireFixture(requireFixture(plan.receipt).operationReceipts[0]),
        { outcome: "CONFLICT" },
      );
    },
    (plan: AssistantPlan) => {
      requireFixture(
        requireFixture(plan.receipt).operationReceipts[0],
      ).resourceRef = "art_foreign";
    },
    (plan: AssistantPlan) => {
      delete requireFixture(requireFixture(plan.receipt).operationReceipts[0])
        .artifactRevision;
    },
    (plan: AssistantPlan) => {
      requireFixture(
        requireFixture(requireFixture(plan.receipt).operationReceipts[0])
          .artifactRevision,
      ).digest = `sha256:${"d".repeat(64)}`;
    },
    (plan: AssistantPlan) => {
      const receipt = requireFixture(plan.receipt);
      receipt.operationReceipts.push(
        requireFixture(receipt.operationReceipts[0]),
      );
    },
  ])("не выдаёт malformed receipt за сохранение", (change) => {
    const plan = appliedFilePlanFixture();
    change(plan);
    expect(appliedProjectFileRevision(plan, "op_file")).toBeUndefined();
  });
});
