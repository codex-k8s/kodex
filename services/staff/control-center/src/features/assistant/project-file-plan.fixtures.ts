import type {
  ArtifactRevision,
  AssistantPlan,
  AssistantPlanOperation,
} from "@/shared/api/generated/openapi/types.gen";

export function fileRevisionFixture(): ArtifactRevision {
  return {
    ref: "arv_next",
    artifactRef: "art_fixture",
    revision: 2,
    fileName: "document.md",
    mediaType: "text/markdown",
    sizeBytes: 3,
    digest: `sha256:${"b".repeat(64)}`,
    scanState: "CLEAN",
    source: "CONTROL_CENTER",
    previewAvailable: true,
    createdAt: "2026-10-10T00:00:00Z",
  };
}
export function fileOperationFixture(): AssistantPlanOperation {
  return {
    ref: "op_file",
    type: "CREATE_PROJECT_FILE_REVISION",
    action: "UPDATE",
    title: "Новая версия документа",
    summary: "Уточнить документ",
    selected: true,
    permitted: true,
    validationProblems: [],
    target: {
      kind: "ARTIFACT",
      ref: "art_fixture",
      name: "document.md",
      version: 7,
    },
    expectedVersion: 7,
    parameters: {
      artifactRef: "art_fixture",
      mediaType: "text/markdown",
      contentEncoding: "UTF8",
      contentRef: "staging-not-a-grant",
      digest: `sha256:${"b".repeat(64)}`,
      sizeBytes: 3,
    },
    before: {
      artifactRef: "art_fixture",
      currentRevisionRef: "arv_previous",
      revision: 1,
      version: 7,
      digest: `sha256:${"a".repeat(64)}`,
      fileName: "document.md",
      mediaType: "text/markdown",
      sizeBytes: 1,
      scanState: "CLEAN",
      lifecycleState: "ACTIVE",
    },
    after: {
      artifactRef: "art_fixture",
      fileName: "document.md",
      mediaType: "text/markdown",
      digest: `sha256:${"b".repeat(64)}`,
      sizeBytes: 3,
      contentRef: "staging-not-a-grant",
      previousRevisionRef: "arv_previous",
      createsImmutableRevision: true,
    },
  };
}
export function appliedFilePlanFixture(): AssistantPlan {
  return {
    ref: "pln_file",
    version: 4,
    revision: 3,
    state: "APPLIED",
    conversationRef: "cnv_file",
    projectRef: "prj_file",
    operations: [fileOperationFixture()],
    applied: true,
    auditSummary: "Обновить файл",
    contentDigest: "c".repeat(64),
    validationProblems: [],
    nextActions: [],
    receipt: {
      ref: "rcpt_file",
      planRef: "pln_file",
      planRevision: 3,
      outcome: "APPLIED",
      operationReceipts: [
        {
          operationRef: "op_file",
          resourceRef: "art_fixture",
          outcome: "APPLIED",
          auditRef: "audit_file",
          artifactRevision: fileRevisionFixture(),
        },
      ],
      conflicts: [],
      auditRefs: [],
      createdResourceRefs: [],
      createdAt: "2026-10-10T00:00:00Z",
    },
  };
}

export function requireFixture<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Required test fixture is missing");
  return value;
}
