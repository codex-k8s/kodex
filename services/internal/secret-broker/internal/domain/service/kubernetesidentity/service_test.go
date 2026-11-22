package kubernetesidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// setupFixture records local port effects while retaining owner admission across service replacement.
// It models explicit UNKNOWN versus missing completion; real SQL and actual TLS have separate tests.
// setupFixture считает local port effects и сохраняет owner admission при замене service.
// Различает explicit UNKNOWN и missing completion; real SQL и actual TLS проверяются отдельно.
type setupFixture struct {
	snapshot                           value.KubernetesIdentityUploadSnapshot
	identity                           value.KubernetesIdentityLocator
	resolveErr, verifyErr, effectErr   error
	completeErr                        error
	claimed, reported, rejected        bool
	creates, observes, reads, verifies int
	claims, completes                  int
	completion                         targetrepo.IdentityUploadCompletion
	cancel                             context.CancelFunc
	expiredAdmission                   bool
}

// ResolveKubernetesIdentityUpload exposes only durable fixture state, including unresolved attempts.
// ResolveKubernetesIdentityUpload предоставляет лишь durable fixture state, включая unresolved attempts.
func (fixture *setupFixture) ResolveKubernetesIdentityUpload(context.Context, string, string) (value.KubernetesIdentityUploadSnapshot, error) {
	return fixture.snapshot, fixture.resolveErr
}

// ClaimKubernetesIdentityUpload refuses a repeated write and an observation without a reported fact.
// ClaimKubernetesIdentityUpload отклоняет повторную write и observation без reported fact.
func (fixture *setupFixture) ClaimKubernetesIdentityUpload(_ context.Context, input targetrepo.IdentityUploadClaim) (value.KubernetesIdentityUploadAdmission, error) {
	fixture.claims++
	if fixture.rejected || fixture.claimed && (!fixture.reported || !fixture.snapshot.ReconcileOnly) || input.ContentSHA256 != fixture.snapshot.Provision.ContentSHA256 {
		return value.KubernetesIdentityUploadAdmission{}, targetrepo.ErrSetupRejected
	}
	fixture.claimed = true
	expires := time.Now().UTC().Add(20 * time.Second)
	if fixture.expiredAdmission {
		expires = time.Now().UTC().Add(-time.Second)
	}
	return value.KubernetesIdentityUploadAdmission{SetupRef: input.SetupRef, AttemptRef: "ksa_fixture01", Fence: "fence_fixture01", Generation: 1,
		ExpiresAt: expires, Provision: fixture.snapshot.Provision, ReconcileOnly: fixture.snapshot.ReconcileOnly, KnownIdentity: fixture.snapshot.KnownIdentity}, nil
}

// CompleteKubernetesIdentityUpload requires independent receipt delivery after cancellation and joins.
// CompleteKubernetesIdentityUpload требует independent receipt delivery после cancellation и join.
func (fixture *setupFixture) CompleteKubernetesIdentityUpload(ctx context.Context, input targetrepo.IdentityUploadCompletion) (value.KubernetesIdentitySetupView, error) {
	fixture.completes++
	fixture.completion = input
	if ctx.Err() != nil {
		return value.KubernetesIdentitySetupView{}, errors.New("fixture completion inherited cancellation")
	}
	if fixture.completeErr != nil {
		fixture.snapshot.ReconcileOnly = true
		return value.KubernetesIdentitySetupView{}, fixture.completeErr
	}
	fixture.reported = true
	fixture.rejected = input.Rejected != nil
	state := "COMPLETED"
	if input.Unknown {
		fixture.snapshot.ReconcileOnly = true
		state = "UNKNOWN"
	}
	if input.Rejected != nil {
		state = "ACK_REJECTED"
	}
	return value.KubernetesIdentitySetupView{CanonicalContext: "fixture-context", SetupRef: fixture.snapshot.SetupRef, ConnectionRef: "int_fixture01", ConnectionVersion: 1, IdentityRef: fixture.snapshot.Provision.IdentityRef,
		IdentityRevision: fixture.snapshot.Provision.Revision, TargetRef: fixture.snapshot.Target.ID, Policy: "PROJECT_SHARED", State: state, Version: 2, ExpiresAt: fixture.snapshot.ExpiresAt}, nil
}

// VerifyKubernetesIdentity records the selected-target preflight without accessing external resources.
// VerifyKubernetesIdentity считает selected-target preflight без обращения к внешним ресурсам.
func (fixture *setupFixture) VerifyKubernetesIdentity(context.Context, []byte, value.KubernetesTarget) error {
	fixture.verifies++
	return fixture.verifyErr
}

// CreateKubernetesIdentity represents one joined storage call and optionally cancels its caller.
// CreateKubernetesIdentity представляет один joined storage call и может отменить caller.
func (fixture *setupFixture) CreateKubernetesIdentity(context.Context, value.KubernetesIdentityProvision, []byte) (value.KubernetesIdentityLocator, error) {
	fixture.creates++
	if fixture.cancel != nil {
		fixture.cancel()
	}
	return fixture.identity, fixture.effectErr
}

// ObserveKubernetesIdentity never increments the write counter or invents a missing UID.
// ObserveKubernetesIdentity не увеличивает write counter и не выдумывает missing UID.
func (fixture *setupFixture) ObserveKubernetesIdentity(context.Context, value.KubernetesIdentityProvision) (value.KubernetesIdentityLocator, error) {
	fixture.observes++
	return fixture.identity, fixture.effectErr
}

// ReadKubernetesIdentity supplies an exact known object's private copy for bounded clearing.
// ReadKubernetesIdentity возвращает private copy exact known object для bounded clearing.
func (fixture *setupFixture) ReadKubernetesIdentity(context.Context, value.KubernetesIdentityLocator) ([]byte, error) {
	fixture.reads++
	return []byte("synthetic-private-document"), fixture.effectErr
}

// newSetupFixture creates isolated metadata and port state without materializing a Kubernetes object.
// newSetupFixture создаёт isolated metadata и port state без materialization Kubernetes object.
func newSetupFixture(t *testing.T) (*setupFixture, *Service, []byte) {
	t.Helper()
	material := []byte("synthetic-private-document")
	hash := sha256.Sum256(material)
	provision := value.KubernetesIdentityProvision{IdentityRef: "kbi_fixture01", TargetRef: "target_fixture01", Namespace: "broker-private", Revision: 1, ContentSHA256: hex.EncodeToString(hash[:])}
	name, err := value.KubernetesIdentitySecretName(provision.IdentityRef, provision.Revision)
	if err != nil {
		t.Fatal("derive fixture material name")
	}
	fixture := &setupFixture{snapshot: value.KubernetesIdentityUploadSnapshot{SetupRef: "ksu_fixture01", Digest: hex.EncodeToString(hash[:]), Provision: provision,
		Target: value.KubernetesTarget{ID: provision.TargetRef}, ContentBytes: uint32(len(material)), ExpiresAt: time.Now().UTC().Add(time.Minute)},
		identity: value.KubernetesIdentityLocator{IdentityRef: provision.IdentityRef, TargetRef: provision.TargetRef, Revision: 1, Namespace: provision.Namespace, Name: name,
			UID: "76b0b43a-84c2-44d6-81a4-7c477a92ef2e", ResourceVersion: "42", ContentSHA256: provision.ContentSHA256}}
	service, err := New(fixture, fixture, fixture, fixture, "broker_fixture01", provision.Namespace)
	if err != nil {
		t.Fatal("construct isolated setup service")
	}
	return fixture, service, material
}

// TestKubernetesIdentitySetupNoWriteBeforeExactAdmission covers malformed content and denied preflight.
// No parser failure, lost Claim or expired admission can result in a storage request or fake receipt.
// TestKubernetesIdentitySetupNoWriteBeforeExactAdmission проверяет malformed content и denied preflight.
// Parser failure, lost Claim и expired admission не приводят к storage request либо fake receipt.
func TestKubernetesIdentitySetupNoWriteBeforeExactAdmission(t *testing.T) {
	for _, scenario := range []string{"content", "preflight", "unreported", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, service, material := newSetupFixture(t)
			switch scenario {
			case "content":
				material[0] ^= 1
			case "preflight":
				fixture.verifyErr = targetrepo.ErrSetupRejected
			case "unreported":
				fixture.claimed = true
			case "expired":
				fixture.expiredAdmission = true
			}
			if _, err := service.Materialize(context.Background(), fixture.snapshot.SetupRef, make([]byte, 32), material); err == nil || fixture.creates != 0 || fixture.observes != 0 || fixture.completes != 0 {
				t.Fatal("invalid content, unavailable owner or expired grant produced a storage effect")
			}
		})
	}
}

// TestKubernetesIdentitySetupLostAcknowledgementRequiresReportedUnknown exercises service replacement.
// Explicit UNKNOWN permits only Observe; lost Complete and rejected ACK never admit that observation.
// TestKubernetesIdentitySetupLostAcknowledgementRequiresReportedUnknown проверяет service replacement.
// Explicit UNKNOWN допускает лишь Observe; lost Complete и rejected ACK не допускают observation.
func TestKubernetesIdentitySetupLostAcknowledgementRequiresReportedUnknown(t *testing.T) {
	for _, scenario := range []string{"unknown", "unreported", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, service, material := newSetupFixture(t)
			fixture.effectErr = targetrepo.ErrIdentityCreateUnknown
			if scenario == "unreported" {
				fixture.completeErr = targetrepo.ErrSetupUnavailable
			}
			if scenario == "rejected" {
				fixture.effectErr = &targetrepo.IdentityAcknowledgementRejection{Receipt: value.KubernetesIdentityAcknowledgementRejection{ResponseSHA256: fixture.snapshot.Digest}}
			}
			_, _ = service.Materialize(context.Background(), fixture.snapshot.SetupRef, make([]byte, 32), material)
			if fixture.creates != 1 || fixture.completes != 1 || scenario == "rejected" && fixture.completion.Rejected == nil || scenario != "rejected" && !fixture.completion.Unknown {
				t.Fatal("first storage effect lost its exact typed outcome")
			}
			fixture.effectErr, fixture.completeErr = nil, nil
			replacement, err := New(fixture, fixture, fixture, fixture, "broker_replaced01", "broker-private")
			if err != nil {
				t.Fatal("construct replacement service")
			}
			_, err = replacement.Observe(context.Background(), fixture.snapshot.SetupRef, make([]byte, 32))
			if scenario == "unknown" {
				if err != nil || fixture.observes != 1 || fixture.completes != 2 {
					t.Fatal("reported transport ambiguity did not reach separately admitted observation")
				}
			} else if err == nil || fixture.observes != 0 || fixture.completes != 1 {
				t.Fatal("unreported or rejected write gained observation credit")
			}
			if fixture.creates != 1 {
				t.Fatal("replacement service repeated immutable Create")
			}
		})
	}
}

// TestKubernetesIdentitySetupCancellationStillDeliversExactFact verifies independent completion budget.
// Completed local storage work is joined before receipt delivery; caller cancellation cannot erase it.
// TestKubernetesIdentitySetupCancellationStillDeliversExactFact проверяет independent completion budget.
// Local storage work joined до receipt delivery; caller cancellation не может стереть этот факт.
func TestKubernetesIdentitySetupCancellationStillDeliversExactFact(t *testing.T) {
	fixture, service, material := newSetupFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.cancel = cancel
	view, err := service.Materialize(ctx, fixture.snapshot.SetupRef, make([]byte, 32), material)
	if err != nil || view.State != "COMPLETED" || fixture.creates != 1 || fixture.completes != 1 || fixture.completion.Identity == nil || fixture.completion.Unknown {
		t.Fatal("caller cancellation erased a joined storage fact or inherited its completion deadline")
	}
}
