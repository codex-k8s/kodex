package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesSetupLedgerMoveIdentity sets only synthetic metadata for the moved-source negative.
// queryKubernetesSetupLedgerMoveIdentity задаёт лишь synthetic metadata для moved-source negative.
//
//go:embed testdata/sql/kubernetes_setup_ledger_move_identity.sql
var queryKubernetesSetupLedgerMoveIdentity string

// TestKubernetesIdentityAcknowledgementConflictOwnerComponent exercises KNOWN-before-rejection order.
// Historical synthetic READY metadata is not package/RBAC qualification; the actual owner callback
// must revoke only a still-current original RI and preserve a subsequently selected different RI.
// TestKubernetesIdentityAcknowledgementConflictOwnerComponent проверяет порядок KNOWN-before-rejection.
// Historical synthetic READY metadata не квалифицирует package/RBAC; actual owner callback обязан
// отозвать лишь ещё текущую original RI и сохранить subsequently selected different RI.
func TestKubernetesIdentityAcknowledgementConflictOwnerComponent(t *testing.T) {
	for _, moved := range []bool{false, true} {
		name := "current-original"
		if moved {
			name = "moved-source"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			fixture := newKubernetesSetupOwnerFixture(t, ctx)
			tx, err := fixture.pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin acknowledgement conflict fixture")
			}
			defer func() { _ = tx.Rollback(ctx) }()
			setupID, original, _ := seedKubernetesSetupLedgerTx(t, ctx, tx, fixture, 0)
			fence := "kuf_ack_conflict_write"
			fenceHash := sha256.Sum256([]byte(fence))
			var attemptID string
			if tx.QueryRow(ctx, queryKubernetesSetupLedgerAttempt, pgx.StrictNamedArgs{
				"setup_id": setupID, "ref": "ksa_ack_conflict_write", "generation": 1, "mode": "WRITE", "fence_sha256": hex.EncodeToString(fenceHash[:]),
				"duration_milliseconds": 30000,
			}).Scan(&attemptID) != nil {
				t.Fatal("admit synthetic conflict write")
			}
			if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerState, pgx.StrictNamedArgs{"setup_id": setupID, "state": "ATTEMPTING"}); err != nil {
				t.Fatal("record synthetic conflict write admission")
			}
			for _, state := range []string{"CHECK_REQUIRED", "CHECKING", "READY"} {
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerBindingState, pgx.StrictNamedArgs{"setup_id": setupID, "state": state}); err != nil {
					t.Fatal("seed synthetic historical verified source")
				}
			}
			private, err := scanKubernetesUploadRow(tx.QueryRow(ctx, queryKubernetesSetupPrivatePermit, pgx.StrictNamedArgs{
				"setup_ref": original.SetupRef, "permit_digest": strings.Repeat("d", 64),
			}))
			if err != nil {
				t.Fatal("read original immutable conflict owner")
			}
			secretName, err := managedkubernetes.IdentitySecretName(original.IdentityRef, original.IdentityRevision)
			if err != nil {
				t.Fatal("derive synthetic conflict material name")
			}
			locator := value.ManagedKubernetesIdentityLocator{IdentityRef: original.IdentityRef, TargetRef: original.TargetRef,
				Revision: original.IdentityRevision, Namespace: fixture.candidate.IdentityNamespace, Name: secretName,
				UID: uuid.NewString(), ResourceVersion: "synthetic_known_revision", ContentSHA256: original.ContentDigest}
			if _, err := recordKubernetesUploadFactTx(ctx, tx, private, attemptID, command.KubernetesIdentityUploadComplete{Identity: &locator}); err != nil {
				t.Fatal("retain synthetic original known fact")
			}
			if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerAttemptClose, pgx.StrictNamedArgs{"setup_id": setupID, "state": "COMPLETED"}); err != nil {
				t.Fatal("close known synthetic attempt")
			}
			if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerState, pgx.StrictNamedArgs{"setup_id": setupID, "state": "COMPLETED"}); err != nil {
				t.Fatal("retain synthetic completed setup history")
			}
			if moved {
				owner := private.owner
				owner.bindingVersion, owner.bindingGeneration = 4, 2
				identityID, _, err := insertIntegrationBrokerIdentityRevisionTx(ctx, tx, owner)
				if err != nil {
					t.Fatal("allocate synthetic subsequent metadata revision")
				}
				if tag, err := tx.Exec(ctx, queryKubernetesSetupLedgerMoveIdentity, pgx.StrictNamedArgs{"identity_id": identityID}); err != nil || tag.RowsAffected() != 1 {
					t.Fatal("move synthetic source before delayed negative acknowledgement")
				}
			}
			if tx.Commit(ctx) != nil {
				t.Fatal("commit synthetic known-before-rejection boundary")
			}
			before, err := fixture.repository.GetPersonalIntegrationCredential(ctx, fixture.principal, fixture.begin.ConnectionRef)
			if err != nil || before.BrokerIdentity == nil {
				t.Fatal("read current synthetic source before negative callback")
			}
			broker := fixture.principal
			broker.CallerWorkload, broker.Permission = "secret-broker", "platform.runtime.kubernetes.setup.complete"
			witness := value.ManagedKubernetesAcknowledgementRejection{ResponseSHA256: strings.Repeat("a", 64), ObservedUID: uuid.NewString()}
			input := command.KubernetesIdentityUploadComplete{SetupRef: original.SetupRef, AttemptRef: "ksa_ack_conflict_write", Generation: 1,
				Fence: fence, WorkloadInstance: "synthetic-broker", RejectedAcknowledgement: &witness}
			result, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, input)
			if err != nil || result.State != "COMPLETED" || result.ReasonCode != kubernetesSetupAcknowledgementConflict {
				t.Fatal("late negative acknowledgement erased history or lost its conflict classification")
			}
			after, err := fixture.repository.GetPersonalIntegrationCredential(ctx, fixture.principal, fixture.begin.ConnectionRef)
			if err != nil || after.BrokerIdentity == nil || after.BrokerIdentity.Ref != before.BrokerIdentity.Ref {
				t.Fatal("negative acknowledgement replaced current identity metadata")
			}
			if moved {
				if after.State != before.State || after.Generation != before.Generation || after.Version != before.Version {
					t.Fatal("old negative acknowledgement revoked a subsequently selected identity")
				}
			} else if after.State != "REVOKED" || after.Generation != before.Generation+1 || after.Version != before.Version+1 || after.VerifiedBrokerIdentityRef != "" {
				t.Fatal("conflicting acknowledgement retained usable original authority")
			}
			if _, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, input); err != nil {
				t.Fatal("negative conflict receipt replay failed")
			}
			replayed, err := fixture.repository.GetPersonalIntegrationCredential(ctx, fixture.principal, fixture.begin.ConnectionRef)
			if err != nil || replayed.Generation != after.Generation || replayed.Version != after.Version {
				t.Fatal("replayed conflict advanced or revoked a new owner generation")
			}
		})
	}
}
