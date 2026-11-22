package httptransport

import (
	"slices"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// validKubernetesInputHTTPRef checks syntax only; the owner resolves current actor/root/stream eligibility before any read or OCC lookup.
// validKubernetesInputHTTPRef проверяет лишь syntax; current actor/root/stream eligibility owner разрешает до read либо OCC lookup.
func validKubernetesInputHTTPRef(ref, prefix string) bool {
	return len(ref) >= len(prefix)+8 && len(ref) <= 96 && strings.HasPrefix(ref, prefix) && opaqueHTTPReference.MatchString(ref)
}

// validKubernetesInputHTTPToken bounds opaque pagination without interpreting it as a stream or authorization selector.
// validKubernetesInputHTTPToken ограничивает opaque pagination без трактовки как stream либо authorization selector.
func validKubernetesInputHTTPToken(token string) bool {
	return len(token) <= 2048 && utf8.ValidString(token) && !strings.ContainsAny(token, "\x00\r\n")
}

// validKubernetesInputHTTPKey preserves the published exact idempotency alphabet without whitespace trimming or case folding.
// validKubernetesInputHTTPKey сохраняет published exact idempotency alphabet без whitespace trimming либо case folding.
func validKubernetesInputHTTPKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == ':' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// kubernetesInputPublicSubmission rejects mixed nested origins and unknown states before metadata is exposed; it contains no payload or storage locator.
// kubernetesInputPublicSubmission отвергает mixed nested origins и unknown states до выдачи metadata; payload либо storage locator отсутствуют.
func kubernetesInputPublicSubmission(input *cp.ManagedKubernetesStreamInputSubmission, view generated.KubernetesStreamInputView) (*generated.KubernetesStreamInputSubmission, bool) {
	return kubernetesInputPublicSubmissionTuple(input, view.InvocationRef, view.StreamRef, view.Generation, uint64(view.MaximumFrameBytes), view.InputChannels)
}

// kubernetesInputPublicSubmissionTuple validates only immutable metadata against explicit parent coordinates, never a fabricated owner view or effect credit.
// kubernetesInputPublicSubmissionTuple проверяет лишь immutable metadata по explicit parent coordinates, не создавая fake owner view либо effect credit.
func kubernetesInputPublicSubmissionTuple(input *cp.ManagedKubernetesStreamInputSubmission, invocation, stream string, generation int64, maximum uint64, channels []generated.KubernetesStreamInputChannel) (*generated.KubernetesStreamInputSubmission, bool) {
	if input == nil {
		return nil, true
	}
	state := generated.KubernetesStreamInputSubmissionState(strings.TrimPrefix(input.State.String(), "MANAGED_KUBERNETES_STREAM_INPUT_SUBMISSION_STATE_"))
	channel := generated.KubernetesStreamInputChannel(input.Channel)
	if len(input.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInputHTTPRef(input.Ref, "kif_") || input.InvocationRef != invocation || input.StreamRef != stream || input.Generation != generation || !managedkubernetes.ValidFrameSequence(input.Sequence) || input.Sequence > uint64(maximumSafeJSONInteger) || !channel.Valid() || !slices.Contains(channels, channel) || !state.Valid() || !validManagedDigest(input.PayloadDigest) || uint64(input.ByteCount) > maximum || input.ByteCount == 0 && !input.EndOfInput || input.ByteCount != 0 && input.EndOfInput {
		return nil, false
	}
	return &generated.KubernetesStreamInputSubmission{Ref: input.Ref, InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, Generation: input.Generation, Sequence: int64(input.Sequence), Channel: channel, PayloadDigest: input.PayloadDigest, ByteCount: int64(input.ByteCount), EndOfInput: input.EndOfInput, State: state}, true
}

// kubernetesInputPublicView keeps safe current coordinates separate from fresh effect authority and preserves historical owner deadlines unchanged.
// kubernetesInputPublicView отделяет safe current coordinates от fresh effect authority и сохраняет historical owner deadlines без изменений.
func kubernetesInputPublicView(input *cp.ManagedKubernetesStreamInputView) (generated.KubernetesStreamInputView, bool) {
	var empty generated.KubernetesStreamInputView
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInputHTTPRef(input.InvocationRef, "inv_") || !validKubernetesInputHTTPRef(input.StreamRef, "kst_") || !validKubernetesInputHTTPRef(input.RunRef, "run_") || !validKubernetesInputHTTPRef(input.NodeRef, "nod_") || !validManagedVersion(input.Generation) || !validManagedVersion(input.Version) || input.Resource == nil || len(input.Resource.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(input.TargetRef) || len(input.TargetRef) > 96 || !validManagedVersion(input.TargetRevision) || !validKubernetesInputHTTPRef(input.ConnectionRef, "int_") || input.MaximumStagedBytes == 0 || input.MaximumStagedBytes > managedkubernetes.MaximumStagedBytes || input.MaximumStagedObjects == 0 || input.MaximumStagedObjects > managedkubernetes.MaximumStagedObjects || input.MaximumFrameBytes == 0 || input.MaximumFrameBytes > managedkubernetes.MaximumFrameBytes || input.MaximumTotalBytes < uint64(input.MaximumFrameBytes) || input.MaximumTotalBytes > managedkubernetes.MaximumStreamBytes || input.AcknowledgedSequence > uint64(maximumSafeJSONInteger) || len(input.InputChannels) > 7 || len(input.ClosedInputChannels) > 7 || input.HardDeadline == nil || input.HardDeadline.CheckValid() != nil || len(input.HardDeadline.ProtoReflect().GetUnknown()) != 0 || input.ReasonCode != "" && !managedkubernetes.ValidReasonCode(input.ReasonCode) {
		return empty, false
	}
	address := managedkubernetes.ResourceAddress{Group: input.Resource.Group, Version: input.Resource.Version, Resource: input.Resource.Resource, Namespace: input.Resource.Namespace, Name: input.Resource.Name, UID: input.Resource.Uid, ResourceVersion: input.Resource.ResourceVersion, Subresource: input.Resource.Subresource, Namespaced: input.Resource.Namespaced, AllNamespaces: input.Resource.AllNamespaces}
	if managedkubernetes.ValidateStreamInputViewResource(input.Operation, input.Namespace, input.PodName, input.Container, address) != nil || (input.Operation == "kubernetes.watch" || input.Operation == "kubernetes.watch.all-namespaces") && len(input.InputChannels) != 0 {
		return empty, false
	}
	operation, known := integrationpackage.KubernetesOperationByKey(input.Operation)
	if !known || operation.Execution != integrationpackage.KubernetesStream {
		return empty, false
	}
	state := generated.KubernetesStreamInputViewState(strings.TrimPrefix(input.State.String(), "MANAGED_KUBERNETES_OWNER_STREAM_STATE_"))
	if !state.Valid() {
		return empty, false
	}
	result := generated.KubernetesStreamInputView{Resource: generated.KubernetesOwnerGateResource{Group: address.Group, Version: address.Version, Resource: address.Resource, Namespace: address.Namespace, Name: address.Name, Uid: address.UID, ResourceVersion: address.ResourceVersion, Subresource: address.Subresource, Namespaced: address.Namespaced, AllNamespaces: address.AllNamespaces}, TargetRef: input.TargetRef, TargetRevision: input.TargetRevision, ConnectionRef: input.ConnectionRef, InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, RunRef: input.RunRef, NodeRef: input.NodeRef, Generation: input.Generation, Version: input.Version, Operation: input.Operation, Namespace: input.Namespace, PodName: input.PodName, Container: input.Container, State: state, InputChannels: make([]generated.KubernetesStreamInputChannel, 0, len(input.InputChannels)), ClosedInputChannels: make([]generated.KubernetesStreamInputChannel, 0, len(input.ClosedInputChannels)), MaximumFrameBytes: int64(input.MaximumFrameBytes), MaximumTotalBytes: int64(input.MaximumTotalBytes), MaximumStagedBytes: int64(input.MaximumStagedBytes), MaximumStagedObjects: int64(input.MaximumStagedObjects), AcknowledgedSequence: int64(input.AcknowledgedSequence), HardDeadline: input.HardDeadline.AsTime(), ReasonCode: input.ReasonCode}
	for _, raw := range input.InputChannels {
		channel := generated.KubernetesStreamInputChannel(raw)
		if !channel.Valid() || slices.Contains(result.InputChannels, channel) {
			return empty, false
		}
		result.InputChannels = append(result.InputChannels, channel)
	}
	for _, raw := range input.ClosedInputChannels {
		channel := generated.KubernetesStreamInputChannel(raw)
		if !slices.Contains(result.InputChannels, channel) || slices.Contains(result.ClosedInputChannels, channel) {
			return empty, false
		}
		result.ClosedInputChannels = append(result.ClosedInputChannels, channel)
	}
	pending, ok := kubernetesInputPublicSubmission(input.Pending, result)
	if !ok {
		return empty, false
	}
	result.Pending = pending
	return result, true
}

// kubernetesInputPublicReadback binds normal and lost-ACK metadata to the exact requested invocation/stream, never a scalar-first active child.
// kubernetesInputPublicReadback связывает normal и lost-ACK metadata с exact requested invocation/stream без scalar-first active child.
func kubernetesInputPublicReadback(view *cp.ManagedKubernetesStreamInputView, submission *cp.ManagedKubernetesStreamInputSubmission, invocation, stream string) (generated.KubernetesStreamInputReadback, bool) {
	var empty generated.KubernetesStreamInputReadback
	mapped, ok := kubernetesInputPublicView(view)
	if !ok || mapped.InvocationRef != invocation || mapped.StreamRef != stream {
		return empty, false
	}
	result, ok := kubernetesInputPublicSubmission(submission, mapped)
	if !ok {
		return empty, false
	}
	return generated.KubernetesStreamInputReadback{View: mapped, Submission: result}, true
}
