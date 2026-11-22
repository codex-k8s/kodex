package managedkubernetes

import (
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
	"math"
	"time"
)

// ArtifactCaptureOrigin describes only the captured owner-ACK prefix of one immutable
// revision. PARTIAL, missing native evidence and unknown remote outcomes never imply remote stop.
// It carries no credential locator, identity scope, object key or permission to execute another tool.
// ArtifactCaptureOrigin описывает только captured owner-ACK prefix immutable revision.
// PARTIAL, missing native evidence и unknown remote outcomes не означают remote stop.
// Не передаёт credential locator, identity scope, object key либо permission для следующего tool.
type ArtifactCaptureOrigin struct {
	InvocationRef       string          `json:"invocationRef"`
	StreamRef           string          `json:"streamRef"`
	StreamGeneration    int64           `json:"streamGeneration"`
	CaptureGeneration   int64           `json:"captureGeneration"`
	Operation           string          `json:"operation"`
	Channel             string          `json:"channel"`
	Kind                string          `json:"kind"`
	AcknowledgedPrefix  uint64          `json:"acknowledgedPrefix,string"`
	OutputWatermark     uint64          `json:"outputWatermark,string"`
	DroppedBytes        uint64          `json:"droppedBytes,string"`
	NativeTerminalKnown bool            `json:"nativeTerminalKnown"`
	TransportJoined     bool            `json:"transportJoined"`
	OwnerState          string          `json:"ownerState"`
	LocalState          string          `json:"localState"`
	RemoteOutcome       string          `json:"remoteOutcome"`
	ExitCode            *int32          `json:"exitCode,omitempty"`
	Resource            ResourceAddress `json:"resource"`
	TargetRef           string          `json:"targetRef"`
	TargetRevision      int64           `json:"targetRevision"`
	ConnectionRef       string          `json:"connectionRef"`
	SourceDigest        string          `json:"sourceDigest"`
	ReasonCode          string          `json:"reasonCode"`
	SealedAt            time.Time       `json:"sealedAt"`
}

// Validate checks the safe read-only capture variant without granting artifact or target access.
// Exact private provenance and current actor eligibility remain the owner's responsibility.
// Validate проверяет safe read-only capture variant без artifact либо target access.
// Exact private provenance и current actor eligibility остаются ответственностью owner.
func (origin ArtifactCaptureOrigin) Validate() error {
	for _, ref := range []string{origin.InvocationRef, origin.TargetRef, origin.ConnectionRef} {
		if !ValidLocator(ref) || len(ref) > 96 {
			return ErrInvalidStreamReceipt
		}
	}
	if !opaqueref.Valid(origin.StreamRef, "kst") || origin.StreamGeneration < 1 || origin.CaptureGeneration < 1 || origin.TargetRevision < 1 || origin.SealedAt.IsZero() || origin.SealedAt.Year() < 2000 || origin.SealedAt.Year() > 9999 || !ValidDigest(origin.SourceDigest) || origin.Resource.Validate() != nil || origin.AcknowledgedPrefix > origin.OutputWatermark || origin.OutputWatermark > math.MaxInt64 || origin.DroppedBytes > MaximumStreamBytes {
		return ErrInvalidStreamReceipt
	}
	switch origin.Operation {
	case "kubernetes.exec", "kubernetes.attach", "kubernetes.debug", "kubernetes.logs", "kubernetes.watch", "kubernetes.watch.all-namespaces", "kubernetes.cp.upload", "kubernetes.cp.download", "kubernetes.port-forward":
	default:
		return ErrInvalidStreamReceipt
	}
	watch := origin.Operation == "kubernetes.watch" || origin.Operation == "kubernetes.watch.all-namespaces"
	resource := origin.Resource
	if watch {
		if resource.Name != "" || resource.UID != "" || resource.ResourceVersion != "" || resource.Subresource != "" || resource.AllNamespaces != (origin.Operation == "kubernetes.watch.all-namespaces") {
			return ErrInvalidStreamReceipt
		}
	} else if resource.Group != "" || resource.Version != "v1" || resource.Resource != "pods" || !resource.Namespaced || resource.AllNamespaces || resource.Name == "" || resource.UID == "" || resource.ResourceVersion == "" || resource.Subresource != "" {
		return ErrInvalidStreamReceipt
	}
	switch origin.Channel {
	case "CONTROL", "STDOUT", "STDERR", "PORT_FORWARD", "FILE_CONTENT":
	default:
		return ErrInvalidStreamReceipt
	}
	switch origin.OwnerState {
	case "COMPLETED", "CANCELED", "REVOKED", "EXPIRED", "LOST":
	default:
		return ErrInvalidStreamReceipt
	}
	if origin.Kind != "FULL" && origin.Kind != "PARTIAL" {
		return ErrInvalidStreamReceipt
	}
	if !origin.NativeTerminalKnown {
		if origin.TransportJoined || origin.LocalState != "UNKNOWN" || origin.RemoteOutcome != "UNKNOWN" || origin.ExitCode != nil || origin.ReasonCode != "" {
			return ErrInvalidStreamReceipt
		}
	} else {
		switch origin.LocalState {
		case "COMPLETED", "CANCELED", "REVOKED", "EXPIRED", "LOST":
		default:
			return ErrInvalidStreamReceipt
		}
		switch origin.RemoteOutcome {
		case "SUCCEEDED", "FAILED", "UNKNOWN":
		default:
			return ErrInvalidStreamReceipt
		}
		if !ValidStreamReason(origin.ReasonCode) || origin.RemoteOutcome == "UNKNOWN" && origin.ExitCode != nil || origin.ExitCode != nil && (*origin.ExitCode < 0 || origin.RemoteOutcome == "SUCCEEDED" && *origin.ExitCode != 0 || origin.RemoteOutcome == "FAILED" && *origin.ExitCode == 0) {
			return ErrInvalidStreamReceipt
		}
	}
	if origin.Kind == "FULL" && (!origin.NativeTerminalKnown || !origin.TransportJoined || origin.OwnerState != "COMPLETED" || origin.LocalState != "COMPLETED" || origin.AcknowledgedPrefix != origin.OutputWatermark || origin.DroppedBytes != 0) {
		return ErrInvalidStreamReceipt
	}
	return nil
}
