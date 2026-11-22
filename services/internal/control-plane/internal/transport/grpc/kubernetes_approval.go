package grpc

// kubernetesApprovalUnavailableMessage avoids leaking invalid Gate metadata or infrastructure errors.
// kubernetesApprovalUnavailableMessage не раскрывает invalid Gate metadata либо infrastructure errors.
const kubernetesApprovalUnavailableMessage = "Kubernetes child approval is unavailable"
