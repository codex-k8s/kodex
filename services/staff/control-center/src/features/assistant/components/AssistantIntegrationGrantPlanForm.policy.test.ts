import { defineComponent, type Ref, type SetupContext } from "vue";
import { createI18n } from "vue-i18n";
import { expect, it, vi } from "vitest";
import type {
  Agent,
  AssistantPlanOperationInput,
  IntegrationConnection,
  IntegrationGrantCapabilityCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import { editableOperations } from "../model";
import { captureSetupState } from "@/test-utils/setup-harness";
import Form from "./AssistantIntegrationGrantPlanForm.vue";

const capability = {
  key: "write",
  name: "Запись",
  description: "",
  risk: "WRITE" as const,
  approvalRequired: true,
  operation: "CREATE" as const,
  resourceKind: "GITHUB_REPOSITORY" as const,
  inputFields: [],
  approvalPolicy: "HUMAN_EACH_EFFECT" as const,
  allowedApprovalPolicies: [
    "HUMAN_EACH_EFFECT" as const,
    "HUMAN_SCOPED" as const,
  ],
  inputSchema:
    '{"type":"object","properties":{"repository":{"type":"string"}}}',
};
async function setup(policy?: string) {
  const input: AssistantPlanOperationInput = {
    ref: "operation_test",
    type: "CHANGE_INTEGRATION_GRANT",
    action: "UPDATE",
    target: {
      kind: "INTEGRATION_CONNECTION",
      ref: "connection_test",
      version: 5,
      name: "GitHub",
    },
    expectedVersion: 5,
    selected: true,
    permitted: true,
    validationProblems: [],
    title: "Доступ",
    summary: "Доступ",
    parameters: {
      connectionRef: "connection_test",
      agentRef: "agent_test",
      capabilityKey: "write",
      enabled: false,
      ...(policy ? { approvalPolicy: policy } : {}),
      approvalScopePaths: ["/repository"],
    },
    before: {},
    after: {},
  };
  const operation = editableOperations([
    { ...input, permitted: true, validationProblems: [] },
  ])[0];
  const emit = vi.fn();
  const original = (
    Form as unknown as {
      setup: (props: Record<string, unknown>, context: SetupContext) => object;
    }
  ).setup;
  const state = (await captureSetupState(
    defineComponent({
      setup(_props, context) {
        return original({ operation, disabled: false }, { ...context, emit });
      },
    }),
    (app) => app.use(createI18n({ legacy: false, locale: "ru" })),
  )) as unknown as {
    connection: Ref<IntegrationConnection | undefined>;
    recipient: Ref<Agent | undefined>;
    candidate: Ref<IntegrationGrantCapabilityCandidate | undefined>;
    selectedApprovalPolicy: Ref<string | undefined>;
    approvalScopeValid: Ref<boolean>;
    setEnabled(value: boolean): void;
    chooseApprovalPolicy(value: string): void;
  };
  state.connection.value = {
    ref: "connection_test",
    version: 5,
    name: "GitHub",
    definitionKey: "github",
    definitionVersion: "1.0",
    definitionDigest: "a".repeat(64),
    state: "CONNECTED",
    credentialsConfigured: true,
    credentialsHint: "",
    grants: [],
    nextActions: [],
    publicConfiguration: {},
    capabilities: [capability],
  };
  state.candidate.value = {
    capability,
    grantable: true,
    reason: "READY",
    pins: { connectionVersion: 5, contextDigest: "a".repeat(64) },
  };
  emit.mockClear();
  return { state, emit };
}
it("не подставляет default policy в исторический проектный plan без explicit field", async () => {
  const { state } = await setup();
  expect(state.selectedApprovalPolicy.value).toBeUndefined();
  expect(state.approvalScopeValid.value).toBe(false);
});
it("сохраняет policy и paths при revoke и не разрешает unknown policy", async () => {
  const { state, emit } = await setup("HUMAN_SCOPED");
  expect(state.selectedApprovalPolicy.value).toBe("HUMAN_SCOPED");
  expect(state.approvalScopeValid.value).toBe(true);
  state.setEnabled(false);
  expect(emit).toHaveBeenCalledWith("parameter", "enabled", false);
  expect(emit).not.toHaveBeenCalledWith("parameter", "approvalScopePaths", []);
  emit.mockClear();
  state.chooseApprovalPolicy("NONE");
  expect(emit).not.toHaveBeenCalled();
  state.chooseApprovalPolicy("HUMAN_EACH_EFFECT");
  expect(emit).toHaveBeenCalledWith(
    "parameter",
    "approvalPolicy",
    "HUMAN_EACH_EFFECT",
  );
  expect(emit).toHaveBeenCalledWith("parameter", "approvalScopePaths", []);
});
