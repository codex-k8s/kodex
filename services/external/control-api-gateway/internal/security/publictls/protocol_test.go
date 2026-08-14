package publictls

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestIdempotencyKeyIsBoundToCurrentProtocolVersion(t *testing.T) {
	t.Parallel()

	manager := &Manager{generation: 7, certificateSHA256: strings.Repeat("a", 64)}
	current := manager.idempotencyKey("prepare")
	previous := []uuid.UUID{
		uuid.NewSHA1(uuid.NameSpaceOID, []byte(
			"control-api-gateway-public-tls:prepare:7:"+manager.certificateSHA256,
		)),
		uuid.NewSHA1(uuid.NameSpaceOID, []byte(
			"control-api-gateway-public-tls:v2:prepare:7:"+manager.certificateSHA256,
		)),
		uuid.NewSHA1(uuid.NameSpaceOID, []byte(
			"control-api-gateway-public-tls:v3:prepare:7:"+manager.certificateSHA256,
		)),
	}
	for _, previousKey := range previous {
		if current == previousKey {
			t.Fatal("current public TLS protocol must not reuse a previous receipt namespace")
		}
	}
	if current != manager.idempotencyKey("prepare") {
		t.Fatal("public TLS idempotency key must remain deterministic")
	}
}

func TestNewRejectsNonAtomicMaterialLayout(t *testing.T) {
	t.Parallel()

	first := t.TempDir()
	second := t.TempDir()
	_, err := New(Config{
		CertificateFile: first + "/tls.crt", PrivateKeyFile: first + "/tls.key",
		CAFile: first + "/ca.crt", MaterialFile: second + "/material.json",
		ServerName: "control-api.mattercodex.local",
	})
	if err == nil || err.Error() != "public TLS material is not atomic" {
		t.Fatalf("separate Secret revisions must be rejected: %v", err)
	}
	_, err = New(Config{
		CertificateFile: first + "/tls.crt", PrivateKeyFile: first + "/tls.key",
		CAFile: first + "/ca.crt", MaterialFile: first + "/material.json",
		ServerName: "control-api.mattercodex.local",
	})
	if err == nil || err.Error() != "public TLS material revision is unavailable" {
		t.Fatalf("an unversioned directory must be rejected: %v", err)
	}
}

type protocolControl struct {
	state   *controlplanev1.GatewayPublicTLSState
	now     time.Time
	overlap time.Duration
}

func (control *protocolControl) PrepareGatewayPublicTLS(_ context.Context, request *controlplanev1.PrepareGatewayPublicTLSRequest, _ ...grpc.CallOption) (*controlplanev1.PrepareGatewayPublicTLSResponse, error) {
	candidate := &controlplanev1.GatewayPublicTLSMaterial{
		Generation: request.GetGeneration(), CertificateSha256: request.GetCertificateSha256(),
		NotBefore: request.GetNotBefore(), NotAfter: request.GetNotAfter(),
	}
	if control.state == nil {
		if request.GetGeneration() != 1 || request.GetPredecessorGeneration() != 0 || request.GetPredecessorCertificateSha256() != "" {
			return nil, errors.New("skipped initial generation")
		}
		control.state = &controlplanev1.GatewayPublicTLSState{Pending: candidate, UpdatedAt: timestamppb.New(control.now)}
		return &controlplanev1.PrepareGatewayPublicTLSResponse{State: control.state}, nil
	}
	if exactMaterial(control.state.GetPending(), candidate) {
		return &controlplanev1.PrepareGatewayPublicTLSResponse{State: control.state}, nil
	}
	if exactMaterial(control.state.GetApplied(), candidate) ||
		(exactMaterial(control.state.GetPrevious(), candidate) && control.state.GetOverlapExpiresAt().AsTime().After(control.now)) {
		return &controlplanev1.PrepareGatewayPublicTLSResponse{State: control.state}, nil
	}
	applied := control.state.GetApplied()
	if control.state.GetPending() != nil || applied == nil || request.GetGeneration() != applied.GetGeneration()+1 ||
		request.GetPredecessorGeneration() != applied.GetGeneration() || request.GetPredecessorCertificateSha256() != applied.GetCertificateSha256() {
		return nil, errors.New("rollback, skip, or predecessor mismatch")
	}
	control.state.Pending = candidate
	return &controlplanev1.PrepareGatewayPublicTLSResponse{State: control.state}, nil
}

func (control *protocolControl) ConfirmGatewayPublicTLS(_ context.Context, request *controlplanev1.ConfirmGatewayPublicTLSRequest, _ ...grpc.CallOption) (*controlplanev1.ConfirmGatewayPublicTLSResponse, error) {
	if control.state == nil || control.state.GetPending() == nil ||
		control.state.GetPending().GetGeneration() != request.GetGeneration() ||
		control.state.GetPending().GetCertificateSha256() != request.GetCertificateSha256() {
		return nil, errors.New("pending material mismatch")
	}
	control.state.Previous = control.state.Applied
	if control.state.Previous != nil {
		control.state.OverlapExpiresAt = timestamppb.New(control.now.Add(control.overlap))
	}
	control.state.Applied = control.state.Pending
	control.state.Pending = nil
	control.state.UpdatedAt = timestamppb.New(control.now)
	return &controlplanev1.ConfirmGatewayPublicTLSResponse{State: control.state}, nil
}

func (control *protocolControl) CheckGatewayPublicTLS(_ context.Context, request *controlplanev1.CheckGatewayPublicTLSRequest, _ ...grpc.CallOption) (*controlplanev1.CheckGatewayPublicTLSResponse, error) {
	candidate := &controlplanev1.GatewayPublicTLSMaterial{Generation: request.GetGeneration(), CertificateSha256: request.GetCertificateSha256()}
	if control.state != nil && (sameGenerationDigest(control.state.GetApplied(), candidate) ||
		sameGenerationDigest(control.state.GetPending(), candidate) ||
		(sameGenerationDigest(control.state.GetPrevious(), candidate) && control.state.GetOverlapExpiresAt().AsTime().After(control.now))) {
		return &controlplanev1.CheckGatewayPublicTLSResponse{State: control.state}, nil
	}
	return nil, errors.New("served material is outside authoritative state")
}

func exactMaterial(left, right *controlplanev1.GatewayPublicTLSMaterial) bool {
	return sameGenerationDigest(left, right) && left.GetNotBefore().AsTime().Equal(right.GetNotBefore().AsTime()) &&
		left.GetNotAfter().AsTime().Equal(right.GetNotAfter().AsTime())
}

func sameGenerationDigest(left, right *controlplanev1.GatewayPublicTLSMaterial) bool {
	return left != nil && right != nil && left.GetGeneration() == right.GetGeneration() &&
		left.GetCertificateSha256() == right.GetCertificateSha256()
}

func TestForwardOnlyPrepareConfirmCrashOverlapAndRollback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	control := &protocolControl{now: now, overlap: 15 * time.Minute}
	first := testManager(1, strings.Repeat("1", 64), 0, "", now)
	if err := first.Prepare(context.Background(), control); err != nil {
		t.Fatalf("prepare generation 1: %v", err)
	}
	if control.state.GetApplied() != nil || control.state.GetPending().GetGeneration() != 1 {
		t.Fatal("prepare must leave generation 1 pending")
	}

	// Crash after Prepare must replay the exact PENDING candidate without
	// changing APPLIED. A fresh process object models pod replacement.
	restarted := testManager(1, strings.Repeat("1", 64), 0, "", now)
	if err := restarted.Prepare(context.Background(), control); err != nil || control.state.GetApplied() != nil {
		t.Fatalf("prepare replay after crash: state=%v err=%v", control.state, err)
	}
	if err := restarted.Confirm(context.Background(), control); err != nil {
		t.Fatalf("confirm generation 1: %v", err)
	}

	second := testManager(2, strings.Repeat("2", 64), 1, strings.Repeat("1", 64), now.Add(time.Minute))
	if err := second.Prepare(context.Background(), control); err != nil {
		t.Fatalf("prepare generation 2: %v", err)
	}
	if control.state.GetApplied().GetGeneration() != 1 || control.state.GetPending().GetGeneration() != 2 {
		t.Fatal("prepare generation 2 must not advance applied state")
	}
	if err := first.Check(context.Background(), control); err != nil {
		t.Fatalf("old replica must remain ready while generation 2 is pending: %v", err)
	}
	if err := second.Confirm(context.Background(), control); err != nil {
		t.Fatalf("confirm generation 2: %v", err)
	}
	if control.state.GetApplied().GetGeneration() != 2 || control.state.GetPrevious().GetGeneration() != 1 {
		t.Fatal("confirm must atomically move applied to previous overlap")
	}
	if err := first.Check(context.Background(), control); err != nil {
		t.Fatalf("old replica must be accepted during overlap: %v", err)
	}
	control.now = control.state.GetOverlapExpiresAt().AsTime().Add(time.Microsecond)
	if err := first.Check(context.Background(), control); err == nil {
		t.Fatal("old replica must fail readiness after overlap")
	}
	if err := first.Prepare(context.Background(), control); err == nil {
		t.Fatal("rollback generation must be rejected after overlap")
	}
	skipped := testManager(4, strings.Repeat("4", 64), 2, strings.Repeat("2", 64), now)
	if err := skipped.Prepare(context.Background(), control); err == nil {
		t.Fatal("skipped generation must be rejected")
	}
}

func testManager(generation uint64, digest string, predecessor uint64, predecessorDigest string, now time.Time) *Manager {
	return &Manager{
		generation: generation, certificateSHA256: digest, predecessor: predecessor,
		predecessorSHA256: predecessorDigest, notBefore: now.Add(-time.Minute), notAfter: now.Add(time.Hour),
	}
}
