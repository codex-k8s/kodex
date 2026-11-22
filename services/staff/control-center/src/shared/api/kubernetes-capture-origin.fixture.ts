import type { KubernetesArtifactCaptureOrigin } from "./generated/openapi/types.gen";

/** Синтетический partial capture с подтверждённым удалённым exit 0. */
export function captureOriginFixture(): KubernetesArtifactCaptureOrigin {
  return {
    invocationRef: "inv_fixture123",
    streamRef: "kst_fixture123",
    streamGeneration: 1,
    captureGeneration: 2,
    operation: "kubernetes.exec",
    channel: "STDOUT",
    kind: "PARTIAL",
    acknowledgedPrefix: "1",
    outputWatermark: "2",
    droppedBytes: "0",
    nativeTerminalKnown: true,
    transportJoined: true,
    ownerState: "LOST",
    localState: "LOST",
    remoteOutcome: "SUCCEEDED",
    exitCode: 0,
    resource: {
      group: "",
      version: "v1",
      resource: "pods",
      namespace: "review",
      name: "web",
      uid: "30000000-0000-4000-8000-000000000001",
      resourceVersion: "17",
      subresource: "",
      namespaced: true,
      allNamespaces: false,
    },
    targetRef: "operator/target",
    targetRevision: 1,
    connectionRef: "int_fixture123",
    sourceDigest: "a".repeat(64),
    reasonCode: "OUTPUT_LOST",
    sealedAt: "2026-11-16T01:00:00Z",
  };
}
