import { beforeEach, describe, expect, it, vi } from "vitest";
import { readProjectAssistant, readProjectAssistantAgent } from "./api";
import { readAssistantProjectHelper } from "./project-helper-readback";

import {
  assistantAgentEnvironmentBindingTarget,
  assistantEnvironmentDraftTarget,
  assistantInstructionDraftTarget,
  assistantOperationProjectRef,
  assistantProjectHelperScope,
  editableAssistantOperationProjectRef,
  editableOperations,
  updateOperationParameter,
} from "./model";
import type {
  AssistantPlan,
  AssistantPlanOperation,
} from "@/shared/api/generated/openapi/types.gen";

const org = "org_fixture";
const project = "prj_fixture";
const agent = "agt_fixture";
const profile = "asstp_fixture";
vi.mock("./api", () => ({
  readProjectAssistant: vi.fn(),
  readProjectAssistantAgent: vi.fn(),
}));
const types = [
  "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
  "CREATE_INSTRUCTION_DRAFT",
  "BIND_AGENT_RUNTIME_ENVIRONMENT",
] as const;

function operation(type: (typeof types)[number]): AssistantPlanOperation {
  const pins = {
    projectAssistantRef: agent,
    assistantScope: "PROJECT",
    scopeKind: "PROJECT",
    organizationRef: org,
    projectRef: project,
    assistantProfileRef: profile,
    agentVersion: 3,
    runtimeEnvironmentBindingRef: "envbind_fixture",
    runtimeEnvironmentVersionRef: "envver_fixture",
    runtimeEnvironmentDigest: "a".repeat(64),
  };
  const fields =
    type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
      ? { environmentRef: "env_fixture", name: "Среда" }
      : type === "CREATE_INSTRUCTION_DRAFT"
        ? { agentRef: agent, instructions: "Понятные инструкции помощника" }
        : {
            agentRef: agent,
            environmentRef: "env_fixture",
            versionRef: "envver_fixture",
          };
  return {
    ref: "op_fixture",
    type,
    action: "UPDATE",
    title: "Настройка проектного помощника",
    summary: "Только после подтверждения",
    selected: true,
    permitted: true,
    validationProblems: [],
    expectedVersion: type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION" ? 5 : 3,
    target: {
      name: "Помощник проекта",
      kind:
        type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
          ? "ENVIRONMENT"
          : "AGENT",
      ref:
        type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION" ? "env_fixture" : agent,
      version: type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION" ? 5 : 3,
    },
    parameters: { ...pins, ...fields },
    before: { ...pins, ...fields },
    after: { ...pins, ...fields },
  };
}
function plan(value: AssistantPlanOperation): AssistantPlan {
  return {
    ref: "plan_fixture",
    conversationRef: "conv_fixture",
    revision: 2,
    version: 3,
    state: "APPLIED",
    applied: true,
    contentDigest: "a".repeat(64),
    auditSummary: "Настройка",
    operations: [value],
    validationProblems: [],
    nextActions: [],
    receipt: {
      ref: "receipt_fixture",
      planRef: "plan_fixture",
      planRevision: 2,
      outcome: "APPLIED",
      operationReceipts: [
        {
          operationRef: value.ref,
          resourceRef:
            value.type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
              ? "envdraft_fixture"
              : agent,
          outcome: "APPLIED",
          auditRef: "audit_fixture",
        },
      ],
      conflicts: [],
      auditRefs: ["audit_fixture"],
      createdResourceRefs: [],
      createdAt: "2026-10-04T00:00:00Z",
    },
  };
}

describe("целевая область проектного помощника в общесистемном плане", () => {
  it.each(types)(
    "%s сохраняет область плана и проверяет полные закрепления",
    (type) => {
      const value = operation(type);
      const source = plan(value);
      expect(assistantProjectHelperScope(source, value, org)).toEqual({
        projectRef: project,
        agentRef: agent,
        profileRef: profile,
      });
      expect(assistantOperationProjectRef(source, value, org)).toBe(project);
      expect(source.projectRef).toBeUndefined();
      const [editable] = editableOperations([value]);
      expect(editable).toBeDefined();
      if (!editable) return;
      updateOperationParameter(
        editable,
        type === "CREATE_INSTRUCTION_DRAFT" ? "instructions" : "name",
        "Новый текст настройки",
      );
      expect(editableAssistantOperationProjectRef(source, editable, org)).toBe(
        project,
      );
    },
  );
  it.each(types)(
    "%s закрыто отклоняет неверного владельца, версию и каждый snapshot",
    (type) => {
      const original = operation(type);
      expect(
        assistantOperationProjectRef(plan(original), original),
      ).toBeUndefined();
      expect(
        assistantOperationProjectRef(plan(original), original, "org_foreign"),
      ).toBeUndefined();
      for (const snapshot of ["parameters", "before", "after"] as const)
        for (const key of Object.keys(original.parameters).filter((key) =>
          [
            "projectAssistantRef",
            "assistantScope",
            "scopeKind",
            "organizationRef",
            "projectRef",
            "assistantProfileRef",
            "agentVersion",
            "runtimeEnvironmentBindingRef",
            "runtimeEnvironmentVersionRef",
            "runtimeEnvironmentDigest",
          ].includes(key),
        )) {
          const value = structuredClone(original);
          Reflect.deleteProperty(value[snapshot], key);
          expect(
            assistantOperationProjectRef({ projectRef: project }, value, org),
          ).toBeUndefined();
        }
      for (const key of ["ref", "kind", "version"] as const) {
        const value = structuredClone(original);
        Reflect.deleteProperty(value.target, key);
        expect(
          assistantOperationProjectRef(plan(value), value, org),
        ).toBeUndefined();
      }
      const [editable] = editableOperations([original]);
      if (!editable) return;
      updateOperationParameter(editable, "projectRef", "prj_foreign");
      expect(
        editableAssistantOperationProjectRef(plan(original), editable, org),
      ).toBeUndefined();
      editable.parametersText = "{}";
      editable.beforeText = "{}";
      editable.afterText = "{}";
      expect(
        editableAssistantOperationProjectRef(plan(original), editable, org),
      ).toBeUndefined();
    },
  );
  it("malformed locator не превращается в обычную операцию проекта", () => {
    for (const locator of ["", null, false, 0, "unknown"]) {
      const value = operation("CREATE_INSTRUCTION_DRAFT");
      value.parameters = { projectAssistantRef: locator };
      value.before = {};
      value.after = {};
      expect(
        assistantOperationProjectRef({ projectRef: project }, value, org),
      ).toBeUndefined();
    }
    const value = operation("CREATE_INSTRUCTION_DRAFT");
    value.type = "UPDATE_AGENT";
    expect(
      assistantOperationProjectRef({ projectRef: project }, value, org),
    ).toBeUndefined();
  });
  it("SYSTEM из проекта A настраивает helper B, не меняя проект истории", () => {
    const value = operation("CREATE_INSTRUCTION_DRAFT");
    const source = { projectRef: "prj_source_context" };
    expect(assistantOperationProjectRef(source, value, org)).toBe(project);
    expect(source.projectRef).toBe("prj_source_context");
  });
  it("обычные операции используют только projectRef плана", () => {
    const value = operation("CREATE_INSTRUCTION_DRAFT");
    value.parameters = { agentRef: agent, instructions: "Инструкция" };
    value.before = { agentRef: agent };
    value.after = { agentRef: agent };
    expect(assistantOperationProjectRef({ projectRef: project }, value)).toBe(
      project,
    );
    expect(assistantOperationProjectRef({}, value, org)).toBeUndefined();
  });
  it.each(types)(
    "%s использует только точную квитанцию выбранной операции",
    (type) => {
      const value = operation(type);
      const source = plan(value);
      const target = () =>
        type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
          ? assistantEnvironmentDraftTarget(source, value.ref, org)
          : type === "CREATE_INSTRUCTION_DRAFT"
            ? assistantInstructionDraftTarget(source, value.ref, org)
            : assistantAgentEnvironmentBindingTarget(source, value.ref, org);
      expect(target()?.projectRef).toBe(project);
      value.selected = false;
      expect(target()).toBeUndefined();
      value.selected = true;
      if (!source.receipt) return;
      source.receipt.planRevision = 1;
      expect(target()).toBeUndefined();
      source.receipt.planRevision = 2;
      const receipt = source.receipt.operationReceipts[0];
      if (!receipt) throw new Error("Missing synthetic operation receipt");
      receipt.resourceRef = "agt_foreign";
      if (type !== "PREPARE_RUNTIME_ENVIRONMENT_REVISION")
        expect(target()).toBeUndefined();
    },
  );
});

describe("авторитетное чтение помощника перед переходом из квитанции", () => {
  beforeEach(() => vi.clearAllMocks());
  const currentProfile = {
    ref: profile,
    projectRef: project,
    agentRef: agent,
    name: "Помощник проекта",
    state: "ACTIVE" as const,
    version: 1,
    createdAt: "2026-10-04T00:00:00Z",
    updatedAt: "2026-10-04T00:00:00Z",
  };
  it("сначала проверяет exact профиль и затем использует только его actual agentRef", async () => {
    vi.mocked(readProjectAssistant).mockResolvedValue(currentProfile);
    const value = operation("CREATE_INSTRUCTION_DRAFT");
    const signal = new AbortController().signal;
    await readAssistantProjectHelper(plan(value), value, org, signal);
    expect(readProjectAssistant).toHaveBeenCalledWith(project, signal);
    expect(readProjectAssistantAgent).toHaveBeenCalledWith(
      currentProfile,
      signal,
    );
  });
  it.each(["ref", "projectRef", "agentRef"] as const)(
    "не читает сотрудника при чужом %s профиля",
    async (key) => {
      vi.mocked(readProjectAssistant).mockResolvedValue({
        ...currentProfile,
        [key]: "foreign_fixture",
      });
      const value = operation("CREATE_INSTRUCTION_DRAFT");
      await expect(
        readAssistantProjectHelper(
          plan(value),
          value,
          org,
          new AbortController().signal,
        ),
      ).rejects.toThrow("profile readback mismatch");
      expect(readProjectAssistantAgent).not.toHaveBeenCalled();
    },
  );
  it("не выполняет HTTP при missing/foreign bootstrap org и malformed locator", async () => {
    const value = operation("CREATE_INSTRUCTION_DRAFT");
    const signal = new AbortController().signal;
    await expect(
      readAssistantProjectHelper(plan(value), value, undefined, signal),
    ).rejects.toThrow("owner pins mismatch");
    await expect(
      readAssistantProjectHelper(plan(value), value, "org_foreign", signal),
    ).rejects.toThrow("owner pins mismatch");
    value.before.projectAssistantRef = null;
    await expect(
      readAssistantProjectHelper(plan(value), value, org, signal),
    ).rejects.toThrow("owner pins mismatch");
    expect(readProjectAssistant).not.toHaveBeenCalled();
    expect(readProjectAssistantAgent).not.toHaveBeenCalled();
  });
});
