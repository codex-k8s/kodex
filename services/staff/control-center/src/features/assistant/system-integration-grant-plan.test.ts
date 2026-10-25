import type {
  AssistantPlanOperationInput,
  SystemAssistantIntegrationGrantCandidates,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import {
  systemIntegrationGrantPlanCandidate,
  systemIntegrationGrantPlanOwner,
  systemIntegrationGrantAppliedCandidate,
  systemIntegrationGrantReceiptRef,
} from "./system-integration-grant-plan";
import {
  integrationGrantOperationFixture,
  integrationGrantCandidateFixture,
  integrationGrantPlanContract,
} from "./integration-grant-plan.test-helpers";

const pins = {
  connectionRef: "connection_test",
  capabilityKey: "create",
  scopeKind: "ORGANIZATION",
  organizationRef: "organization_test",
  systemAssistantRef: "assistant_test",
  definitionVersion: "1.0",
  definitionDigest: "a".repeat(64),
  grantRef: "",
  grantVersion: 0,
  defaultApprovalPolicy: "HUMAN_EACH_EFFECT",
  allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
};
const before = {
  ...pins,
  enabled: false,
  approvalPolicy: "HUMAN_EACH_EFFECT",
  approvalScopePaths: [],
};
function operation(): AssistantPlanOperationInput {
  return integrationGrantOperationFixture(
    pins,
    before,
    "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
  );
}
const candidate: SystemAssistantIntegrationGrantCandidate =
  integrationGrantCandidateFixture();
const page: SystemAssistantIntegrationGrantCandidates = {
  scopeKind: "ORGANIZATION",
  organizationRef: pins.organizationRef,
  assistantRef: pins.systemAssistantRef,
  assistantVersion: 3,
  connectionRef: pins.connectionRef,
  connectionVersion: 5,
  definitionVersion: pins.definitionVersion,
  definitionDigest: pins.definitionDigest,
  items: [candidate],
  total: 1,
  nextPageToken: "",
};

integrationGrantPlanContract({
  label: "owner-confirmed SYSTEM grant plan",
  expectedType: "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
  organizationRef: pins.organizationRef,
  connectionRef: pins.connectionRef,
  before,
  operation,
  candidate,
  page,
  ownerPatches: [
    { projectRef: "project_test" },
    { assistantProfileRef: "profile_test" },
    { assistantScope: "PROJECT" },
    { organizationRef: "foreign" },
    { scopeKind: "PROJECT" },
    { systemAssistantRef: "other" },
    { grantVersion: 1 },
    { approvalPolicy: "NONE" },
  ],
  ownerMatches: systemIntegrationGrantPlanOwner,
  candidateMatches: systemIntegrationGrantPlanCandidate,
  appliedCandidate: systemIntegrationGrantAppliedCandidate,
  receiptRef: systemIntegrationGrantReceiptRef,
});
