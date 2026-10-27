import { expect, it } from "vitest";
import type { RunNode } from "@/shared/api/generated/openapi/types.gen";
import { validWorkflowAttemptEvidence } from "./workflow-evidence";

it("сохраняет owner ARTIFACT_REVIEW facts и effectiveState без private provenance", () => {
  const node = {
    workflowResults: [
      {
        attempt: 1,
        runRef: "run_12345678",
        accepted: false,
        reason: "CHANGES_REQUIRED",
        results: [],
        effectiveState: "REWORK_PENDING",
        evidence: [
          {
            key: "design",
            kind: "ARTIFACT_REVIEW",
            artifactRef: "art_12345678",
            artifactRevision: "arev_12345678",
            digest: "a".repeat(64),
            verdict: "CHANGES_REQUIRED",
          },
          {
            key: "comment",
            kind: "REVIEW",
            receiptRef: "erc_12345678",
            repository: "owner/repo",
            number: 42,
            headSha: "b".repeat(40),
            digest: "c".repeat(64),
            verdict: "PASS",
          },
        ],
      },
    ],
  } as unknown as RunNode;
  expect(validWorkflowAttemptEvidence(node)).toBe(true);
  for (const fault of [
    "accepted",
    "private",
    "state",
    "state-object",
    "missing-artifact",
    "missing-comment",
  ] as const) {
    const changed = structuredClone(node);
    const attempt = changed.workflowResults?.[0];
    if (!attempt) throw new Error("Missing fixture attempt");
    if (fault === "accepted") attempt.accepted = true;
    if (fault === "private") Object.assign(attempt, { reworkSubjects: [] });
    if (fault === "state")
      Object.assign(attempt, { effectiveState: "UNKNOWN" });
    if (fault === "state-object")
      Object.assign(attempt, { effectiveState: {} });
    if (fault === "missing-artifact" && attempt.evidence?.[0])
      delete attempt.evidence[0].artifactRevision;
    if (fault === "missing-comment" && attempt.evidence?.[1])
      delete attempt.evidence[1].receiptRef;
    expect(validWorkflowAttemptEvidence(changed), fault).toBe(false);
  }
});
