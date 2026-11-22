import { describe, expect, it } from "vitest";
import {
  canResolvePurgeRecovery,
  purgeRecoveryStatusKey,
} from "./purge-recovery";
import type { ProjectPurgeRecovery } from "@/shared/api/generated/openapi/types.gen";

describe("project staging blocker", () => {
  it("explains preserved eligibility without offering unknown-upload abandonment", () => {
    const value: ProjectPurgeRecovery = {
      projectRef: "prj_disposable",
      version: 1,
      snapshotDigest: "a".repeat(64),
      state: "BLOCKED",
      unknownContentCount: 2,
      cleanupPendingCount: 0,
      projectPresent: true,
      projectVersion: 1,
      canResolve: false,
      resolutionRef: "",
      reasonCode: "KUBERNETES_STAGING_RECONCILIATION_REQUIRED",
    };
    expect(purgeRecoveryStatusKey(value)).toBe(
      "projects.recovery.kubernetesStaging",
    );
    expect(canResolvePurgeRecovery(value)).toBe(false);
    const manual: ProjectPurgeRecovery = {
      ...value,
      reasonCode: "ARTIFACT_PURGE_RECONCILIATION_REQUIRED",
    };
    expect(purgeRecoveryStatusKey(manual)).toBe(
      "projects.recovery.artifactPurge",
    );
    expect(canResolvePurgeRecovery(manual)).toBe(false);
  });
});
