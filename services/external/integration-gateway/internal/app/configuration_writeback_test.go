package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// writeBackOwnerFixture simulates versioned owner receipts and lost acknowledgements while counting begin, complete and failure calls without external writes.
// RU: writeBackOwnerFixture имитирует версионированные owner-квитанции и потерянные подтверждения, считая begin, complete и failure без внешней записи.
type writeBackOwnerFixture struct {
	cp.ManagedConfigurationGitWriteBackWorkServiceClient
	work                        *cp.ManagedConfigurationGitWriteBackWork
	begins, completes, failures int
	already, foreign            bool
	beginErr, completeErr       error
}

// ClaimManagedConfigurationGitWriteBackWork rejects an unexpected claimant or limit and returns exactly the configured synthetic item.
// RU: ClaimManagedConfigurationGitWriteBackWork отклоняет неожиданный claimant либо limit и возвращает ровно заданную синтетическую работу.
func (owner *writeBackOwnerFixture) ClaimManagedConfigurationGitWriteBackWork(_ context.Context, request *cp.ClaimManagedConfigurationGitWriteBackWorkRequest, _ ...grpc.CallOption) (*cp.ClaimManagedConfigurationGitWriteBackWorkResponse, error) {
	if request.GetLimit() != 1 || request.GetClaimant() != "fixture" {
		return nil, errors.New("fixture claim changed")
	}
	return &cp.ClaimManagedConfigurationGitWriteBackWorkResponse{Work: []*cp.ManagedConfigurationGitWriteBackWork{owner.work}}, nil
}

// receipt clones the original proposal with a newer version and can corrupt its approval digest to exercise immutable-scope rejection.
// RU: receipt клонирует исходное предложение с новой версией и может испортить approval digest для проверки отклонения изменённой неизменяемой области.
func (owner *writeBackOwnerFixture) receipt() *cp.ManagedConfigurationGitWriteBack {
	p := proto.Clone(owner.work.GetProposal()).(*cp.ManagedConfigurationGitWriteBack)
	p.Version++
	if owner.foreign {
		p.ApprovalDigest = "foreign"
	}
	return p
}

// BeginManagedConfigurationGitWriteBackEffect checks lease, parent and content pins, then supplies a controlled EFFECT_STARTED receipt or lost ACK.
// RU: BeginManagedConfigurationGitWriteBackEffect проверяет привязки аренды, родителя и содержимого, затем выдаёт управляемую квитанцию EFFECT_STARTED либо потерянный ACK.
func (owner *writeBackOwnerFixture) BeginManagedConfigurationGitWriteBackEffect(_ context.Context, request *cp.BeginManagedConfigurationGitWriteBackEffectRequest, _ ...grpc.CallOption) (*cp.BeginManagedConfigurationGitWriteBackEffectResponse, error) {
	owner.begins++
	if !proto.Equal(request.GetLease(), owner.work.GetLease()) || request.GetParentCommitSha() != owner.work.GetProposal().GetBaseCommitSha() || request.GetContentSha256() != owner.work.GetProposal().GetProposedContentSha256() {
		return nil, errors.New("fixture begin pins changed")
	}
	p := owner.receipt()
	p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_EFFECT_STARTED
	p.CandidateCommitSha = request.GetCandidateCommitSha()
	return &cp.BeginManagedConfigurationGitWriteBackEffectResponse{Proposal: p, AlreadyStarted: owner.already}, owner.beginErr
}

// CompleteManagedConfigurationGitWriteBackEffect asserts the original lease and returns a timestamped branch or pull-request receipt with an optional ACK failure.
// RU: CompleteManagedConfigurationGitWriteBackEffect проверяет исходную аренду и возвращает квитанцию ветки либо pull request с временем и возможной ошибкой ACK.
func (owner *writeBackOwnerFixture) CompleteManagedConfigurationGitWriteBackEffect(_ context.Context, request *cp.CompleteManagedConfigurationGitWriteBackEffectRequest, _ ...grpc.CallOption) (*cp.CompleteManagedConfigurationGitWriteBackEffectResponse, error) {
	owner.completes++
	if !proto.Equal(request.GetLease(), owner.work.GetLease()) {
		return nil, errors.New("fixture complete lease changed")
	}
	p := owner.receipt()
	p.CandidateCommitSha = request.GetCandidateCommitSha()
	p.BranchConfirmedAt = timestamppb.Now()
	p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_QUEUED
	if request.GetEffect() == cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_PULL_REQUEST {
		p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_SUCCEEDED
		p.PullRequestRef = request.GetPullRequestRef()
		p.PullRequestUrl = request.GetPullRequestUrl()
		p.PullRequestConfirmedAt = timestamppb.Now()
	}
	return &cp.CompleteManagedConfigurationGitWriteBackEffectResponse{Proposal: p}, owner.completeErr
}

// FailManagedConfigurationGitWriteBackWork requires exact lease pins and a specified cause before returning a synthetic UNKNOWN_OUTCOME receipt.
// RU: FailManagedConfigurationGitWriteBackWork требует точных привязок аренды и заданной причины до возврата синтетической квитанции UNKNOWN_OUTCOME.
func (owner *writeBackOwnerFixture) FailManagedConfigurationGitWriteBackWork(_ context.Context, request *cp.FailManagedConfigurationGitWriteBackWorkRequest, _ ...grpc.CallOption) (*cp.FailManagedConfigurationGitWriteBackWorkResponse, error) {
	owner.failures++
	if !proto.Equal(request.GetLease(), owner.work.GetLease()) || request.GetFailureCode() == cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_UNSPECIFIED {
		return nil, errors.New("fixture failure pins changed")
	}
	p := owner.receipt()
	p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_UNKNOWN_OUTCOME
	return &cp.FailManagedConfigurationGitWriteBackWorkResponse{Proposal: p}, nil
}

// writeBackExecutionFixture counts preparation, effects, readback and cleanup while injecting provider ACK failures without Git or HTTP access.
// RU: writeBackExecutionFixture считает подготовку, эффекты, readback и очистку, подставляя ошибки provider ACK без доступа к Git либо HTTP.
type writeBackExecutionFixture struct {
	prepares, pushes, verifies, finds, creates, closes int
	exists                                             bool
	pushErr, verifyErr, createErr                      error
}

// Close counts cleanup calls so each opened execution must be released exactly once on successful and failed paths.
// RU: Close считает очистки, чтобы каждое открытое исполнение освобождалось ровно один раз при успехе и ошибке.
func (execution *writeBackExecutionFixture) Close() { execution.closes++ }

// PrepareCandidate counts preparation and returns fixed synthetic commit, tree and blob identities without constructing a repository.
// RU: PrepareCandidate считает подготовки и возвращает фиксированные синтетические идентификаторы commit, tree и blob без создания репозитория.
func (execution *writeBackExecutionFixture) PrepareCandidate(context.Context) (integration.ConfigurationWriteBackCandidate, error) {
	execution.prepares++
	return integration.ConfigurationWriteBackCandidate{CommitSHA: strings.Repeat("b", 40), TreeSHA: strings.Repeat("c", 40), BlobSHA: strings.Repeat("d", 40), BaseBlobSHA: strings.Repeat("e", 40)}, nil
}

// PushCandidate counts each branch-effect attempt and returns the configured provider error, exposing forbidden retries to assertions.
// RU: PushCandidate считает каждую попытку изменения ветки и возвращает заданную ошибку провайдера, позволяя обнаружить запрещённые повторы.
func (execution *writeBackExecutionFixture) PushCandidate(context.Context, integration.ConfigurationWriteBackCandidate) error {
	execution.pushes++
	return execution.pushErr
}

// VerifyBranch counts readback and independently injects verification failure even when a push acknowledgement was already lost.
// RU: VerifyBranch считает readback и независимо подставляет ошибку проверки, в том числе после потери подтверждения push.
func (execution *writeBackExecutionFixture) VerifyBranch(context.Context, integration.ConfigurationWriteBackCandidate) error {
	execution.verifies++
	return execution.verifyErr
}

// FindPullRequest counts lookups and reports controlled existence with a fixed synthetic reference, never issuing a provider request.
// RU: FindPullRequest считает поиски и сообщает управляемое наличие с фиксированной синтетической ссылкой, не обращаясь к провайдеру.
func (execution *writeBackExecutionFixture) FindPullRequest(context.Context, string) (integration.ConfigurationWriteBackPullRequest, bool, error) {
	execution.finds++
	return integration.ConfigurationWriteBackPullRequest{Ref: "7", URL: "https://github.com/acme/repo/pull/7"}, execution.exists, nil
}

// CreatePullRequest marks the synthetic request as existing before returning an optional error, modeling an applied effect whose ACK was lost.
// RU: CreatePullRequest отмечает синтетический запрос существующим до возможной ошибки, моделируя выполненный эффект с потерянным ACK.
func (execution *writeBackExecutionFixture) CreatePullRequest(context.Context, string) (integration.ConfigurationWriteBackPullRequest, error) {
	execution.creates++
	execution.exists = true
	return integration.ConfigurationWriteBackPullRequest{}, execution.createErr
}

// TestWriteBackReceiptRejectsChangedImmutableScope first accepts the exact newer receipt, then rejects each mutated kind, format, base hash, timestamp or unknown failure enum.
// RU: TestWriteBackReceiptRejectsChangedImmutableScope сначала принимает точную новую квитанцию, затем отклоняет изменения kind, format, исходного хеша, времён и неизвестный failure enum.
func TestWriteBackReceiptRejectsChangedImmutableScope(t *testing.T) {
	for name, mutate := range map[string]func(*cp.ManagedConfigurationGitWriteBack){
		"kind": func(p *cp.ManagedConfigurationGitWriteBack) {
			p.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION
		},
		"format":   func(p *cp.ManagedConfigurationGitWriteBack) { p.ContentFormat = "foreign" },
		"base":     func(p *cp.ManagedConfigurationGitWriteBack) { p.BaseContentSha256 = "foreign" },
		"created":  func(p *cp.ManagedConfigurationGitWriteBack) { p.CreatedAt = timestamppb.Now() },
		"expiry":   func(p *cp.ManagedConfigurationGitWriteBack) { p.ExpiresAt = timestamppb.Now() },
		"approval": func(p *cp.ManagedConfigurationGitWriteBack) { p.ApprovedAt = timestamppb.Now() },
		"failure": func(p *cp.ManagedConfigurationGitWriteBack) {
			p.FailureCode = cp.ManagedConfigurationGitWriteBackFailure(999)
		},
	} {
		t.Run(name, func(t *testing.T) {
			work := writeBackFixtureWork()
			p := proto.Clone(work.GetProposal()).(*cp.ManagedConfigurationGitWriteBack)
			p.Version++
			if !validWriteBackReceipt(p, work) {
				t.Fatal("exact receipt rejected")
			}
			mutate(p)
			if validWriteBackReceipt(p, work) {
				t.Fatal("changed scope accepted")
			}
		})
	}
}

// writeBackFixtureWork creates one synthetic approved proposal with exact source/configuration/connection revisions, a fenced lease and future work deadline.
// RU: writeBackFixtureWork создаёт одно синтетическое одобренное предложение с точными ревизиями источника, конфигурации и подключения, fenced-арендой и будущим сроком работы.
func writeBackFixtureWork() *cp.ManagedConfigurationGitWriteBackWork {
	p := &cp.ManagedConfigurationGitWriteBack{Ref: "mcwb_fixture", Version: 10, ConfigurationRef: "cfg_fixture", ConfigurationVersion: 2, SourceRef: "source_fixture", SourceVersion: 3, ConnectionRef: "connection_fixture", ConnectionVersion: 4, ApprovalDigest: strings.Repeat("a", 64), BaseCommitSha: strings.Repeat("a", 40), ProposedContentSha256: strings.Repeat("b", 64), ProposalBranch: "kodex/writeback/mcwb_fixture", RepositoryRef: "acme/repo", SourceRefName: "main", Path: "config.yaml", CandidateCommitSha: strings.Repeat("b", 40)}
	return &cp.ManagedConfigurationGitWriteBackWork{Proposal: p, Lease: &cp.ManagedConfigurationGitWriteBackLease{ProposalRef: p.Ref, Attempt: 1, ClaimGeneration: 1, Claimant: "fixture", Fence: "fixture-fence", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, Deadline: timestamppb.New(time.Now().Add(time.Minute)), Mode: cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE, Effect: cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_BRANCH, CandidateTreeSha: strings.Repeat("c", 40), CandidateBlobSha: strings.Repeat("d", 40)}
}

// TestConfigurationWriteBackDoesNotResendAcrossOwnerAndProviderFailures covers branch and pull-request execution, recovery, repeated begin and lost owner/provider ACKs.
// RU: TestConfigurationWriteBackDoesNotResendAcrossOwnerAndProviderFailures проверяет ветку и pull request, восстановление, повторный begin и потерю owner/provider ACK.
// It requires one cleanup, no effect before a valid owner receipt, no writes during read-only recovery, no hidden retry and readback before successful completion.
// RU: Тест требует единственную очистку, отсутствие эффекта до верной owner-квитанции, запрет записи при read-only восстановлении, отсутствие скрытого повтора и readback до успешного завершения.
func TestConfigurationWriteBackDoesNotResendAcrossOwnerAndProviderFailures(t *testing.T) {
	for _, name := range []string{"branch", "branch-lost-begin", "branch-repeated-begin", "branch-recovery", "branch-lost-push-ack", "branch-unconfirmed", "foreign-begin", "lost-complete", "pr", "pr-recovery-missing", "pr-recovery-found", "pr-lost-begin", "pr-lost-create-ack"} {
		t.Run(name, func(t *testing.T) {
			owner := &writeBackOwnerFixture{work: writeBackFixtureWork()}
			execution := &writeBackExecutionFixture{}
			if strings.HasPrefix(name, "pr") {
				owner.work.Effect = cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_PULL_REQUEST
			}
			if strings.Contains(name, "recovery") {
				owner.work.Mode = cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY
			}
			if strings.Contains(name, "lost-begin") {
				owner.beginErr = errors.New("fixture begin ACK lost")
			}
			if name == "branch-repeated-begin" {
				owner.already = true
			}
			if name == "foreign-begin" {
				owner.foreign = true
			}
			if name == "lost-complete" {
				owner.completeErr = errors.New("fixture complete ACK lost")
			}
			if name == "branch-lost-push-ack" || name == "branch-unconfirmed" {
				execution.pushErr = errors.New("fixture push ACK lost")
			}
			if name == "branch-unconfirmed" {
				execution.verifyErr = errors.New("fixture readback unavailable")
			}
			if name == "pr-recovery-found" {
				execution.exists = true
			}
			if name == "pr-lost-create-ack" {
				execution.createErr = errors.New("fixture create ACK lost")
			}
			count, err := processWriteBack(t.Context(), owner, func(context.Context, *cp.ManagedConfigurationGitWriteBackWork) (writeBackExecution, error) {
				return execution, nil
			}, Config{InstanceID: "fixture", RequestTimeout: time.Second, OperationTimeout: 10 * time.Second})
			wantError := strings.Contains(name, "lost-begin") || name == "foreign-begin" || name == "lost-complete"
			if (err != nil) != wantError || execution.closes != 1 {
				t.Fatalf("result=%d error=%v closes=%d", count, err, execution.closes)
			}
			if wantError && name != "lost-complete" && (execution.pushes != 0 || execution.creates != 0 || owner.completes != 0) {
				t.Fatal("effect occurred before exact owner receipt")
			}
			if strings.Contains(name, "recovery") && (execution.prepares != 0 || execution.pushes != 0 || execution.creates != 0 || owner.begins != 0) {
				t.Fatal("readonly recovery resent effect")
			}
			if owner.already && execution.pushes != 0 {
				t.Fatal("repeated begin resent push")
			}
			if execution.pushes > 1 || execution.creates > 1 || owner.completes > 1 || owner.failures > 1 {
				t.Fatal("hidden retry")
			}
			if name == "branch-unconfirmed" || name == "pr-recovery-missing" {
				if owner.failures != 1 || owner.completes != 0 {
					t.Fatal("unconfirmed effect became success")
				}
			} else if !wantError && owner.completes != 1 {
				t.Fatal("exact readback was not acknowledged")
			}
		})
	}
}
