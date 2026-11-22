package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// ErrInvalidStreamReceipt rejects unproven or inconsistent observations without exposing private pins.
// ErrInvalidStreamReceipt отклоняет недоказанные либо противоречивые observations без private pins.
var ErrInvalidStreamReceipt = errors.New("invalid managed Kubernetes stream receipt")

// BindingDigest commits to every exact original claim field, including its private fence.
// The digest is not independent authority; the owner recomputes it from its verified binding.
// BindingDigest связывает все exact original claim fields, включая private fence.
// Digest не является authority; owner пересчитывает его из проверенного binding.
func BindingDigest(binding Binding) (string, error) {
	if binding.Validate() != nil || binding.InvocationRef == "" {
		return "", ErrInvalidStreamReceipt
	}
	return streamReceiptDigest("kodex.managed-kubernetes.binding/v1", struct {
		WorkloadInstance string `json:"workloadInstance"`
		InvocationRef    string `json:"invocationRef"`
		LeaseRef         string `json:"leaseRef"`
		Fence            string `json:"fence"`
		Generation       int64  `json:"generation"`
		InputDigest      string `json:"inputDigest"`
	}{binding.WorkloadInstance, binding.InvocationRef, binding.LeaseRef, binding.Fence, binding.Generation, binding.InputDigest})
}

// StreamReceiptOrigin links observed evidence to one immutable owner child, stream and original claim.
// Structural validation grants no authority: the owner compares every field with its current graph.
// StreamReceiptOrigin связывает evidence с immutable owner child, stream и original claim.
// Structural validation не выдаёт authority: owner сравнивает все fields с current graph.
type StreamReceiptOrigin struct {
	SnapshotDigest   string `json:"snapshotDigest"`
	BindingDigest    string `json:"bindingDigest"`
	ChildInputDigest string `json:"childInputDigest"`
	EffectRef        string `json:"effectRef"`
	EffectGeneration int64  `json:"effectGeneration"`
	StreamRef        string `json:"streamRef"`
	StreamGeneration int64  `json:"streamGeneration"`
	Operation        string `json:"operation"`
	TargetRef        string `json:"targetRef"`
	TargetRevision   int64  `json:"targetRevision"`
}

// Validate requires complete exact owner references and a registered streaming operation.
// Validate требует complete exact owner references и registered streaming operation.
func (origin StreamReceiptOrigin) Validate() error {
	if !ValidDigest(origin.SnapshotDigest) || !ValidDigest(origin.BindingDigest) || !ValidDigest(origin.ChildInputDigest) || !ValidLocator(origin.EffectRef) || !ValidLocator(origin.StreamRef) || !ValidLocator(origin.TargetRef) || origin.EffectGeneration < 1 || origin.StreamGeneration < 1 || origin.TargetRevision < 1 {
		return ErrInvalidStreamReceipt
	}
	switch origin.Operation {
	case "kubernetes.watch", "kubernetes.watch.all-namespaces", "kubernetes.logs", "kubernetes.exec", "kubernetes.attach", "kubernetes.debug", "kubernetes.cp.upload", "kubernetes.cp.download", "kubernetes.port-forward":
		return nil
	default:
		return ErrInvalidStreamReceipt
	}
}

const (
	// HTTPWatchV1 names the bounded Kubernetes watch JSON stream, not an arbitrary HTTP endpoint.
	// HTTPWatchV1 обозначает bounded Kubernetes watch JSON stream, не arbitrary HTTP endpoint.
	HTTPWatchV1 = "HTTP_WATCH_V1"
	// HTTPLogsV1 names the bounded Pod log response under exact registered resource scope.
	// HTTPLogsV1 обозначает bounded Pod log response с exact registered resource scope.
	HTTPLogsV1 = "HTTP_LOGS_V1"
)

// streamProtocolMatches binds the finite protocol family to the operation without fallback.
// streamProtocolMatches связывает finite protocol family с operation без fallback.
func streamProtocolMatches(operation, protocol string) bool {
	switch protocol {
	case HTTPWatchV1:
		return operation == "kubernetes.watch" || operation == "kubernetes.watch.all-namespaces"
	case HTTPLogsV1:
		return operation == "kubernetes.logs"
	case string(WebSocketV5), string(SPDYV4):
		return operation == "kubernetes.exec" || operation == "kubernetes.attach" || operation == "kubernetes.debug" || operation == "kubernetes.cp.upload" || operation == "kubernetes.cp.download"
	case string(PortForwardWebSocketV4), string(PortForwardSPDYV1):
		return operation == "kubernetes.port-forward"
	default:
		return false
	}
}

// HandshakeReceipt records actual negotiated transport facts without URLs, credentials or response bodies.
// Privileged peers require the observed SPKI digest, which the owner compares with its registered pin.
// HandshakeReceipt сохраняет actual negotiated transport facts без URL, credentials либо response bodies.
// Для privileged peer нужен observed SPKI digest, сравниваемый owner с registered pin.
type HandshakeReceipt struct {
	Origin              StreamReceiptOrigin `json:"origin"`
	TransportRef        string              `json:"transportRef"`
	TransportGeneration int64               `json:"transportGeneration"`
	Protocol            string              `json:"protocol"`
	HTTPStatus          int                 `json:"httpStatus"`
	NegotiatedProtocol  string              `json:"negotiatedProtocol"`
	PeerSPKISHA256      string              `json:"peerSPKISHA256"`
}

// Validate accepts only an observed successful handshake for the exact finite protocol profile.
// Validate принимает только observed successful handshake для exact finite protocol profile.
func (receipt HandshakeReceipt) Validate() error {
	if receipt.Origin.Validate() != nil || !ValidLocator(receipt.TransportRef) || receipt.TransportGeneration < 1 || !streamProtocolMatches(receipt.Origin.Operation, receipt.Protocol) {
		return ErrInvalidStreamReceipt
	}
	if receipt.Protocol == HTTPWatchV1 || receipt.Protocol == HTTPLogsV1 {
		if receipt.HTTPStatus != 200 || receipt.NegotiatedProtocol != "" || receipt.TransportRef != receipt.Origin.TargetRef || receipt.TransportGeneration != receipt.Origin.TargetRevision || receipt.PeerSPKISHA256 != "" && !ValidDigest(receipt.PeerSPKISHA256) {
			return ErrInvalidStreamReceipt
		}
	} else if receipt.HTTPStatus != 101 || receipt.NegotiatedProtocol != PrivilegedProtocol(receipt.Protocol).WireProtocol() || !ValidDigest(receipt.PeerSPKISHA256) {
		return ErrInvalidStreamReceipt
	}
	return nil
}

// Digest hashes the complete validated handshake; the owner must independently recompute it.
// Digest хеширует complete validated handshake; owner независимо пересчитывает его.
func (receipt HandshakeReceipt) Digest() (string, error) {
	if receipt.Validate() != nil {
		return "", ErrInvalidStreamReceipt
	}
	return streamReceiptDigest("kodex.managed-kubernetes.handshake/v1", receipt)
}

// TerminalReceipt separates local join from remote evidence and links back to the original handshake.
// CleanEOF is meaningful only for HTTP watch/logs; it never substitutes an exec exit or tunnel receipt.
// TerminalReceipt отделяет local join от remote evidence и ссылается на original handshake.
// CleanEOF значим только для HTTP watch/logs; не заменяет exec exit либо tunnel receipt.
type TerminalReceipt struct {
	Origin           StreamReceiptOrigin `json:"origin"`
	HandshakeDigest  string              `json:"handshakeDigest"`
	Protocol         string              `json:"protocol"`
	TransportJoined  bool                `json:"transportJoined"`
	LocalState       string              `json:"localState"`
	RemoteOutcome    string              `json:"remoteOutcome"`
	ExitCode         *int32              `json:"exitCode,omitempty"`
	CleanEOF         bool                `json:"cleanEOF"`
	RemoteStatusCode int                 `json:"remoteStatusCode,omitempty"`
	ReasonCode       string              `json:"reasonCode"`
}

// Validate refuses invented remote success, contradictory exits and unknown lifecycle/reason values.
// Validate отклоняет выдуманный remote success, противоречивые exits и unknown lifecycle/reason values.
func (receipt TerminalReceipt) Validate() error {
	if receipt.Origin.Validate() != nil || !streamProtocolMatches(receipt.Origin.Operation, receipt.Protocol) {
		return ErrInvalidStreamReceipt
	}
	if !ValidDigest(receipt.HandshakeDigest) && !(receipt.HandshakeDigest == "" && (receipt.ReasonCode == "HANDSHAKE_FAILED" || receipt.ReasonCode == "OWNER_STATE_LOST")) {
		return ErrInvalidStreamReceipt
	}
	// Missing local ownership does not prove a failed handshake or a joined writer in another replica.
	// Потеря local ownership не доказывает неуспех handshake либо join writer другой replica.
	if receipt.ReasonCode == "OWNER_STATE_LOST" && (receipt.HandshakeDigest != "" || receipt.TransportJoined || receipt.LocalState != "LOST" || receipt.RemoteOutcome != "UNKNOWN") {
		return ErrInvalidStreamReceipt
	}
	switch receipt.LocalState {
	case "COMPLETED", "CANCELED", "REVOKED", "EXPIRED", "LOST":
	default:
		return ErrInvalidStreamReceipt
	}
	if !ValidStreamReason(receipt.ReasonCode) {
		return ErrInvalidStreamReceipt
	}
	if receipt.LocalState == "COMPLETED" && (!receipt.TransportJoined || receipt.RemoteOutcome == "UNKNOWN") {
		return ErrInvalidStreamReceipt
	}
	if receipt.RemoteOutcome == "UNKNOWN" {
		if receipt.ExitCode != nil || receipt.CleanEOF || receipt.RemoteStatusCode != 0 || receipt.ReasonCode == "REMOTE_EXIT" || receipt.ReasonCode == "CLEAN_EOF" || receipt.ReasonCode == "REMOTE_FAILURE" {
			return ErrInvalidStreamReceipt
		}
		return nil
	}
	if receipt.RemoteOutcome != "SUCCEEDED" && receipt.RemoteOutcome != "FAILED" || !receipt.TransportJoined || !ValidDigest(receipt.HandshakeDigest) {
		return ErrInvalidStreamReceipt
	}
	if receipt.Protocol == HTTPWatchV1 || receipt.Protocol == HTTPLogsV1 {
		if receipt.ExitCode != nil {
			return ErrInvalidStreamReceipt
		}
		if receipt.RemoteOutcome == "SUCCEEDED" {
			if !receipt.CleanEOF || receipt.RemoteStatusCode != 0 || !receipt.matchesObservedReason("CLEAN_EOF") {
				return ErrInvalidStreamReceipt
			}
		} else if receipt.CleanEOF || receipt.RemoteStatusCode < 400 || receipt.RemoteStatusCode > 599 || !receipt.matchesObservedReason("REMOTE_FAILURE") {
			return ErrInvalidStreamReceipt
		}
		return nil
	}
	if receipt.Protocol == string(PortForwardWebSocketV4) || receipt.Protocol == string(PortForwardSPDYV1) || receipt.ExitCode == nil || *receipt.ExitCode < 0 || *receipt.ExitCode > 255 || receipt.CleanEOF || receipt.RemoteStatusCode != 0 || !receipt.matchesObservedReason("REMOTE_EXIT") || (*receipt.ExitCode == 0) != (receipt.RemoteOutcome == "SUCCEEDED") {
		return ErrInvalidStreamReceipt
	}
	return nil
}

// matchesObservedReason preserves a proven remote outcome together with a distinct local stop cause.
// It never supplies missing remote evidence or treats incomplete output delivery as COMPLETED.
// matchesObservedReason сохраняет proven remote outcome вместе с отдельной local stop cause.
// Не подставляет missing remote evidence и не превращает incomplete output delivery в COMPLETED.
func (receipt TerminalReceipt) matchesObservedReason(remoteReason string) bool {
	if receipt.ReasonCode == remoteReason {
		return true
	}
	switch receipt.LocalState {
	case "CANCELED":
		return receipt.ReasonCode == "CANCELED"
	case "REVOKED":
		return receipt.ReasonCode == "REVOKED"
	case "EXPIRED":
		return receipt.ReasonCode == "EXPIRED"
	case "LOST":
		return receipt.ReasonCode == "TRANSPORT_LOST" || receipt.ReasonCode == "OUTPUT_LOST" || receipt.ReasonCode == "AUTHORITY_UNAVAILABLE" || receipt.ReasonCode == "LIMIT_EXCEEDED"
	default:
		return false
	}
}

// Digest commits to the complete validated terminal observation without implying owner acceptance.
// Digest связывает complete validated terminal observation, не подразумевая owner acceptance.
func (receipt TerminalReceipt) Digest() (string, error) {
	if receipt.Validate() != nil {
		return "", ErrInvalidStreamReceipt
	}
	return streamReceiptDigest("kodex.managed-kubernetes.terminal/v1", receipt)
}

// streamReceiptDigest uses typed canonical JSON with a domain separator and clears its temporary bytes.
// streamReceiptDigest использует typed canonical JSON с domain separator и очищает temporary bytes.
func streamReceiptDigest(domain string, value any) (string, error) {
	raw, err := json.Marshal(struct {
		Domain string `json:"domain"`
		Value  any    `json:"value"`
	}{domain, value})
	if err != nil {
		return "", ErrInvalidStreamReceipt
	}
	defer clear(raw)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// ValidStreamReason recognizes only protocol-owned finite reasons, never private upstream diagnostics.
// ValidStreamReason распознаёт только protocol-owned finite reasons без private upstream diagnostics.
func ValidStreamReason(reason string) bool {
	switch reason {
	case "REMOTE_EXIT", "CLEAN_EOF", "REMOTE_FAILURE", "CANCELED", "REVOKED", "EXPIRED", "TRANSPORT_LOST", "OUTPUT_LOST", "AUTHORITY_UNAVAILABLE", "LIMIT_EXCEEDED", "HANDSHAKE_FAILED", "OWNER_STATE_LOST":
		return true
	default:
		return false
	}
}
