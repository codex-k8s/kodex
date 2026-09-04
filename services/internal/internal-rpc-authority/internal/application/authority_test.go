package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestRestoreBlockAtomicallyClosesAdmissionAndWaitsForInflight(t *testing.T) {
	t.Parallel()
	authority := &Authority{}
	authority.domain.Store(&service.Authority{})
	authority.SetAvailable(true)

	_, done, err := authority.begin()
	if err != nil {
		t.Fatalf("begin admitted operation: %v", err)
	}
	authority.SetRestoreBlocked(true)
	if authority.available.Load() {
		t.Fatal("restore block did not close admission")
	}
	if _, _, err := authority.begin(); err == nil {
		t.Fatal("operation was admitted after restore block")
	}
	authority.SetAvailable(true)
	if authority.available.Load() {
		t.Fatal("generic readiness reopened a restore-blocked authority")
	}

	drainDone := make(chan error, 1)
	go func() {
		drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		drainDone <- authority.WaitDrained(drainCtx)
	}()
	select {
	case err := <-drainDone:
		t.Fatalf("drain completed before inflight operation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	done()
	if err := <-drainDone; err != nil {
		t.Fatalf("wait for inflight drain: %v", err)
	}

	authority.SetRestoreBlocked(false)
	authority.SetAvailable(true)
	if !authority.available.Load() {
		t.Fatal("authority did not reopen after restore fence release")
	}
}

func TestActivateSnapshotReusesPendingReceiptUntilWatermarkPersists(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 26, 1, 0, 0, 0, time.UTC)
	store := &activationStore{activationErrors: []error{
		repository.ErrSnapshotRollback,
		nil,
		nil,
	}}
	domain := newActivationAuthority(t, now, store)
	attestor := &activationAttestor{now: now}
	application, err := NewAuthority(domain, attestor)
	if err != nil {
		t.Fatalf("create authority application: %v", err)
	}
	application.now = func() time.Time { return now }

	if err := application.ActivateSnapshot(context.Background()); err == nil {
		t.Fatal("incomplete publisher promotion was accepted")
	}
	if err := application.ActivateSnapshot(context.Background()); err != nil {
		t.Fatalf("retry pending snapshot activation: %v", err)
	}
	if attestor.calls != 1 {
		t.Fatalf("pending receipt was reissued: calls=%d", attestor.calls)
	}
	if len(store.receiptIDs) != 2 || store.receiptIDs[0] != store.receiptIDs[1] {
		t.Fatalf("pending receipt was not reused: ids=%v", store.receiptIDs)
	}

	if err := application.ActivateSnapshot(context.Background()); err != nil {
		t.Fatalf("refresh activated snapshot: %v", err)
	}
	if attestor.calls != 2 || store.receiptIDs[2] == store.receiptIDs[1] {
		t.Fatalf("successful activation did not clear pending receipt: calls=%d ids=%v", attestor.calls, store.receiptIDs)
	}
}

type activationAttestor struct {
	now   time.Time
	calls int
}

func (attestor *activationAttestor) Attest(
	context.Context,
	repository.SnapshotState,
) (repository.SnapshotAttestationReceipt, error) {
	attestor.calls++
	receiptID := "82d62007-3a12-4c43-8a0b-1dbf694f55b4"
	if attestor.calls > 1 {
		receiptID = "76932eaf-7d68-4a4d-a8dc-22d4ffc7aa72"
	}
	return repository.SnapshotAttestationReceipt{
		ReceiptID: receiptID,
		ExpiresAt: attestor.now.Add(5 * time.Minute),
	}, nil
}

type activationStore struct {
	activationErrors []error
	receiptIDs       []string
}

func (store *activationStore) Reserve(context.Context, repository.Reservation) error {
	return nil
}

func (store *activationStore) ReserveContinuation(
	context.Context,
	repository.Reservation,
	repository.Reservation,
) error {
	return nil
}

func (store *activationStore) ActivateSnapshot(
	_ context.Context,
	state repository.SnapshotState,
) error {
	store.receiptIDs = append(store.receiptIDs, state.AttestationReceiptID)
	index := len(store.receiptIDs) - 1
	if index < len(store.activationErrors) {
		return store.activationErrors[index]
	}
	return nil
}

func (store *activationStore) AcceptVerification(
	context.Context,
	repository.SnapshotState,
	repository.Reservation,
) error {
	return nil
}

func (store *activationStore) Ready(context.Context, repository.SnapshotState) error {
	return nil
}

func (store *activationStore) Close() {}

func newActivationAuthority(
	t *testing.T,
	now time.Time,
	store repository.Store,
) *service.Authority {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key("activation-g1")
	if err != nil {
		t.Fatalf("generate activation key: %v", err)
	}
	const issuer = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	domain, err := service.NewAuthority(model.PolicySnapshot{
		Version: 1, AuthorityABIVersion: model.AuthorityABIVersion,
		DefaultDecision: "DENY", TokenTTLSeconds: 30,
		AllowedClockSkewSeconds: 2, SourceRevision: 1,
		SourceDigestSHA256:      strings.Repeat("a", 64),
		PredecessorDigestSHA256: strings.Repeat("0", 64),
		KeySetRevision:          1, PolicyRevision: 1, SignerGeneration: 1,
		Issuer: issuer, SignerKeyID: key.KeyID,
	}, service.KeyMaterial{
		VerificationKeys: map[string]service.VerificationKeyRecord{
			key.KeyID: {
				Key: key.PublicOnly(), Issuer: issuer, Generation: 1,
				Status: "CURRENT", Purpose: "AUTHORIZATION_CONTEXT",
				Audiences: map[string]struct{}{"urn:kodex:test": {}},
				NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("create activation authority: %v", err)
	}
	return domain
}
