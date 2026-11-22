package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestKubernetesIdentityAcknowledgementOwnerComponent verifies actual terminal receipt and grant closure.
// It uses synthetic admitted rows, not a positive Kubernetes package or external Secret qualification.
// TestKubernetesIdentityAcknowledgementOwnerComponent проверяет actual terminal receipt и grant closure.
// Использует synthetic admitted rows, не positive Kubernetes package либо external Secret qualification.
func TestKubernetesIdentityAcknowledgementOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	fixture := newKubernetesSetupOwnerFixture(t, ctx)
	tx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin acknowledgement owner fixture")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	setupID, original, snapshotHash := seedKubernetesSetupLedgerTx(t, ctx, tx, fixture, 0)
	writeFence, observeFence := "kuf_ack_fixture_write", "kuf_ack_fixture_observe"
	var writeID string
	for index, mode := range []string{"WRITE", "OBSERVE"} {
		fence, ref := writeFence, "ksa_ack_fixture_write"
		if index == 1 {
			fence, ref = observeFence, "ksa_ack_fixture_observe"
		}
		fenceHash := sha256.Sum256([]byte(fence))
		var attemptID string
		if tx.QueryRow(ctx, queryKubernetesSetupLedgerAttempt, pgx.StrictNamedArgs{
			"setup_id": setupID, "ref": ref, "generation": index + 1, "mode": mode, "fence_sha256": hex.EncodeToString(fenceHash[:]),
			"duration_milliseconds": 30000,
		}).Scan(&attemptID) != nil {
			t.Fatal("admit synthetic acknowledgement attempt")
		}
		if index == 0 {
			writeID = attemptID
			if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerState, pgx.StrictNamedArgs{"setup_id": setupID, "state": "ATTEMPTING"}); err != nil {
				t.Fatal("record original write admission")
			}
			if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerAttemptClose, pgx.StrictNamedArgs{"setup_id": setupID, "state": "UNKNOWN"}); err != nil {
				t.Fatal("close original synthetic transport loss")
			}
			if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerUnknown, pgx.StrictNamedArgs{"attempt_id": writeID}); err != nil {
				t.Fatal("retain original classified unknown receipt")
			}
			if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerState, pgx.StrictNamedArgs{"setup_id": setupID, "state": "UNKNOWN"}); err != nil {
				t.Fatal("retain original uncertain setup")
			}
		}
	}
	if tx.Commit(ctx) != nil {
		t.Fatal("commit synthetic owner acknowledgement boundary")
	}
	broker := fixture.principal
	broker.CallerWorkload, broker.Permission = "secret-broker", "platform.runtime.kubernetes.setup.complete"
	witness := value.ManagedKubernetesAcknowledgementRejection{ResponseSHA256: strings.Repeat("a", 64), ObservedUID: uuid.NewString()}
	completion := command.KubernetesIdentityUploadComplete{SetupRef: original.SetupRef, AttemptRef: "ksa_ack_fixture_write", Generation: 1,
		Fence: writeFence, WorkloadInstance: "synthetic-broker", RejectedAcknowledgement: &witness}
	result, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, completion)
	if err != nil || result.State != "ACK_REJECTED" || result.ReasonCode != kubernetesSetupAcknowledgementRejected {
		t.Fatal("rejected acknowledgement failed to close the original setup")
	}
	var active bool
	if fixture.pool.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil || active {
		t.Fatal("rejected acknowledgement retained another active observation grant")
	}
	if replayed, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, completion); err != nil || replayed.Version != result.Version || replayed.State != result.State {
		t.Fatal("lost rejection completion could not rejoin its immutable receipt")
	}
	changed := witness
	changed.ResponseSHA256 = strings.Repeat("b", 64)
	changedCompletion := completion
	changedCompletion.RejectedAcknowledgement = &changed
	if _, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, changedCompletion); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("rejection replay replaced its private witness")
	}
	changedCompletion.RejectedAcknowledgement, changedCompletion.Unknown = nil, true
	if _, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, changedCompletion); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("rejected successful acknowledgement became observation-eligible UNKNOWN")
	}
	broker.Permission = "platform.runtime.kubernetes.setup.claim"
	if _, err := fixture.repository.ClaimKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadClaim{
		SetupRef: original.SetupRef, PermitSHA256: strings.Repeat("d", 64), SnapshotDigest: hex.EncodeToString(snapshotHash[:]),
		ContentSHA256: original.ContentDigest, WorkloadInstance: "synthetic-broker",
	}); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("terminal rejection admitted another create or observation")
	}
	binding, err := fixture.repository.GetPersonalIntegrationCredential(ctx, fixture.principal, fixture.begin.ConnectionRef)
	if err != nil || binding.Generation != 1 || binding.Credential != nil || binding.BrokerIdentity != nil || binding.State != "NOT_CONFIGURED" {
		t.Fatal("rejected acknowledgement became a configured credential")
	}
}
