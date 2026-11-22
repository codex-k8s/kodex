package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// kubernetesArtifactCaptureToProto preserves the exact safe provenance without inferring native
// success or target authority from an artifact. Private subject/object/credential scopes are absent.
// kubernetesArtifactCaptureToProto сохраняет exact safe provenance, не выводя native success либо
// target authority из artifact. Private subject/object/credential scopes отсутствуют.
func kubernetesArtifactCaptureToProto(value *entity.KubernetesArtifactCaptureOrigin) *cp.KubernetesArtifactCaptureOrigin {
	if value == nil {
		return nil
	}
	kind := cp.KubernetesArtifactCaptureKind_KUBERNETES_ARTIFACT_CAPTURE_KIND_UNSPECIFIED
	switch value.Kind {
	case "FULL":
		kind = cp.KubernetesArtifactCaptureKind_KUBERNETES_ARTIFACT_CAPTURE_KIND_FULL
	case "PARTIAL":
		kind = cp.KubernetesArtifactCaptureKind_KUBERNETES_ARTIFACT_CAPTURE_KIND_PARTIAL
	default:
		// Unknown provenance remains an explicit invalid wire variant, never an ordinary artifact.
		// Unknown provenance остаётся explicit invalid wire variant, не ordinary artifact.
	}
	resource := value.Resource
	return &cp.KubernetesArtifactCaptureOrigin{InvocationRef: value.InvocationRef, StreamRef: value.StreamRef, StreamGeneration: value.StreamGeneration, CaptureGeneration: value.CaptureGeneration, Operation: value.Operation, Channel: value.Channel, Kind: kind, AcknowledgedPrefix: value.AcknowledgedPrefix, OutputWatermark: value.OutputWatermark, DroppedBytes: value.DroppedBytes, NativeTerminalKnown: value.NativeTerminalKnown, TransportJoined: value.TransportJoined, OwnerState: value.OwnerState, LocalState: value.LocalState, RemoteOutcome: value.RemoteOutcome, ExitCode: value.ExitCode, Resource: &cp.KubernetesOwnerGateResource{Group: resource.Group, Version: resource.Version, Resource: resource.Resource, Namespace: resource.Namespace, Name: resource.Name, Uid: resource.UID, ResourceVersion: resource.ResourceVersion, Subresource: resource.Subresource, Namespaced: resource.Namespaced, AllNamespaces: resource.AllNamespaces}, TargetRef: value.TargetRef, TargetRevision: value.TargetRevision, ConnectionRef: value.ConnectionRef, SourceDigest: value.SourceDigest, ReasonCode: value.ReasonCode, SealedAt: timestamp(value.SealedAt)}
}
