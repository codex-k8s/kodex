import { describe, expect, it } from "vitest";
import type {
  AssistantPlan,
  AssistantPlanReceipt,
  AssistantPlanOperationInput,
  SystemAssistantIntegrationGrantCandidates,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import {
  editableOperations,
  friendlyPlanOperationType,
  operationInputs,
  updateOperationParameter,
} from "./model";

type GrantPlanContract<Page extends SystemAssistantIntegrationGrantCandidates> =
  {
    label: string;
    expectedType: AssistantPlanOperationInput["type"];
    organizationRef: string;
    connectionRef: string;
    before: Record<string, unknown>;
    operation: () => AssistantPlanOperationInput;
    candidate: SystemAssistantIntegrationGrantCandidate;
    page: Page;
    ownerPatches: readonly Record<string, unknown>[];
    ownerMatches: (
      operation: AssistantPlanOperationInput,
      organizationRef: string | undefined,
    ) => boolean;
    candidateMatches: (
      operation: AssistantPlanOperationInput,
      page: Page,
      candidate: SystemAssistantIntegrationGrantCandidate,
    ) => boolean;
    appliedCandidate: (
      operation: AssistantPlanOperationInput,
      page: Page,
      candidate: SystemAssistantIntegrationGrantCandidate,
      grantRef: string,
    ) => boolean;
    receiptRef: (
      plan: AssistantPlan,
      operationRef: string,
      receipt?: AssistantPlanReceipt,
    ) => string | undefined;
  };

// Создаёт свежий синтетический запрос; owner pins задаёт отдельная scope fixture.
// Данные не выводятся из проверяемых guard-функций.
export function integrationGrantOperationFixture(
  pins: Record<string, unknown> & { connectionRef: string },
  before: Record<string, unknown>,
  type: AssistantPlanOperationInput["type"],
): AssistantPlanOperationInput {
  const after = {
    ...pins,
    enabled: true,
    approvalPolicy: "HUMAN_EACH_EFFECT",
    approvalScopePaths: [],
  };
  return {
    ref: "operation_system_grant",
    type,
    action: "UPDATE",
    title: "Доступ",
    summary: "Подтвердить доступ",
    target: {
      kind: "INTEGRATION_CONNECTION",
      ref: pins.connectionRef,
      name: "GitHub",
      version: 5,
    },
    selected: true,
    permitted: true,
    validationProblems: [],
    expectedVersion: 5,
    parameters: structuredClone(after),
    before: structuredClone(before),
    after: structuredClone(after),
  };
}

// Каждый вызов возвращает независимые массивы входного candidate.
export function integrationGrantCandidateFixture(): SystemAssistantIntegrationGrantCandidate {
  return {
    capability: {
      key: "create",
      name: "Создание",
      description: "",
      risk: "WRITE",
      approvalRequired: true,
      operation: "CREATE",
      resourceKind: "GITHUB_REPOSITORY",
      inputFields: [],
      approvalPolicy: "HUMAN_EACH_EFFECT",
      allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
    },
    grantable: true,
    reason: "READY",
    currentGrantVersion: 0,
    currentGrantEnabled: false,
    currentApprovalScopePaths: [],
  };
}

// Регистрирует одинаковые сценарии для двух явно переданных наборов authority pins.
// Expected values и запрещённые подмены не получают данные из production guards.
export function integrationGrantPlanContract<
  Page extends SystemAssistantIntegrationGrantCandidates,
>(options: GrantPlanContract<Page>): void {
  const { operation, before, candidate, page } = options;
  describe(options.label, () => {
    it("проверяет APPLIED grant по resourceRef квитанции, а не по connection locator", () => {
      const applied = {
        ...candidate,
        currentGrantRef: "grant_applied",
        currentGrantVersion: 1,
        currentGrantEnabled: true,
        currentApprovalPolicy: "HUMAN_EACH_EFFECT" as const,
      };
      const fresh = { ...page, connectionVersion: 6, items: [applied] };
      expect(
        options.appliedCandidate(operation(), fresh, applied, "grant_applied"),
      ).toBe(true);
      expect(
        options.appliedCandidate(
          operation(),
          fresh,
          applied,
          options.connectionRef,
        ),
      ).toBe(false);
      expect(
        options.appliedCandidate(
          operation(),
          fresh,
          { ...applied, currentApprovalPolicy: "NONE" },
          "grant_applied",
        ),
      ).toBe(false);
      const receipt: AssistantPlanReceipt = {
        ref: "receipt_test",
        planRef: "plan_test",
        planRevision: 2,
        outcome: "APPLIED",
        operationReceipts: [
          {
            operationRef: operation().ref,
            resourceRef: "grant_applied",
            outcome: "APPLIED",
            auditRef: "audit_test",
          },
        ],
        conflicts: [],
        auditRefs: [],
        createdResourceRefs: ["grant_applied"],
        createdAt: "2026-10-04T00:00:00Z",
      };
      const plan = {
        ref: "plan_test",
        state: "APPLIED",
        revision: 2,
        operations: [operation()],
        receipt,
      } as AssistantPlan;
      expect(options.receiptRef(plan, operation().ref)).toBe("grant_applied");
      expect(
        options.receiptRef(plan, operation().ref, {
          ...receipt,
          planRef: "foreign_plan",
        }),
      ).toBeUndefined();
      expect(
        options.receiptRef(plan, operation().ref, {
          ...receipt,
          operationReceipts: [
            ...receipt.operationReceipts,
            ...receipt.operationReceipts,
          ],
        }),
      ).toBeUndefined();
    });
    it("принимает exact readonly pins и rejects authority fields", () => {
      expect(options.ownerMatches(operation(), options.organizationRef)).toBe(
        true,
      );
      for (const patch of options.ownerPatches) {
        const current = operation();
        current.parameters = { ...current.parameters, ...patch };
        expect(options.ownerMatches(current, options.organizationRef)).toBe(
          false,
        );
      }
      const forged = operation();
      forged.target = { ...forged.target, kind: "AGENT" };
      expect(options.ownerMatches(forged, options.organizationRef)).toBe(false);
    });
    it("редактирует только policy/enabled/paths и сохраняет исходный before", () => {
      const original = operation();
      const editable = editableOperations([
        { ...original, permitted: true, validationProblems: [] },
      ])[0];
      if (!editable) throw new Error("Missing operation fixture");
      updateOperationParameter(editable, "approvalPolicy", "HUMAN_SCOPED");
      updateOperationParameter(editable, "approvalScopePaths", ["/repository"]);
      updateOperationParameter(editable, "enabled", false);
      const edited = operationInputs([editable])[0];
      expect(edited?.before).toEqual(before);
      expect(edited?.parameters.approvalPolicy).toBe("HUMAN_SCOPED");
      expect(edited?.after.approvalScopePaths).toEqual(["/repository"]);
      expect(
        edited && options.ownerMatches(edited, options.organizationRef),
      ).toBe(true);
      expect(friendlyPlanOperationType(editable)).toBe(options.expectedType);
      editable.parametersText = "invalid";
      expect(friendlyPlanOperationType(editable)).toBe(options.expectedType);
    });
    it("не лечит дрейф опубликованного candidate или текущего grant", () => {
      expect(options.candidateMatches(operation(), page, candidate)).toBe(true);
      expect(
        options.candidateMatches(
          operation(),
          { ...page, connectionVersion: 6 },
          candidate,
        ),
      ).toBe(false);
      expect(
        options.candidateMatches(operation(), page, {
          ...candidate,
          currentGrantRef: "grant_new",
          currentGrantVersion: 1,
        }),
      ).toBe(false);
      expect(
        options.candidateMatches(operation(), page, {
          ...candidate,
          currentGrantEnabled: true,
        }),
      ).toBe(false);
      expect(
        options.candidateMatches(operation(), page, {
          ...candidate,
          grantable: false,
        }),
      ).toBe(false);
      expect(
        options.candidateMatches(operation(), page, {
          ...candidate,
          capability: {
            ...candidate.capability,
            allowedApprovalPolicies: ["HUMAN_EACH_EFFECT"],
          },
        }),
      ).toBe(false);
    });
  });
}
