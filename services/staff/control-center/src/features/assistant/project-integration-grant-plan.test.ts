import type {
  AssistantPlanOperationInput,
  ProjectAssistantIntegrationGrantCandidates,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import {
  projectIntegrationGrantPlanCandidate,
  projectIntegrationGrantPlanOwner,
  projectIntegrationGrantAppliedCandidate,
  projectIntegrationGrantReceiptRef,
} from "./project-integration-grant-plan";
import {
  integrationGrantOperationFixture,
  integrationGrantCandidateFixture,
  integrationGrantPlanContract,
} from "./integration-grant-plan.test-helpers";

const pins = {
  connectionRef: "connection_test",
  capabilityKey: "create",
  scopeKind: "ORGANIZATION",
  assistantScope: "PROJECT",
  projectRef: "project_test",
  assistantProfileRef: "profile_test",
  agentVersion: 3,
  profileVersion: 1,
  organizationRef: "organization_test",
  projectAssistantRef: "assistant_test",
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
    "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
  );
}
const candidate: SystemAssistantIntegrationGrantCandidate =
  integrationGrantCandidateFixture();
const page: ProjectAssistantIntegrationGrantCandidates = {
  scopeKind: "ORGANIZATION",
  organizationRef: pins.organizationRef,
  projectRef: pins.projectRef,
  assistantProfileRef: pins.assistantProfileRef,
  assistantRef: pins.projectAssistantRef,
  assistantVersion: 3,
  profileVersion: 1,
  connectionRef: pins.connectionRef,
  connectionVersion: 5,
  definitionVersion: pins.definitionVersion,
  definitionDigest: pins.definitionDigest,
  items: [candidate],
  total: 1,
  nextPageToken: "",
};

integrationGrantPlanContract({
  label: "owner-confirmed PROJECT grant plan",
  expectedType: "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
  organizationRef: pins.organizationRef,
  connectionRef: pins.connectionRef,
  before,
  operation,
  candidate,
  page,
  ownerPatches: [
    { projectRef: "foreign_project" },
    { assistantProfileRef: "foreign_profile" },
    { assistantScope: "SYSTEM" },
    { organizationRef: "foreign" },
    { scopeKind: "PROJECT" },
    { projectAssistantRef: "other" },
    { grantVersion: 1 },
    { approvalPolicy: "NONE" },
  ],
  ownerMatches: projectIntegrationGrantPlanOwner,
  candidateMatches: projectIntegrationGrantPlanCandidate,
  appliedCandidate: projectIntegrationGrantAppliedCandidate,
  receiptRef: projectIntegrationGrantReceiptRef,
});
