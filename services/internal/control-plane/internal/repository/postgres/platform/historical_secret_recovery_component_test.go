package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestHistoricalSecretRecoveryConcurrencyComponent verifies both lock winners, terminal-phase routing and storage rollback for legacy and draft producers using native commands, backend lock metadata and joined pgx tracers. No business-state/clock SQL, model, provider or Kubernetes operation is performed.
// TestHistoricalSecretRecoveryConcurrencyComponent проверяет обоих победителей блокировки, terminal-phase routing и storage rollback для legacy/draft producers через native commands, backend lock metadata и joined pgx tracers. Business-state/clock SQL, model/provider/Kubernetes operations не используются.
func TestHistoricalSecretRecoveryConcurrencyComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("construct disposable recovery concurrency pool")
	}
	defer pool.Close()
	r := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap recovery concurrency")
	}
	seedObservedCatalogFixture(t, ctx, r)
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	owner.CredentialAuthenticatedAt = time.Now().UTC()
	owner.CredentialACR = "urn:kodex:acr:interactive"
	owner.CredentialAMR = []string{"pwd"}
	for _, producer := range []string{"legacy", "draft"} {
		t.Run(producer+"-admission-wins", func(t *testing.T) {
			f := newHistoricalRecoveryFixture(t, ctx, r, s, owner, producer, producer+"-admission-wins", true)
			f.prepareHistorical(t, ctx)
			principal := f.recoveryPrincipal(t, ctx)
			publisher, releasePublisher, published := startHistoricalSecretCall(t, ctx, r, "runtime_secret_resolve_binding", false, f.publishR1)
			publisherPID := awaitHistoricalSecretPhase(t, ctx, publisher, published)
			phase := "runtime_secret_operation_lock"
			if producer == "draft" {
				phase = "secret_draft_lock"
			}
			recovery, _, recovered := startHistoricalSecretCall(t, ctx, r, phase, true, func(ctx context.Context, s *platformservice.Service) (string, error) {
				return f.recover(ctx, s, principal, f.material)
			})
			recoveryPID := awaitHistoricalSecretPhase(t, ctx, recovery, recovered)
			awaitHistoricalSecretLock(t, ctx, pool, recoveryPID, publisherPID, recovered)
			releasePublisher()
			publishResult := awaitHistoricalSecretResult(t, ctx, published)
			if publishResult.err != nil || publishResult.action != "PUBLISHED" {
				t.Fatalf("winning native publication did not commit: %v", publishResult.err)
			}
			result := awaitHistoricalSecretResult(t, ctx, recovered)
			if result.err != nil || result.action != "KEEP" {
				t.Fatalf("recovery missed committed winning reference: action=%s err=%v", result.action, result.err)
			}
			before := f.snapshot(t, ctx)
			for range 2 {
				action, err := f.recover(ctx, s, principal, f.material)
				if err != nil || action != "KEEP" {
					t.Fatalf("old published operation lost retained material: %v", err)
				}
			}
			if f.snapshot(t, ctx) != before {
				t.Fatal("settled KEEP replay changed owner state or appended effects")
			}
		})
		t.Run(producer+"-recovery-wins", func(t *testing.T) {
			f := newHistoricalRecoveryFixture(t, ctx, r, s, owner, producer, producer+"-recovery-wins", true)
			f.prepareHistorical(t, ctx)
			f.bindEmpty(t, ctx)
			principal := f.recoveryPrincipal(t, ctx)
			trace, release, recovered := startHistoricalSecretCall(t, ctx, r, "runtime_secret_revision_retained", false, func(ctx context.Context, s *platformservice.Service) (string, error) {
				return f.recover(ctx, s, principal, f.material)
			})
			awaitHistoricalSecretPhase(t, ctx, trace, recovered)
			var before string
			if err := pool.QueryRow(ctx, historicalSecretAdmissionSnapshot, f.environment.Ref, f.agentRef).Scan(&before); err != nil {
				t.Fatal("read inverse admission snapshot")
			}
			if err := f.rebindR1(t, ctx); !errors.Is(err, errs.ErrUnavailable) {
				t.Fatalf("contended exact admission must remain temporary: %v", err)
			}
			var after string
			if err := pool.QueryRow(ctx, historicalSecretAdmissionSnapshot, f.environment.Ref, f.agentRef).Scan(&after); err != nil || after != before {
				t.Fatal("contended admission leaked owner effects")
			}
			release()
			result := awaitHistoricalSecretResult(t, ctx, recovered)
			if result.err != nil || result.action != "DELETE" {
				t.Fatalf("winning native retirement did not commit: %v", result.err)
			}
			if err := pool.QueryRow(ctx, historicalSecretAdmissionSnapshot, f.environment.Ref, f.agentRef).Scan(&before); err != nil {
				t.Fatal("read post-retirement admission snapshot")
			}
			if err := f.rebindR1(t, ctx); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("fresh admission resurrected retired R1: %v", err)
			}
			if err := pool.QueryRow(ctx, historicalSecretAdmissionSnapshot, f.environment.Ref, f.agentRef).Scan(&after); err != nil || after != before {
				t.Fatal("retired admission changed ENV or binding effects")
			}
			if producer == "draft" {
				ackHistoricalDraftMaterial(t, ctx, f)
			}
		})
		t.Run(producer+"-probe-transition", func(t *testing.T) { testHistoricalProbeTransition(t, ctx, r, s, owner, producer, false) })
		t.Run(producer+"-fault-rollback", func(t *testing.T) {
			f := newHistoricalRecoveryFixture(t, ctx, r, s, owner, producer, producer+"-fault-rollback", true)
			f.prepareHistorical(t, ctx)
			principal := f.recoveryPrincipal(t, ctx)
			faults := []struct {
				name        string
				query       *string
				replacement string
			}{{"descriptor-read", &queryRuntimeSecretRevisionGet, historicalSecretDescriptorFault}, {"retention-read", &queryRuntimeSecretRevisionRetained, historicalSecretRetentionFault}, {"retire-write", &queryRuntimeSecretRetireRevision, historicalSecretRetireFault}}
			if producer == "draft" {
				faults = append(faults, struct {
					name        string
					query       *string
					replacement string
				}{"cleanup-intent-after-retirement", &querySecretDraftCleanupIntent, historicalSecretIntentFault})
			}
			for _, fault := range faults {
				t.Run(fault.name, func(t *testing.T) {
					before := f.snapshot(t, ctx)
					original := *fault.query
					*fault.query = fault.replacement
					defer func() { *fault.query = original }()
					action, err := f.recover(ctx, s, principal, f.material)
					if !errors.Is(err, errs.ErrUnavailable) || action == "DELETE" {
						t.Fatalf("storage fault authorized cleanup or fallback: action=%s err=%v", action, err)
					}
					if f.snapshot(t, ctx) != before {
						t.Fatal("storage fault leaked retirement, cleanup intent or audit")
					}
				})
			}
			if producer == "draft" {
				testHistoricalDraftClaimPins(t, ctx, f)
			}
		})
	}
	t.Run("legacy-probe-transition-mismatch", func(t *testing.T) { testHistoricalProbeTransition(t, ctx, r, s, owner, "legacy", true) })
}

// testHistoricalProbeTransition completes real CLAIMED work while the nonterminal RC probe is already rolled back, then proves the SERIALIZABLE recheck returns Conflict before any terminal/default-DELETE routing.
// testHistoricalProbeTransition завершает настоящее CLAIMED work после rollback nonterminal RC probe и доказывает Conflict при SERIALIZABLE recheck до любого terminal/default-DELETE routing.
func testHistoricalProbeTransition(t *testing.T, ctx context.Context, r *Repository, s *platformservice.Service, owner value.Principal, producer string, mismatch bool) {
	t.Helper()
	key := producer + "-probe-transition"
	if mismatch {
		key += "-mismatch"
	}
	f := newHistoricalRecoveryFixture(t, ctx, r, s, owner, producer, key, false)
	principal := f.recoveryPrincipal(t, ctx)
	before := f.snapshot(t, ctx)
	action, err := f.recover(ctx, s, principal, f.material)
	if producer == "legacy" && (err != nil || action != "KEEP") || producer == "draft" && !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("unchanged CLAIMED recovery contract drifted: %v", err)
	}
	if f.snapshot(t, ctx) != before {
		t.Fatal("live CLAIMED recovery changed owner completion state")
	}
	observed := f.material
	if mismatch {
		observed.ContentSHA256 = runtimeSecretHashB
	}
	trace, release, recovered := startHistoricalSecretCall(t, ctx, r, "rollback", false, func(ctx context.Context, s *platformservice.Service) (string, error) {
		return f.recover(ctx, s, principal, observed)
	})
	awaitHistoricalSecretPhase(t, ctx, trace, recovered)
	f.complete(t, ctx)
	afterComplete := f.snapshot(t, ctx)
	release()
	result := awaitHistoricalSecretResult(t, ctx, recovered)
	if !errors.Is(result.err, errs.ErrConflict) || result.action == "DELETE" {
		t.Fatalf("probe-to-terminal transition bypassed conflict routing: action=%s err=%v", result.action, result.err)
	}
	if f.snapshot(t, ctx) != afterComplete {
		t.Fatal("SERIALIZABLE terminal recheck added owner effects")
	}
	f.ackCiphertext(t, ctx)
	action, err = f.recover(ctx, s, principal, f.material)
	if err != nil || action != "KEEP" {
		t.Fatalf("fresh terminal recovery could not rejoin: %v", err)
	}
}

// testHistoricalDraftClaimPins preserves exact claimant/generation rejection in the terminal RC probe and never lets the caller select another producer's published material.
// testHistoricalDraftClaimPins сохраняет exact claimant/generation rejection в terminal RC probe и не позволяет caller выбрать published material другого producer.
func testHistoricalDraftClaimPins(t *testing.T, ctx context.Context, f *historicalRecoveryFixture) {
	t.Helper()
	principal := f.recoveryPrincipal(t, ctx)
	for _, kind := range []string{"claimant", "generation"} {
		t.Run(kind, func(t *testing.T) {
			input := port.RuntimeSecretDraftWorkInput{Action: "RECOVER", OperationRef: f.operationRef, ClaimantID: f.work.ClaimantID, ClaimGeneration: f.work.ClaimGeneration, Materialization: &f.material}
			if kind == "claimant" {
				input.ClaimantID = "foreign-broker"
			} else {
				input.ClaimGeneration++
			}
			before := f.snapshot(t, ctx)
			if _, err := f.service.FinishRuntimeSecretDraft(ctx, principal, input); !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("substituted draft claim pin reached recovery: %v", err)
			}
			if f.snapshot(t, ctx) != before {
				t.Fatal("substituted draft claim pin changed owner effects")
			}
		})
	}
}

// ackHistoricalDraftMaterial confirms only the exact persisted materialization DELETE intent and verifies the unchanged ACK replay path; no Kubernetes delete is executed by the fixture.
// ackHistoricalDraftMaterial подтверждает лишь exact persisted materialization DELETE intent и проверяет прежний ACK replay path; Kubernetes delete оснастка не выполняет.
func ackHistoricalDraftMaterial(t *testing.T, ctx context.Context, f *historicalRecoveryFixture) {
	t.Helper()
	input := port.RuntimeSecretDraftWorkInput{Action: "CLEANUP", OperationRef: f.operationRef, ClaimantID: f.work.ClaimantID, ClaimGeneration: f.work.ClaimGeneration, Materialization: &f.material}
	for range 2 {
		result, err := f.service.FinishRuntimeSecretDraft(ctx, f.broker(t, ctx, "platform.runtime-secret-drafts.cleanup.complete"), input)
		if err != nil || !result.Completed {
			t.Fatalf("exact materialization cleanup ACK failed: %v", err)
		}
	}
}
