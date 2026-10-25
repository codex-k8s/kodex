package app

import (
	"context"
	"errors"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// bundleOwnerFixture simulates only typed owner acknowledgements and records exact call cardinality; it cannot contact S3, Kubernetes or a provider.
// bundleOwnerFixture моделирует только typed owner acknowledgements и учитывает exact call cardinality; S3, Kubernetes и provider не вызываются.
type bundleOwnerFixture struct {
	cp.RepositoryBundlePublicationServiceClient
	work                               *cp.RepositoryBundlePublicationWork
	body                               []byte
	beginErr, completeErr              error
	already, substitute                bool
	reads, begins, completes, failures int
}

// ClaimRepositoryBundlePublications returns one cloned immutable fixture epoch so application mutation cannot alter the simulated owner record.
// ClaimRepositoryBundlePublications возвращает cloned immutable fixture epoch, исключая изменение owner record приложением.
func (owner *bundleOwnerFixture) ClaimRepositoryBundlePublications(context.Context, *cp.ClaimRepositoryBundlePublicationsRequest, ...grpc.CallOption) (*cp.ClaimRepositoryBundlePublicationsResponse, error) {
	return &cp.ClaimRepositoryBundlePublicationsResponse{Work: []*cp.RepositoryBundlePublicationWork{proto.Clone(owner.work).(*cp.RepositoryBundlePublicationWork)}}, nil
}

// ReadRepositoryBundlePublication records the sole bounded source read and returns an independent exact source copy.
// ReadRepositoryBundlePublication учитывает sole bounded source read и возвращает independent exact source copy.
func (owner *bundleOwnerFixture) ReadRepositoryBundlePublication(context.Context, *cp.ReadRepositoryBundlePublicationRequest, ...grpc.CallOption) (*cp.ReadRepositoryBundlePublicationResponse, error) {
	owner.reads++
	return &cp.ReadRepositoryBundlePublicationResponse{Metadata: proto.Clone(owner.work.Proposal.Archive).(*cp.RepositoryBundleArtifactPin), Archive: append([]byte(nil), owner.body...)}, nil
}

// BeginRepositoryBundlePublication can lose its ACK or substitute identity; the application must issue no push in either case.
// BeginRepositoryBundlePublication моделирует lost ACK либо substituted identity; приложение не должно делать push в обоих случаях.
func (owner *bundleOwnerFixture) BeginRepositoryBundlePublication(_ context.Context, r *cp.BeginRepositoryBundlePublicationRequest, _ ...grpc.CallOption) (*cp.BeginRepositoryBundlePublicationResponse, error) {
	owner.begins++
	if owner.beginErr != nil {
		return nil, owner.beginErr
	}
	p := proto.Clone(owner.work.Proposal).(*cp.RepositoryBundlePublication)
	p.Version++
	p.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EFFECT_STARTED
	p.CandidateCommitSha = r.CandidateCommitSha
	p.CandidateTreeSha = r.CandidateTreeSha
	if owner.substitute {
		p.GrantRef = "grt_foreign000"
	}
	if !owner.substitute {
		owner.work.Proposal = proto.Clone(p).(*cp.RepositoryBundlePublication)
	}
	return &cp.BeginRepositoryBundlePublicationResponse{Proposal: p, AlreadyStarted: owner.already}, nil
}

// CompleteRepositoryBundlePublication can lose its ACK after a successful proof, preserving exactly one report without application retry or failure cleanup.
// CompleteRepositoryBundlePublication моделирует lost ACK после successful proof, сохраняя один report без application retry либо failure cleanup.
func (owner *bundleOwnerFixture) CompleteRepositoryBundlePublication(_ context.Context, r *cp.CompleteRepositoryBundlePublicationRequest, _ ...grpc.CallOption) (*cp.CompleteRepositoryBundlePublicationResponse, error) {
	owner.completes++
	if owner.completeErr != nil {
		return nil, owner.completeErr
	}
	p := proto.Clone(owner.work.Proposal).(*cp.RepositoryBundlePublication)
	p.Version += 2
	p.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED
	p.CandidateCommitSha = r.CandidateCommitSha
	p.CandidateTreeSha = r.CandidateTreeSha
	p.ConfirmedAt = timestamppb.Now()
	return &cp.CompleteRepositoryBundlePublicationResponse{Proposal: p}, nil
}

// FailRepositoryBundlePublication records known pre-effect failure or begun uncertainty without issuing another effect lease.
// FailRepositoryBundlePublication учитывает known pre-effect failure либо begun uncertainty без новой effect lease.
func (owner *bundleOwnerFixture) FailRepositoryBundlePublication(_ context.Context, r *cp.FailRepositoryBundlePublicationRequest, _ ...grpc.CallOption) (*cp.FailRepositoryBundlePublicationResponse, error) {
	owner.failures++
	p := proto.Clone(owner.work.Proposal).(*cp.RepositoryBundlePublication)
	p.Version++
	p.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME
	p.FailureCode = r.FailureCode
	return &cp.FailRepositoryBundlePublicationResponse{Proposal: p}, nil
}

// bundleProviderFixture observes local preparation, exactly one push attempt and independent proof; provider ACK loss is configurable and carries no raw credentials.
// bundleProviderFixture наблюдает local preparation, exact one push attempt и independent proof; provider ACK loss настраивается без raw credentials.
type bundleProviderFixture struct {
	prepares, pushes, verifies int
	pushErr, verifyErr         error
}

// Close has no external cleanup because this fixture owns no objects or remote refs.
// Close не выполняет external cleanup, поскольку objects либо remote refs оснастке не принадлежат.
func (*bundleProviderFixture) Close() {}

// PrepareCandidate returns one fixed locally prepared identity without a provider write.
// PrepareCandidate возвращает одну fixed locally prepared identity без provider write.
func (provider *bundleProviderFixture) PrepareCandidate(context.Context) (integration.RepositoryBundleCandidate, error) {
	provider.prepares++
	return integration.RepositoryBundleCandidate{CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TreeSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, nil
}

// PushCandidate counts every actual attempted effect, including a lost provider ACK.
// PushCandidate учитывает actual attempted effect, включая lost provider ACK.
func (provider *bundleProviderFixture) PushCandidate(context.Context, integration.RepositoryBundleCandidate) error {
	provider.pushes++
	return provider.pushErr
}

// VerifyCandidate counts independent proof attempts and never issues a write on absence or uncertainty.
// VerifyCandidate учитывает independent proof attempts и не выполняет write при absence либо uncertainty.
func (provider *bundleProviderFixture) VerifyCandidate(context.Context, integration.RepositoryBundleCandidate) error {
	provider.verifies++
	return provider.verifyErr
}

// bundleWorkFixture supplies a bounded private work item for owner-ACK tests; full source/provider validation is exercised separately by adapter and CP policy suites.
// bundleWorkFixture создаёт bounded private work item для owner-ACK tests; full source/provider validation отдельно проверяется adapter и CP policy suites.
func bundleWorkFixture() (*bundleOwnerFixture, Config) {
	body := []byte("bounded source fixture")
	p := &cp.RepositoryBundlePublication{Ref: "rbp_fixture000", Version: 3, ProjectRef: "prj_fixture000", GrantRef: "grt_fixture000", ProposalBranch: "kodex/bundle/rbp_fixture000", BaseCommitSha: "cccccccccccccccccccccccccccccccccccccccc", Archive: &cp.RepositoryBundleArtifactPin{Ref: "art_archive000", Revision: 1, Version: 1, Sha256: repositorybundle.Digest(body), SizeBytes: int64(len(body))}, Manifest: &cp.RepositoryBundleArtifactPin{Ref: "art_manifest00", Revision: 1, Version: 1, Sha256: repositorybundle.Digest([]byte("manifest")), SizeBytes: 8}, State: cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_CLAIMED}
	lease := &cp.RepositoryBundleLease{ProposalRef: p.Ref, Attempt: 1, Generation: 1, Claimant: "bundle-fixture", Fence: "fnc_fixture000", ExpiresAt: timestamppb.New(time.Now().Add(5 * time.Minute))}
	return &bundleOwnerFixture{body: body, work: &cp.RepositoryBundlePublicationWork{Lease: lease, Proposal: p, Deadline: timestamppb.New(time.Now().Add(5 * time.Minute)), Mode: cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE}}, Config{InstanceID: "bundle-fixture", RequestTimeout: time.Second, OperationTimeout: 20 * time.Second}
}

// TestRepositoryBundleReceiptDelivery validates terminal Git receipts independently from known delivery blocks; unknown/partial or nonterminal blocks cannot authorize another external effect.
// TestRepositoryBundleReceiptDelivery проверяет terminal Git receipts отдельно от known delivery blocks; unknown/partial либо nonterminal blocks не разрешают новый external effect.
func TestRepositoryBundleReceiptDelivery(t *testing.T) {
	owner, _ := bundleWorkFixture()
	previous := owner.work.Proposal
	next := proto.Clone(previous).(*cp.RepositoryBundlePublication)
	next.Version++
	next.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED
	next.ContinuationState, next.ContinuationCode, next.ContinuationNodeRef = "BLOCKED", "CALLBACK_INPUT_TOO_LARGE", "nod_exact12345"
	if !validBundleReceipt(next, previous) {
		t.Fatal("known delivery failure erased Git receipt")
	}
	for _, name := range []string{"nonterminal", "unknown code", "wrong node"} {
		v := proto.Clone(next).(*cp.RepositoryBundlePublication)
		switch name {
		case "nonterminal":
			v.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EFFECT_STARTED
		case "unknown code":
			v.ContinuationCode = "RETRY"
		case "wrong node":
			v.ContinuationNodeRef = "run_exact12345"
		}
		if validBundleReceipt(v, previous) {
			t.Fatal("invalid delivery receipt accepted", name)
		}
	}
}

// TestRepositoryBundleWorkLostAcknowledgements proves no blind resend, no push before a new exact Begin ACK, no source read during recovery and separate accounting of attempted effects before owner completion.
// TestRepositoryBundleWorkLostAcknowledgements доказывает отсутствие blind resend, push лишь после нового exact Begin ACK, отсутствие source read при recovery и отдельный учёт attempted effects до owner completion.
func TestRepositoryBundleWorkLostAcknowledgements(t *testing.T) {
	for _, name := range []string{"success", "begin ACK lost", "substituted Begin", "already started", "push ACK lost", "complete ACK lost", "read-only recovery", "unconfirmed remote"} {
		t.Run(name, func(t *testing.T) {
			owner, config := bundleWorkFixture()
			provider := &bundleProviderFixture{}
			expectedPush, expectedVerify, expectedComplete := 1, 1, 1
			wantErr := false
			switch name {
			case "begin ACK lost":
				owner.beginErr = errors.New("owner ACK lost")
				expectedPush, expectedVerify, expectedComplete, wantErr = 0, 0, 0, true
			case "substituted Begin":
				owner.substitute = true
				expectedPush, expectedVerify, expectedComplete, wantErr = 0, 0, 0, true
			case "already started":
				owner.already = true
				expectedPush = 0
			case "push ACK lost":
				provider.pushErr = errors.New("provider ACK lost")
			case "complete ACK lost":
				owner.completeErr = errors.New("owner ACK lost")
				wantErr = true
			case "unconfirmed remote":
				provider.verifyErr = errors.New("remote unavailable")
				expectedComplete = 0
			case "read-only recovery":
				owner.work.Mode = cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY
				owner.work.Proposal.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME
				owner.work.Proposal.CandidateCommitSha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				owner.work.Proposal.CandidateTreeSha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				expectedPush = 0
			}
			observed := 0
			_, err := processBundle(t.Context(), owner, func(_ context.Context, _ *cp.RepositoryBundlePublicationWork, raw []byte) (bundleExecution, error) {
				if name == "read-only recovery" && len(raw) != 0 {
					t.Fatal("recovery received source bytes")
				}
				return provider, nil
			}, func(bool) { observed++ }, config)
			if (err != nil) != wantErr || provider.pushes != expectedPush || provider.verifies != expectedVerify || owner.completes != expectedComplete || observed != expectedPush {
				t.Fatalf("unexpected lifecycle: err=%v push=%d verify=%d complete=%d metric=%d", err, provider.pushes, provider.verifies, owner.completes, observed)
			}
			if name == "read-only recovery" && (owner.reads != 0 || owner.begins != 0 || provider.prepares != 0) {
				t.Fatal("read-only recovery read source or prepared a new effect")
			}
			if name != "unconfirmed remote" && owner.failures != 0 {
				t.Fatal("ACK loss triggered failure cleanup")
			}
		})
	}
}
