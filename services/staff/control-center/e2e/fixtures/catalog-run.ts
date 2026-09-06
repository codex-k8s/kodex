import type { Run } from "../../src/shared/api/generated/openapi/types.gen";
export function syntheticCatalogRun(index: number, projectRef: string): Run {
  const ref = `run_catalog_${String(index)}`;
  return {
    ref,
    version: 1,
    projectRef,
    sessionRef: `session_${ref}`,
    rootRunRef: ref,
    target: {
      type: "AGENT",
      ref: "agent_synthetic",
      displayName: "Synthetic executor with a long display name",
      version: 1,
    },
    title: `${String(index)} Synthetic long run title `.repeat(5),
    titleSource: "SERVER_DEFAULT",
    activitySummary: "Synthetic activity",
    state: "QUEUED",
    source: "CONTROL_CENTER",
    initiator: {
      ref: "subject_synthetic",
      displayName: "Synthetic initiator with a long display name",
    },
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: 1,
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
    artifactRefs: [],
    gateRefs: [],
    createdAt: "2026-09-05T00:00:00Z",
    nextActions: [],
  };
}
