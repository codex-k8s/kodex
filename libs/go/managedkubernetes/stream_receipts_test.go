package managedkubernetes

import (
	"strings"
	"testing"
)

// receiptOriginFixture supplies synthetic immutable graph pins without a live owner or credential.
// receiptOriginFixture задаёт synthetic immutable graph pins без live owner либо credential.
func receiptOriginFixture() StreamReceiptOrigin {
	return StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64), EffectRef: "effect_synthetic", EffectGeneration: 1, StreamRef: "stream_synthetic", StreamGeneration: 7, Operation: "kubernetes.exec", TargetRef: "target_synthetic", TargetRevision: 2}
}

// receiptHandshakeFixture contains actual-profile-shaped facts, not proof of a real handshake.
// receiptHandshakeFixture содержит actual-profile-shaped facts, не proof реального handshake.
func receiptHandshakeFixture() HandshakeReceipt {
	return HandshakeReceipt{Origin: receiptOriginFixture(), TransportRef: "node_transport_synthetic", TransportGeneration: 3, Protocol: string(WebSocketV5), HTTPStatus: 101, NegotiatedProtocol: WebSocketV5.WireProtocol(), PeerSPKISHA256: strings.Repeat("d", 64)}
}

// TestBindingDigestIncludesEveryClaimPin excludes health claims and commits to fence and lineage.
// TestBindingDigestIncludesEveryClaimPin исключает health claims и связывает fence и lineage.
func TestBindingDigestIncludesEveryClaimPin(t *testing.T) {
	base := Binding{WorkloadInstance: "worker_synthetic", InvocationRef: "inv_synthetic", LeaseRef: "lease_synthetic", Fence: "fence_synthetic", Generation: 7, InputDigest: strings.Repeat("a", 64)}
	digest, err := BindingDigest(base)
	if err != nil || !ValidDigest(digest) {
		t.Fatal("valid claim digest failed")
	}
	for _, change := range []func(*Binding){
		func(b *Binding) { b.WorkloadInstance += "_other" }, func(b *Binding) { b.InvocationRef += "_other" }, func(b *Binding) { b.LeaseRef += "_other" }, func(b *Binding) { b.Fence += "_other" }, func(b *Binding) { b.Generation++ }, func(b *Binding) { b.InputDigest = strings.Repeat("b", 64) },
	} {
		candidate := base
		change(&candidate)
		next, err := BindingDigest(candidate)
		if err != nil || next == digest {
			t.Fatal("claim field not bound to digest")
		}
	}
	base.InvocationRef = ""
	base.ConnectionTestRef = "health_synthetic"
	if _, err := BindingDigest(base); err == nil {
		t.Fatal("health claim accepted as stream origin")
	}
}

// TestHandshakeReceiptBindsPinsAndRejectsFallback checks the complete observed transport tuple.
// TestHandshakeReceiptBindsPinsAndRejectsFallback проверяет полный observed transport tuple.
func TestHandshakeReceiptBindsPinsAndRejectsFallback(t *testing.T) {
	base := receiptHandshakeFixture()
	digest, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*HandshakeReceipt){
		func(r *HandshakeReceipt) { r.Origin.SnapshotDigest = strings.Repeat("e", 64) },
		func(r *HandshakeReceipt) { r.Origin.BindingDigest = strings.Repeat("e", 64) },
		func(r *HandshakeReceipt) { r.Origin.ChildInputDigest = strings.Repeat("e", 64) },
		func(r *HandshakeReceipt) { r.Origin.EffectRef += "_other" }, func(r *HandshakeReceipt) { r.Origin.EffectGeneration++ },
		func(r *HandshakeReceipt) { r.Origin.StreamRef += "_other" }, func(r *HandshakeReceipt) { r.Origin.StreamGeneration++ },
		func(r *HandshakeReceipt) { r.Origin.TargetRef += "_other" }, func(r *HandshakeReceipt) { r.Origin.TargetRevision++ },
		func(r *HandshakeReceipt) { r.TransportRef += "_other" }, func(r *HandshakeReceipt) { r.TransportGeneration++ },
		func(r *HandshakeReceipt) { r.PeerSPKISHA256 = strings.Repeat("e", 64) },
		func(r *HandshakeReceipt) { r.Origin.Operation = "kubernetes.attach" },
	} {
		candidate := base
		change(&candidate)
		next, err := candidate.Digest()
		if err != nil || next == digest {
			t.Fatal("handshake field not bound to digest")
		}
	}
	for _, change := range []func(*HandshakeReceipt){
		func(r *HandshakeReceipt) { r.HTTPStatus = 200 }, func(r *HandshakeReceipt) { r.Protocol = "AUTO" },
		func(r *HandshakeReceipt) { r.NegotiatedProtocol = SPDYV4.WireProtocol() }, func(r *HandshakeReceipt) { r.PeerSPKISHA256 = "" },
		func(r *HandshakeReceipt) { r.Origin.Operation = "kubernetes.get" }, func(r *HandshakeReceipt) { r.TransportGeneration = 0 },
	} {
		candidate := base
		change(&candidate)
		if _, err := candidate.Digest(); err == nil {
			t.Fatal("invalid or downgraded handshake accepted")
		}
	}
}

// TestHTTPHandshakeRequiresExactTarget maps non-upgrade reads only to the registered API target.
// TestHTTPHandshakeRequiresExactTarget связывает non-upgrade reads только с registered API target.
func TestHTTPHandshakeRequiresExactTarget(t *testing.T) {
	for _, protocol := range []string{HTTPWatchV1, HTTPLogsV1} {
		receipt := receiptHandshakeFixture()
		receipt.Protocol = protocol
		receipt.HTTPStatus = 200
		receipt.NegotiatedProtocol = ""
		receipt.TransportRef = receipt.Origin.TargetRef
		receipt.TransportGeneration = receipt.Origin.TargetRevision
		receipt.PeerSPKISHA256 = ""
		if protocol == HTTPLogsV1 {
			receipt.Origin.Operation = "kubernetes.logs"
		} else {
			receipt.Origin.Operation = "kubernetes.watch"
		}
		if _, err := receipt.Digest(); err != nil {
			t.Fatal("exact HTTP stream profile rejected")
		}
		receipt.TransportGeneration++
		if _, err := receipt.Digest(); err == nil {
			t.Fatal("HTTP stream accepted foreign target revision")
		}
	}
}

// TestTerminalReceiptRequiresProtocolSpecificEvidence refuses EOF-as-exit and local-close-as-success.
// TestTerminalReceiptRequiresProtocolSpecificEvidence отвергает EOF-as-exit и local-close-as-success.
func TestTerminalReceiptRequiresProtocolSpecificEvidence(t *testing.T) {
	zero := int32(0)
	base := TerminalReceipt{Origin: receiptOriginFixture(), HandshakeDigest: strings.Repeat("d", 64), Protocol: string(WebSocketV5), TransportJoined: true, LocalState: "COMPLETED", RemoteOutcome: "SUCCEEDED", ExitCode: &zero, ReasonCode: "REMOTE_EXIT"}
	digest, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	failed := base
	nonzero := int32(137)
	failed.ExitCode = &nonzero
	failed.RemoteOutcome = "FAILED"
	other, err := failed.Digest()
	if err != nil || other == digest {
		t.Fatal("nonzero remote exit was lost")
	}
	for _, change := range []func(*TerminalReceipt){
		func(r *TerminalReceipt) { r.ExitCode = nil }, func(r *TerminalReceipt) { r.TransportJoined = false },
		func(r *TerminalReceipt) { r.CleanEOF = true }, func(r *TerminalReceipt) { r.RemoteOutcome = "UNKNOWN" },
		func(r *TerminalReceipt) { r.LocalState = "ACTIVE" }, func(r *TerminalReceipt) { r.ReasonCode = "arbitrary" },
		func(r *TerminalReceipt) { r.ExitCode = &nonzero }, func(r *TerminalReceipt) { r.HandshakeDigest = "" },
	} {
		candidate := base
		change(&candidate)
		if _, err := candidate.Digest(); err == nil {
			t.Fatal("unproven remote terminal outcome accepted")
		}
	}
	unknown := base
	unknown.LocalState = "LOST"
	unknown.RemoteOutcome = "UNKNOWN"
	unknown.ExitCode = nil
	unknown.ReasonCode = "TRANSPORT_LOST"
	if _, err := unknown.Digest(); err != nil {
		t.Fatal("uncertain joined socket rejected")
	}
	tunnel := unknown
	tunnel.Origin.Operation = "kubernetes.port-forward"
	tunnel.Protocol = string(PortForwardWebSocketV4)
	if _, err := tunnel.Digest(); err != nil {
		t.Fatal("unknown tunnel outcome rejected")
	}
	tunnel.CleanEOF = true
	tunnel.RemoteOutcome = "SUCCEEDED"
	tunnel.LocalState = "COMPLETED"
	tunnel.ReasonCode = "CLEAN_EOF"
	if _, err := tunnel.Digest(); err == nil {
		t.Fatal("tunnel EOF promoted to execution success")
	}
	logs := tunnel
	logs.Protocol = HTTPLogsV1
	logs.Origin.Operation = "kubernetes.logs"
	if _, err := logs.Digest(); err != nil {
		t.Fatal("clean bounded HTTP log response rejected")
	}
	logs.CleanEOF = false
	logs.RemoteOutcome = "FAILED"
	logs.RemoteStatusCode = 410
	logs.ReasonCode = "REMOTE_FAILURE"
	if _, err := logs.Digest(); err != nil {
		t.Fatal("explicit HTTP remote failure rejected")
	}
	logs.RemoteStatusCode = 0
	if _, err := logs.Digest(); err == nil {
		t.Fatal("HTTP failure invented without status")
	}
}

// TestTerminalReceiptRetainsExitWhenOutputIsLost separates received process status from local delivery.
// TestTerminalReceiptRetainsExitWhenOutputIsLost отделяет полученный process status от local delivery.
func TestTerminalReceiptRetainsExitWhenOutputIsLost(t *testing.T) {
	for state, reason := range map[string]string{"LOST": "OUTPUT_LOST", "CANCELED": "CANCELED", "REVOKED": "REVOKED", "EXPIRED": "EXPIRED"} {
		t.Run(state, func(t *testing.T) {
			zero := int32(0)
			receipt := TerminalReceipt{Origin: receiptOriginFixture(), HandshakeDigest: strings.Repeat("d", 64), Protocol: string(WebSocketV5), TransportJoined: true, LocalState: state, RemoteOutcome: "SUCCEEDED", ExitCode: &zero, ReasonCode: reason}
			if _, err := receipt.Digest(); err != nil {
				t.Fatal("proven exit lost because output delivery stopped")
			}
			receipt.ExitCode = nil
			if _, err := receipt.Digest(); err == nil {
				t.Fatal("local stop cause invented missing exit")
			}
			receipt.ExitCode = &zero
			receipt.LocalState = "COMPLETED"
			if _, err := receipt.Digest(); err == nil {
				t.Fatal("local loss promoted to completed output")
			}
			receipt.LocalState = state
			receipt.Protocol = HTTPLogsV1
			receipt.Origin.Operation = "kubernetes.logs"
			receipt.ExitCode = nil
			receipt.CleanEOF = true
			if _, err := receipt.Digest(); err != nil {
				t.Fatal("received HTTP completion lost with local stop cause")
			}
			receipt.CleanEOF = false
			if _, err := receipt.Digest(); err == nil {
				t.Fatal("local stop invented HTTP completion")
			}
		})
	}
}
