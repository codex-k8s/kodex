import type {
  KubernetesStreamInputView as Input,
  KubernetesPortForwardConnectionView as Connection,
  KubernetesStreamInputSubmission as Submission,
  KubernetesPortForwardConnectionPage as Page,
} from "./generated/openapi/types.gen";
export function input(): Input {
  return {
    invocationRef: "inv_synthetic",
    streamRef: "kst_synthetic",
    runRef: "run_synthetic",
    nodeRef: "nod_synthetic",
    generation: 7,
    operation: "kubernetes.port-forward",
    namespace: "example",
    podName: "web",
    container: "app",
    state: "ACTIVE",
    version: 4,
    inputChannels: ["PORT_FORWARD"],
    closedInputChannels: [],
    maximumFrameBytes: 32768,
    maximumTotalBytes: 65536,
    maximumStagedBytes: 1073741824,
    maximumStagedObjects: 32768,
    acknowledgedSequence: 0,
    hardDeadline: "2099-11-15T12:00:00Z",
    reasonCode: "",
    targetRef: "target_review",
    targetRevision: 2,
    connectionRef: "int_synthetic",
    resource: {
      group: "",
      version: "v1",
      resource: "pods",
      namespace: "example",
      name: "web",
      uid: "30000000-0000-4000-8000-000000000001",
      resourceVersion: "18",
      subresource: "",
      namespaced: true,
      allNamespaces: false,
    },
  };
}
export function connection(): Connection {
  const view = input();
  return {
    connectionRef: `kpf_${"A".repeat(24)}`,
    invocationRef: view.invocationRef,
    streamRef: view.streamRef,
    streamGeneration: view.generation,
    generation: 3,
    slot: 0,
    state: "OPEN",
    inputVersion: 4,
    localWriteClosed: false,
    readEndObserved: false,
    nativeJoined: false,
    hardDeadline: view.hardDeadline,
    reasonCode: "",
  };
}
export function submission(): Submission {
  const view = input();
  return {
    ref: "kif_synthetic",
    invocationRef: view.invocationRef,
    streamRef: view.streamRef,
    generation: 7,
    sequence: 1,
    channel: "PORT_FORWARD",
    payloadDigest: "a".repeat(64),
    byteCount: 51,
    endOfInput: false,
    state: "QUEUED",
  };
}
export function page(): Page {
  return {
    items: [connection()],
    nextPageToken: "",
    profile: {
      protocol: "PORT_FORWARD_SPDY_V1",
      maximumActiveConnections: 16,
      activeConnections: 1,
      admissionReasonCode: "",
      ports: [{ port: 18080, targetPort: 8080 }],
      timeoutSeconds: 120,
    },
  };
}
