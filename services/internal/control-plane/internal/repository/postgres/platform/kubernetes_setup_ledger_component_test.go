package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryKubernetesSetupLedgerFixture creates schema-only synthetic rows under actual tenant FKs.
// queryKubernetesSetupLedgerFixture создаёт schema-only synthetic rows с actual tenant FKs.
//
//go:embed testdata/sql/kubernetes_setup_ledger_fixture.sql
var queryKubernetesSetupLedgerFixture string

// queryKubernetesSetupLedgerAttempt exercises the real single-write and bounded attempt constraints.
// queryKubernetesSetupLedgerAttempt проверяет real single-write и bounded attempt constraints.
//
//go:embed testdata/sql/kubernetes_setup_ledger_attempt.sql
var queryKubernetesSetupLedgerAttempt string

// queryKubernetesSetupLedgerWaitExpiry lets a short real lease expire without mutating immutable dates.
// queryKubernetesSetupLedgerWaitExpiry ожидает expiry короткого real lease без изменения immutable dates.
//
//go:embed testdata/sql/kubernetes_setup_ledger_wait_expiry.sql
var queryKubernetesSetupLedgerWaitExpiry string

// queryKubernetesSetupLedgerState advances the real setup row without replacing immutable input.
// queryKubernetesSetupLedgerState меняет real setup row без замены immutable input.
//
//go:embed testdata/sql/kubernetes_setup_ledger_state.sql
var queryKubernetesSetupLedgerState string

// queryKubernetesSetupLedgerAttemptClose closes exact fixture attempts inside their owner transaction.
// queryKubernetesSetupLedgerAttemptClose закрывает exact fixture attempts в owner transaction.
//
//go:embed testdata/sql/kubernetes_setup_ledger_attempt_close.sql
var queryKubernetesSetupLedgerAttemptClose string

// queryKubernetesSetupLedgerConstraints forces deferred closure checks before fixture rollback.
// queryKubernetesSetupLedgerConstraints запускает deferred closure checks до fixture rollback.
//
//go:embed testdata/sql/kubernetes_setup_ledger_constraints.sql
var queryKubernetesSetupLedgerConstraints string

// queryKubernetesSetupLedgerUnknown retains an uncertain external outcome without a fake locator.
// queryKubernetesSetupLedgerUnknown сохраняет uncertain external outcome без fake locator.
//
//go:embed testdata/sql/kubernetes_setup_ledger_unknown.sql
var queryKubernetesSetupLedgerUnknown string

// queryKubernetesSetupLedgerObservationRewrite deliberately attacks immutable retained evidence.
// queryKubernetesSetupLedgerObservationRewrite намеренно нарушает immutable retained evidence.
//
//go:embed testdata/sql/kubernetes_setup_ledger_observation_rewrite.sql
var queryKubernetesSetupLedgerObservationRewrite string

// queryKubernetesSetupLedgerOwner reads resolved fixture metadata, never user-selected authority.
// queryKubernetesSetupLedgerOwner читает resolved fixture metadata, не user-selected authority.
//
//go:embed testdata/sql/kubernetes_setup_ledger_owner.sql
var queryKubernetesSetupLedgerOwner string

// queryKubernetesSetupLedgerBindingState prepares historical safe metadata for the actual revoke command.
// queryKubernetesSetupLedgerBindingState подготавливает historical safe metadata для actual revoke command.
//
//go:embed testdata/sql/kubernetes_setup_ledger_binding_state.sql
var queryKubernetesSetupLedgerBindingState string

// queryKubernetesSetupLedgerConnectionChange exercises the actual existing Connection lifecycle trigger.
// queryKubernetesSetupLedgerConnectionChange проверяет actual existing Connection lifecycle trigger.
//
//go:embed testdata/sql/kubernetes_setup_ledger_connection_change.sql
var queryKubernetesSetupLedgerConnectionChange string

// queryKubernetesPersonalTestRewrite attacks immutable source metadata without a generic descriptor.
// queryKubernetesPersonalTestRewrite проверяет отказ изменения immutable source metadata без generic descriptor.
//
//go:embed testdata/sql/kubernetes_personal_test_rewrite.sql
var queryKubernetesPersonalTestRewrite string

// TestKubernetesIdentitySetupLedgerComponent checks actual PostgreSQL constraints, not an API mock.
// The schema-only Definition FK is not a Kubernetes package or caller-authority qualification.
// Positive Kubernetes admission, external CREATE and late evidence remain separate consumer tests.
// TestKubernetesIdentitySetupLedgerComponent проверяет actual PostgreSQL constraints, не API mock.
// Schema-only Definition FK не квалифицирует Kubernetes package либо caller authority.
// Positive Kubernetes admission, external CREATE и late evidence проверяются отдельно с consumer.
func TestKubernetesIdentitySetupLedgerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	fixture := newKubernetesSetupOwnerFixture(t, ctx)
	pool, repository, candidate := fixture.pool, fixture.repository, fixture.candidate
	principal, transportPrincipal, current := fixture.principal, fixture.transport, fixture.current
	beginInput := fixture.begin
	for _, scenario := range []string{"observe-before-write", "second-write", "partial-cancel", "unproven-complete", "expired-prepared", "cancel-observe", "immutable-unknown", "unreported-write", "unreported-observe", "dependency-cancel", "metadata-without-material", "broker-test-source-oneof", "connection-change-invalidates-broker", "verified-broker-revoke"} {
		t.Run(scenario, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated setup ledger transaction")
			}
			defer func() { _ = tx.Rollback(ctx) }()
			age := 0
			if scenario == "expired-prepared" {
				age = 100
			}
			setupID, original, snapshotHash := seedKubernetesSetupLedgerTx(t, ctx, tx, fixture, age)
			private, err := scanKubernetesUploadRow(tx.QueryRow(ctx, queryKubernetesSetupPrivatePermit, pgx.StrictNamedArgs{
				"setup_ref": original.SetupRef, "permit_digest": strings.Repeat("d", 64),
			}))
			if err != nil || private.input != original || private.id != setupID || private.owner.subjectID != current.actorID {
				t.Fatal("private setup reader lost its exact immutable owner snapshot")
			}
			var active bool
			if tx.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil || !active {
				t.Fatal("prepared or expired unclosed setup disappeared from operator drain boundary")
			}
			if scenario == "metadata-without-material" {
				var owner integrationBrokerIdentityWriteOwner
				if tx.QueryRow(ctx, queryKubernetesSetupLedgerOwner, pgx.StrictNamedArgs{"setup_id": setupID}).Scan(
					&owner.organizationID, &owner.connectionID, &owner.connectionRef, &owner.subjectID,
					&owner.bindingID, &owner.bindingRef, &owner.targetRef, &owner.connectionVersion,
					&owner.bindingVersion, &owner.bindingGeneration, &owner.policy, &owner.purpose,
				) != nil {
					t.Fatal("read exact fixture metadata owner")
				}
				identityID, metadata, err := insertIntegrationBrokerIdentityRevisionTx(ctx, tx, owner)
				if err != nil || metadata.Revision != 2 || metadata.Purpose != owner.purpose || metadata.TargetRef != owner.targetRef {
					t.Fatal("metadata allocation lost its immutable revision or owner pins")
				}
				if _, err := repository.attachIntegrationBrokerIdentityRevisionTx(ctx, tx, current, owner, identityID, time.Time{}); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("unmaterialized metadata was attached as an executable identity")
				}
				return
			}
			if scenario == "broker-test-source-oneof" {
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerBindingState, pgx.StrictNamedArgs{"setup_id": setupID, "state": "CHECK_REQUIRED"}); err != nil {
					t.Fatal("prepare synthetic broker source metadata")
				}
				binding, err := readPersonalCredentialTx(ctx, tx, current, "conn_setup_ledger")
				if err != nil || binding.BrokerIdentity == nil {
					t.Fatal("read exact synthetic broker verification source")
				}
				started, snapshot, err := platformservice.BeginPersonalIntegrationBrokerIdentityVerification(binding,
					entity.IntegrationCredentialInitiator{OrganizationID: current.organizationID, SubjectID: current.actorID, SubjectKind: "USER"}, binding.Version,
					entity.PersonalIntegrationCredentialTestSpec{Ref: "pct_broker_source_fixture", ConnectionVersion: binding.ConnectionVersion, DefinitionDigest: original.DefinitionDigest,
						ConfigurationDigest: original.ConfigurationDigest, HealthCapabilityKey: "kubernetes.identity", AllowedReadCapabilityKeys: []string{"kubernetes.identity"},
						BrokerPurpose: entity.IntegrationBrokerIdentityKubernetes, BrokerTargetRef: original.TargetRef})
				if err != nil || snapshot.CredentialRef != "" || snapshot.BrokerIdentityRef != binding.BrokerIdentity.Ref {
					t.Fatal("private verification source acquired a generic credential")
				}
				if persistPersonalCredentialVerification(ctx, tx, private.owner.bindingID, binding.Version, started) != nil {
					t.Fatal("persist exact broker checking generation")
				}
				encoded, err := json.Marshal(snapshot)
				if err != nil {
					t.Fatal("encode synthetic private verification metadata")
				}
				arguments := pgx.StrictNamedArgs{"ref": snapshot.Ref, "organization_id": current.organizationID,
					"binding_id": private.owner.bindingID, "subject_id": current.actorID, "version": started.Version, "snapshot": encoded}
				if tag, err := tx.Exec(ctx, queryPersonalCredentialTestInsert, arguments); err != nil || tag.RowsAffected() != 1 {
					t.Fatal("broker verification job required a fake token revision")
				}
				for _, attack := range []string{"foreign-source", "rewrite"} {
					child, err := tx.Begin(ctx)
					if err != nil {
						t.Fatal("begin isolated verification source counterfactual")
					}
					if attack == "foreign-source" {
						wrong := snapshot
						wrong.Ref, wrong.BrokerIdentityRef = "pct_foreign_source_fixture", "bki_foreign_source"
						arguments["ref"] = wrong.Ref
						arguments["snapshot"], _ = json.Marshal(wrong)
						_, err = child.Exec(ctx, queryPersonalCredentialTestInsert, arguments)
					} else {
						_, err = child.Exec(ctx, queryKubernetesPersonalTestRewrite, snapshot.Ref)
					}
					if err == nil {
						t.Fatal("foreign or mutable broker test source passed native guards")
					}
					if child.Rollback(ctx) != nil {
						t.Fatal("rollback rejected broker test source counterfactual")
					}
				}
				return
			}
			attempt := func(mode string, generation int) (string, error) {
				var ref string
				fenceHash := sha256.Sum256([]byte("kuf_setup_ledger_" + mode + "_" + strconv.Itoa(generation)))
				duration := 30000
				if scenario == "unreported-write" && mode == "WRITE" || scenario == "unreported-observe" && mode == "OBSERVE" {
					duration = 250
				}
				err := tx.QueryRow(ctx, queryKubernetesSetupLedgerAttempt, pgx.StrictNamedArgs{
					"setup_id": setupID, "ref": "ksa_setup_ledger_" + strings.Repeat("a", generation),
					"generation": generation, "mode": mode, "fence_sha256": hex.EncodeToString(fenceHash[:]),
					"duration_milliseconds": duration,
				}).Scan(&ref)
				return ref, err
			}
			state := func(next string) {
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerState, pgx.StrictNamedArgs{"setup_id": setupID, "state": next}); err != nil {
					t.Fatal("advance synthetic setup ledger")
				}
			}
			closeAttempt := func(next string) {
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerAttemptClose, pgx.StrictNamedArgs{"setup_id": setupID, "state": next}); err != nil {
					t.Fatal("close synthetic upload attempt")
				}
			}
			unreportedClosure := func() {
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerWaitExpiry); err != nil {
					t.Fatal("wait for the short native attempt lease")
				}
				args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "setup_id": setupID}
				if _, err := tx.Exec(ctx, queryKubernetesSetupExpireAttempts, args); err != nil {
					t.Fatal("close expired native attempt grant")
				}
				var granted, wrote, reported bool
				if tx.QueryRow(ctx, queryKubernetesSetupAttemptBoundary, args).Scan(&granted, &wrote, &reported) != nil || granted || !wrote || reported {
					t.Fatal("lease expiry manufactured a classified outcome or another observation credit")
				}
				if err := cancelKubernetesIdentitySetupsTx(ctx, tx, current.organizationID, fixture.begin.ConnectionRef, ""); err != nil {
					t.Fatal("cancel unreported authority without erasing its outcome")
				}
				view, err := readKubernetesSetupView(tx.QueryRow(ctx, queryKubernetesSetupPrivateView, args))
				if err != nil || view.State != "CANCELLED" || view.ReasonCode != kubernetesSetupCompletionRequired {
					t.Fatal("cancelled metadata forgot unreported material history")
				}
			}
			if scenario == "observe-before-write" {
				if _, err := attempt("OBSERVE", 1); err == nil {
					t.Fatal("observation without an original write intent was admitted")
				}
				return
			}
			if scenario == "expired-prepared" {
				if _, err := attempt("WRITE", 1); err == nil {
					t.Fatal("expired setup obtained write credit")
				}
				return
			}
			writeID, err := attempt("WRITE", 1)
			if err != nil {
				t.Fatal("admit sole synthetic write attempt")
			}
			state("ATTEMPTING")
			if scenario == "unreported-write" {
				unreportedClosure()
				return
			}
			if scenario == "verified-broker-revoke" || scenario == "connection-change-invalidates-broker" {
				for _, next := range []string{"CHECK_REQUIRED", "CHECKING", "READY"} {
					if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerBindingState, pgx.StrictNamedArgs{"setup_id": setupID, "state": next}); err != nil {
						var postgresError *pgconn.PgError
						if errors.As(err, &postgresError) {
							t.Fatalf("historical metadata transition %s rejected: SQLSTATE=%s constraint=%s", next, postgresError.Code, postgresError.ConstraintName)
						}
						t.Fatal("seed historical verified broker metadata")
					}
				}
				if scenario == "connection-change-invalidates-broker" {
					if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerConnectionChange); err != nil {
						t.Fatal("Connection lifecycle retained mixed verification or active setup authority")
					}
					binding, err := readPersonalCredentialTx(ctx, tx, current, "conn_setup_ledger")
					if err != nil || binding.State != "CHECK_REQUIRED" || binding.VerifiedBrokerIdentityRef != "" || binding.Generation != 2 {
						t.Fatal("Connection change failed to invalidate broker verification without rewriting generation")
					}
					if tx.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil || active {
						t.Fatal("Connection change retained setup or attempt authority")
					}
					if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerConstraints); err != nil {
						t.Fatal("Connection change violated complete terminal grant closure")
					}
					return
				}
				if tx.Commit(ctx) != nil {
					t.Fatal("commit historical fixture for the actual owner command")
				}
				reader := principal
				reader.Permission = "platform.integrations.kubernetes.setup.get"
				before, err := repository.GetKubernetesIdentitySetup(ctx, reader, command.KubernetesIdentitySetupRead{
					ConnectionRef: "conn_setup_ledger", SetupRef: "ksu_setup_ledger_fixture"})
				if err != nil || before.State != "ATTEMPTING" || before.BindingVersion != 4 || before.BindingGeneration != 2 {
					t.Fatal("addressed setup reader lost current owner metadata")
				}
				broker := reader
				broker.CallerWorkload, broker.Permission = "secret-broker", "platform.runtime.kubernetes.setup.resolve"
				resolveInput := command.KubernetesIdentityUploadResolve{SetupRef: before.Ref, PermitSHA256: strings.Repeat("d", 64)}
				wrongPermit := resolveInput
				wrongPermit.PermitSHA256 = strings.Repeat("c", 64)
				if _, err := repository.ResolveKubernetesIdentityUpload(ctx, broker, wrongPermit); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("unmatched permit selected an original owner or private material")
				}
				if _, err := repository.ResolveKubernetesIdentityUpload(ctx, broker, resolveInput); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("private resolver accepted a TOKEN package as Kubernetes authority")
				}
				broker.Permission = "platform.runtime.kubernetes.setup.claim"
				if _, err := repository.ClaimKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadClaim{
					SetupRef: before.Ref, PermitSHA256: resolveInput.PermitSHA256, SnapshotDigest: hex.EncodeToString(snapshotHash[:]),
					ContentSHA256: original.ContentDigest, WorkloadInstance: "broker_fixture_instance",
				}); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("private claim issued storage credit under a TOKEN package")
				}
				rejoined, err := repository.GetKubernetesIdentitySetup(ctx, reader, command.KubernetesIdentitySetupRead{
					ConnectionRef: "conn_setup_ledger", BeginIdempotencyKey: "setup-ledger-fixture"})
				if err != nil || rejoined.Ref != before.Ref || rejoined.IdentityRef != before.IdentityRef || rejoined.Version != before.Version {
					t.Fatal("lost Begin response could not rejoin its original metadata without another permit")
				}
				beginner := reader
				beginner.Permission = kubernetesSetupBeginOperation
				originalBindingVersion := int64(1)
				beginMutation := value.Mutation{IdempotencyKey: "setup-ledger-fixture", ExpectedVersion: &originalBindingVersion}
				admission, err := repository.BeginKubernetesIdentitySetup(ctx, beginner, beginMutation, beginInput)
				if err != nil || !admission.ReconcileOnly || len(admission.Permit) != 0 || admission.Endpoint.ServerOrigin != "" || admission.View.Ref != before.Ref {
					t.Fatal("Begin replay issued another permit or required the current package to decode safe history")
				}
				changedBegin := beginInput
				changedBegin.ContentBytes++
				if _, err := repository.BeginKubernetesIdentitySetup(ctx, beginner, beginMutation, changedBegin); !errors.Is(err, errs.ErrIdempotencyReuse) {
					t.Fatal("Begin receipt accepted changed immutable content intent")
				}
				beginMutation.IdempotencyKey = "setup-ledger-not-kubernetes"
				beginMutation.ExpectedVersion = &before.BindingVersion
				if _, err := repository.BeginKubernetesIdentitySetup(ctx, beginner, beginMutation, beginInput); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("TOKEN package admitted a new Kubernetes setup")
				}
				if _, err := repository.GetKubernetesIdentitySetup(ctx, reader, command.KubernetesIdentitySetupRead{
					ConnectionRef: beginInput.ConnectionRef, BeginIdempotencyKey: beginMutation.IdempotencyKey}); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("rejected non-Kubernetes Begin retained a partial setup intent")
				}
				foreign := reader
				foreign.CallerWorkload = "integration-gateway"
				if _, err := repository.GetKubernetesIdentitySetup(ctx, foreign, command.KubernetesIdentitySetupRead{
					ConnectionRef: "conn_setup_ledger", SetupRef: before.Ref}); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("another workload read owner-local setup metadata")
				}
				canceller := reader
				canceller.Permission = kubernetesSetupCancelOperation
				cancelInput := command.KubernetesIdentitySetupCancel{ConnectionRef: "conn_setup_ledger", SetupRef: before.Ref}
				staleVersion := before.Version + 1
				cancelMutation := value.Mutation{IdempotencyKey: "setup-ledger-owner-cancel", ExpectedVersion: &staleVersion}
				if _, err := repository.CancelKubernetesIdentitySetup(ctx, canceller, cancelMutation, cancelInput); !errors.Is(err, errs.ErrVersionMismatch) {
					t.Fatal("stale cancellation changed setup authority")
				}
				if pool.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil || !active {
					t.Fatal("failed cancellation closed an active upload attempt")
				}
				missing := cancelInput
				missing.SetupRef = "ksu_unknown_fixture"
				if _, err := repository.CancelKubernetesIdentitySetup(ctx, canceller, cancelMutation, missing); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("cancellation checked OCC before resolving the owned setup")
				}
				cancelMutation.ExpectedVersion = &before.Version
				cancelled, err := repository.CancelKubernetesIdentitySetup(ctx, canceller, cancelMutation, cancelInput)
				if err != nil || cancelled.State != "CANCELLED" || cancelled.Version != before.Version+1 || cancelled.IdentityRef != before.IdentityRef {
					t.Fatal("addressed cancellation failed to retain identity history and close setup")
				}
				if pool.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil || active {
					t.Fatal("addressed cancellation retained an active attempt")
				}
				replayed, err := repository.CancelKubernetesIdentitySetup(ctx, canceller, cancelMutation, cancelInput)
				if err != nil || replayed.Ref != cancelled.Ref || replayed.State != cancelled.State ||
					replayed.Version != cancelled.Version || replayed.BindingGeneration != cancelled.BindingGeneration ||
					!replayed.ExpiresAt.Equal(cancelled.ExpiresAt) {
					t.Fatal("lost cancellation response failed to replay its exact receipt before OCC")
				}
				cancelMutation.ExpectedVersion = &cancelled.Version
				if _, err := repository.CancelKubernetesIdentitySetup(ctx, canceller, cancelMutation, cancelInput); !errors.Is(err, errs.ErrIdempotencyReuse) {
					t.Fatal("cancellation receipt accepted a changed version intent")
				}
				cancelMutation.IdempotencyKey = "setup-ledger-owner-cancel-noop"
				if noop, err := repository.CancelKubernetesIdentitySetup(ctx, canceller, cancelMutation, cancelInput); err != nil || noop.Version != cancelled.Version {
					t.Fatal("cancelled setup required another write or advanced its version")
				}
				broker.Permission = "platform.runtime.kubernetes.setup.complete"
				completion := command.KubernetesIdentityUploadComplete{SetupRef: before.Ref, AttemptRef: "ksa_setup_ledger_a",
					Generation: 1, WorkloadInstance: "synthetic-broker", Fence: "kuf_setup_ledger_WRITE_1", Unknown: true}
				foreignCompletion := completion
				foreignCompletion.Fence = "kuf_foreign_ledger_fence"
				if _, err := repository.CompleteKubernetesIdentityUpload(ctx, broker, foreignCompletion); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("an unrelated fence reported the original material outcome")
				}
				unknown, err := repository.CompleteKubernetesIdentityUpload(ctx, broker, completion)
				if err != nil || unknown.State != "CANCELLED" || unknown.Version != cancelled.Version || unknown.BindingGeneration != 2 {
					t.Fatal("late UNKNOWN evidence revived cancelled authority or changed the binding")
				}
				secretName, err := managedkubernetes.IdentitySecretName(before.IdentityRef, before.IdentityRevision)
				if err != nil {
					t.Fatal("derive synthetic immutable material name")
				}
				completion.Unknown = false
				completion.Identity = &value.ManagedKubernetesIdentityLocator{IdentityRef: before.IdentityRef, TargetRef: before.TargetRef,
					Revision: before.IdentityRevision, Namespace: candidate.IdentityNamespace, Name: secretName,
					UID: uuid.NewString(), ResourceVersion: "synthetic_resource_version", ContentSHA256: beginInput.ContentSHA256}
				known, err := repository.CompleteKubernetesIdentityUpload(ctx, broker, completion)
				if err != nil || known.State != "CANCELLED" || known.Version != cancelled.Version || known.BindingGeneration != 2 {
					t.Fatal("late KNOWN evidence rebound cancelled setup or rewrote its immutable history")
				}
				replacement := *completion.Identity
				replacement.UID = uuid.NewString()
				foreignCompletion = completion
				foreignCompletion.Identity = &replacement
				if _, err := repository.CompleteKubernetesIdentityUpload(ctx, broker, foreignCompletion); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("a later completion adopted a replacement private Secret UID")
				}
				service, err := platformservice.New(repository)
				if err != nil {
					t.Fatal("compose actual revoke owner service")
				}
				actor := transportPrincipal
				actor.Permission = "platform.personal-integrations.revoke"
				version := int64(4)
				result, err := service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: actor,
					Mutation: value.Mutation{IdempotencyKey: "historical-broker-revoke", ExpectedVersion: &version},
					Payload:  command.PersonalIntegrationCredentialInput{ConnectionRef: "conn_setup_ledger"}})
				if err != nil || result.PersonalIntegrationCredential == nil || result.PersonalIntegrationCredential.State != "REVOKED" ||
					result.PersonalIntegrationCredential.VerifiedBrokerIdentityRef != "" || result.PersonalIntegrationCredential.Generation != 3 {
					t.Fatal("actual revoke failed to fence a verified historical broker identity")
				}
				if pool.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil || active {
					t.Fatal("actual revoke left setup or attempt authority active")
				}
				after, err := repository.GetKubernetesIdentitySetup(ctx, reader, command.KubernetesIdentitySetupRead{
					ConnectionRef: "conn_setup_ledger", SetupRef: before.Ref})
				if err != nil || after.State != "CANCELLED" || after.IdentityRef != before.IdentityRef ||
					after.BindingGeneration != 3 || after.Version <= before.Version {
					t.Fatal("addressed setup history hid cancellation or rewrote its identity metadata")
				}
				if late, err := repository.CompleteKubernetesIdentityUpload(ctx, broker, completion); err != nil || late.State != "CANCELLED" || late.BindingGeneration != 3 {
					t.Fatal("replayed material evidence revived a revoked binding generation")
				}
				return
			}
			if scenario == "partial-cancel" {
				state("CANCELLED")
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerConstraints); err == nil {
					t.Fatal("terminal setup retained an active attempt at its transaction boundary")
				}
				return
			}
			closeAttempt("UNKNOWN")
			state("UNKNOWN")
			if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerUnknown, pgx.StrictNamedArgs{"attempt_id": writeID}); err != nil {
				t.Fatal("retain uncertain write evidence")
			}
			switch scenario {
			case "unreported-observe":
				if _, err := attempt("OBSERVE", 2); err != nil {
					t.Fatal("admit short synthetic observation attempt")
				}
				unreportedClosure()
			case "second-write":
				if _, err := attempt("WRITE", 2); err == nil {
					t.Fatal("another generation recreated the original material write credit")
				}
			case "unproven-complete":
				state("COMPLETED")
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerConstraints); err == nil {
					t.Fatal("UNKNOWN write was promoted to materialized completion")
				}
			case "immutable-unknown":
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerObservationRewrite, pgx.StrictNamedArgs{"attempt_id": writeID}); err == nil {
					t.Fatal("uncertain material history was erased")
				}
			case "cancel-observe":
				if _, err := attempt("OBSERVE", 2); err != nil {
					t.Fatal("read-only reconciliation after uncertain write was rejected")
				}
				closeAttempt("CANCELLED")
				state("CANCELLED")
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerConstraints); err != nil {
					t.Fatal("atomic cancellation failed its complete grant closure")
				}
				if tx.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil || active {
					t.Fatal("cancelled authority was confused with retained UNKNOWN material history")
				}
				if _, err := attempt("OBSERVE", 3); err == nil {
					t.Fatal("cancelled setup regained an observation grant")
				}
			case "dependency-cancel":
				if _, err := attempt("OBSERVE", 2); err != nil {
					t.Fatal("admit synthetic read-only reconciliation")
				}
				if err := cancelKubernetesIdentitySetupsTx(ctx, tx, current.organizationID, "conn_setup_ledger", ""); err != nil {
					t.Fatal("close exact Connection setup dependency")
				}
				if _, err := tx.Exec(ctx, queryKubernetesSetupLedgerConstraints); err != nil {
					t.Fatal("dependency cancellation retained a partial terminal envelope")
				}
				if tx.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil || active {
					t.Fatal("dependency cancellation left setup or attempt authority active")
				}
			}
		})
	}
}
