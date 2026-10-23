package app

import (
	"context"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"google.golang.org/protobuf/proto"
)

// maximumConfigurationWriteBackOperation caps provider execution before reserved owner-finalization time is deducted from lease and work deadlines.
// RU: maximumConfigurationWriteBackOperation ограничивает provider-исполнение до вычета owner-финализации из сроков аренды и работы.
const maximumConfigurationWriteBackOperation = 30 * time.Second

// errConfigurationWriteBack hides malformed owner-response details while rejecting an unprovable writeback lifecycle transition.
// RU: errConfigurationWriteBack скрывает детали неверного owner-ответа и отклоняет неподтверждённый переход жизненного цикла записи.
var errConfigurationWriteBack = errors.New("configuration writeback owner response is invalid")

// writeBackExecution separates preparation, external effects and readback so the application can require owner receipts before mutation.
// RU: writeBackExecution разделяет подготовку, внешние эффекты и readback, позволяя приложению требовать owner-квитанции до изменения.
type writeBackExecution interface {
	// Close releases execution-owned resources; the application defers it after every successful open.
	// RU: Close освобождает ресурсы исполнения; приложение откладывает вызов после каждого успешного открытия.
	Close()
	// PrepareCandidate produces exact commit, tree and blob pins for the owner's begin-effect request.
	// RU: PrepareCandidate формирует точные привязки commit, tree и blob для owner-запроса начала эффекта.
	PrepareCandidate(context.Context) (integration.ConfigurationWriteBackCandidate, error)
	// PushCandidate publishes the candidate only after the caller validates a fresh begin-effect receipt permitting execution.
	// RU: PushCandidate публикует кандидата только после проверки вызывающим свежей begin-effect квитанции, разрешающей исполнение.
	PushCandidate(context.Context, integration.ConfigurationWriteBackCandidate) error
	// VerifyBranch reads back the pinned candidate without using a failed push as permission to resend it.
	// RU: VerifyBranch сверяет закреплённого кандидата, не используя ошибку push как разрешение повторной отправки.
	VerifyBranch(context.Context, integration.ConfigurationWriteBackCandidate) error
	// FindPullRequest locates the claimed candidate's pull request and distinguishes confirmed absence from a failed lookup.
	// RU: FindPullRequest ищет pull request назначенного кандидата и отличает подтверждённое отсутствие от ошибки поиска.
	FindPullRequest(context.Context, string) (integration.ConfigurationWriteBackPullRequest, bool, error)
	// CreatePullRequest performs creation without replacing the caller's mandatory subsequent readback.
	// RU: CreatePullRequest выполняет создание, не заменяя обязательный последующий readback вызывающего кода.
	CreatePullRequest(context.Context, string) (integration.ConfigurationWriteBackPullRequest, error)
}

// writeBackOpen binds an execution to one owner-claimed item and its operation context, not to authority inferred from caller-supplied identifiers.
// RU: writeBackOpen связывает исполнение с одной назначенной owner-работой и контекстом операции, а не с полномочиями из переданных идентификаторов.
type writeBackOpen func(context.Context, *cp.ManagedConfigurationGitWriteBackWork) (writeBackExecution, error)

// processConfigurationWriteBackWork opens the concrete provider execution with effect metrics and delegates the owner-controlled lifecycle to processWriteBack.
// RU: processConfigurationWriteBackWork открывает конкретное provider-исполнение с метриками эффектов и передаёт owner-жизненный цикл в processWriteBack.
func processConfigurationWriteBackWork(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, adapter *integration.Adapter, metrics *businessmetrics.Metrics, config Config) (int, error) {
	return processWriteBack(ctx, control, func(ctx context.Context, work *cp.ManagedConfigurationGitWriteBackWork) (writeBackExecution, error) {
		execution, err := adapter.OpenConfigurationWriteBack(ctx, work)
		if err != nil {
			return nil, err
		}
		return &observedWriteBack{writeBackExecution: execution, metrics: metrics}, nil
	}, config)
}

// observedWriteBack decorates only external push and pull-request creation attempts, keeping preparation, readback and owner completion separate.
// RU: observedWriteBack учитывает только внешние попытки push и создания pull request, отделяя подготовку, readback и owner completion.
type observedWriteBack struct {
	writeBackExecution
	metrics *businessmetrics.Metrics
}

// PushCandidate records the attempted branch effect after the provider returns, independently of later readback or owner finalization.
// RU: PushCandidate учитывает попытку изменения ветки после возврата провайдера независимо от последующего readback и owner-финализации.
func (execution *observedWriteBack) PushCandidate(ctx context.Context, candidate integration.ConfigurationWriteBackCandidate) error {
	err := execution.writeBackExecution.PushCandidate(ctx, candidate)
	execution.metrics.ConfigurationWriteBack(true, err == nil)
	return err
}

// CreatePullRequest records the attempted effect and preserves both provider result and error for subsequent reconciliation.
// RU: CreatePullRequest учитывает попытку эффекта и сохраняет результат и ошибку провайдера для последующего согласования.
func (execution *observedWriteBack) CreatePullRequest(ctx context.Context, commit string) (integration.ConfigurationWriteBackPullRequest, error) {
	result, err := execution.writeBackExecution.CreatePullRequest(ctx, commit)
	execution.metrics.ConfigurationWriteBack(false, err == nil)
	return result, err
}

// processWriteBack claims one item and reserves two owner-request budgets before lease expiry and work deadline; successful open always receives deferred Close.
// RU: processWriteBack получает одну работу и резервирует два owner-запроса до конца аренды и срока работы; успешное открытие всегда получает отложенный Close.
// Branch and pull-request effects require an exact begin receipt; read-only recovery or AlreadyStarted suppresses new provider mutations.
// RU: Эффекты ветки и pull request требуют точной begin-квитанции; read-only восстановление либо AlreadyStarted запрещают новые provider-изменения.
// Push and creation acknowledgements are reconciled by readback, never blind resend; owner RPC errors return without hidden retries.
// RU: Подтверждения push и создания согласуются через readback, а не слепой повтор; ошибки owner RPC возвращаются без скрытых повторов.
// A count of one means an owner-confirmed completion or failure report, not necessarily a successful proposal.
// RU: Счётчик один означает подтверждённый владельцем отчёт о завершении либо ошибке, а не обязательно успешное предложение.
func processWriteBack(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, open writeBackOpen, config Config) (int, error) {
	if control == nil {
		return 0, errConfigurationWriteBack
	}
	claimContext, cancelClaim := context.WithTimeout(ctx, config.RequestTimeout)
	response, err := control.ClaimManagedConfigurationGitWriteBackWork(claimContext, &cp.ClaimManagedConfigurationGitWriteBackWorkRequest{Claimant: config.InstanceID, Limit: 1})
	cancelClaim()
	if err != nil {
		return 0, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(response.GetWork()) > 1 {
		return 0, errConfigurationWriteBack
	}
	if len(response.GetWork()) == 0 {
		return 0, nil
	}
	work := response.GetWork()[0]
	if work == nil || work.GetLease() == nil || work.GetProposal() == nil || work.GetLease().GetClaimant() != config.InstanceID || work.GetLease().GetExpiresAt() == nil || work.GetLease().GetExpiresAt().CheckValid() != nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil {
		return 0, errConfigurationWriteBack
	}
	deadline := time.Now().Add(min(config.OperationTimeout, maximumConfigurationWriteBackOperation))
	for _, end := range []time.Time{work.GetLease().GetExpiresAt().AsTime(), work.GetDeadline().AsTime()} {
		if end.Add(-2 * config.RequestTimeout).Before(deadline) {
			deadline = end.Add(-2 * config.RequestTimeout)
		}
	}
	if !deadline.After(time.Now()) {
		return 0, errConfigurationWriteBack
	}
	operation, cancelOperation := context.WithDeadline(ctx, deadline)
	defer cancelOperation()
	execution, err := open(operation, work)
	if err != nil {
		return failWriteBack(ctx, control, work, config, err)
	}
	defer execution.Close()
	candidate := integration.ConfigurationWriteBackCandidate{CommitSHA: work.GetProposal().GetCandidateCommitSha(), TreeSHA: work.GetCandidateTreeSha(), BlobSHA: work.GetCandidateBlobSha()}
	readOnly := work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY
	if work.GetEffect() == cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_BRANCH {
		if !readOnly {
			candidate, err = execution.PrepareCandidate(operation)
			if err != nil {
				return failWriteBack(ctx, control, work, config, err)
			}
			readOnly, err = beginWriteBack(ctx, control, work, config, candidate)
			if err != nil {
				return 0, err
			}
			if !readOnly {
				err = execution.PushCandidate(operation, candidate)
			}
		}
		// Ошибка push не является разрешением на повтор: допускается только readback.
		verifyErr := execution.VerifyBranch(operation, candidate)
		if verifyErr != nil {
			if err != nil {
				verifyErr = err
			}
			return failWriteBack(ctx, control, work, config, verifyErr)
		}
		return completeWriteBack(ctx, control, work, config, candidate, integration.ConfigurationWriteBackPullRequest{})
	}
	if work.GetEffect() != cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_PULL_REQUEST {
		return 0, errConfigurationWriteBack
	}
	if err = execution.VerifyBranch(operation, candidate); err != nil {
		return failWriteBack(ctx, control, work, config, err)
	}
	found, exists, err := execution.FindPullRequest(operation, candidate.CommitSHA)
	if err != nil {
		return failWriteBack(ctx, control, work, config, err)
	}
	if !readOnly {
		readOnly, err = beginWriteBack(ctx, control, work, config, candidate)
		if err != nil {
			return 0, err
		}
		if !readOnly && !exists {
			_, createErr := execution.CreatePullRequest(operation, candidate.CommitSHA)
			found, exists, err = execution.FindPullRequest(operation, candidate.CommitSHA)
			if err != nil || !exists {
				if createErr != nil {
					err = createErr
				}
				if err == nil {
					err = errConfigurationWriteBack
				}
				return failWriteBack(ctx, control, work, config, err)
			}
		}
	}
	if !exists {
		return failWriteBack(ctx, control, work, config, errConfigurationWriteBack)
	}
	return completeWriteBack(ctx, control, work, config, candidate, found)
}

// beginWriteBack submits cloned lease, candidate and approved-content pins, then requires a matching EFFECT_STARTED owner receipt.
// RU: beginWriteBack передаёт копию аренды, привязки кандидата и одобренного содержимого, затем требует совпадающую owner-квитанцию EFFECT_STARTED.
// Its boolean is AlreadyStarted, requiring readback rather than a repeated effect; an uncertain owner RPC is not retried here.
// RU: Возвращаемый boolean — AlreadyStarted, требующий readback вместо повтора эффекта; неопределённый owner RPC здесь не повторяется.
func beginWriteBack(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, work *cp.ManagedConfigurationGitWriteBackWork, config Config, candidate integration.ConfigurationWriteBackCandidate) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	response, err := control.BeginManagedConfigurationGitWriteBackEffect(ctx, &cp.BeginManagedConfigurationGitWriteBackEffectRequest{Lease: proto.Clone(work.GetLease()).(*cp.ManagedConfigurationGitWriteBackLease), Effect: work.GetEffect(), CandidateCommitSha: candidate.CommitSHA, CandidateTreeSha: candidate.TreeSHA, CandidateBlobSha: candidate.BlobSHA, BaseBlobSha: candidate.BaseBlobSHA, ParentCommitSha: work.GetProposal().GetBaseCommitSha(), ContentSha256: work.GetProposal().GetProposedContentSha256()})
	if err != nil {
		return false, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validWriteBackReceipt(response.GetProposal(), work) || response.GetProposal().GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_EFFECT_STARTED || response.GetProposal().GetCandidateCommitSha() != candidate.CommitSHA {
		return false, errConfigurationWriteBack
	}
	return response.GetAlreadyStarted(), nil
}

// failWriteBack reports a classified cause once unless the parent is cancelled, preserving exact lease pins within the owner-request timeout.
// RU: failWriteBack однократно сообщает классифицированную причину, если родитель не отменён, сохраняя точные привязки аренды в пределах owner-таймаута.
// Only matching QUEUED, FAILED or UNKNOWN_OUTCOME receipts count as processed; failure reporting never fabricates a successful effect.
// RU: Только совпадающие квитанции QUEUED, FAILED либо UNKNOWN_OUTCOME считаются обработанными; сообщение об ошибке не выдумывает успешный эффект.
func failWriteBack(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, work *cp.ManagedConfigurationGitWriteBackWork, config Config, cause error) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	response, err := control.FailManagedConfigurationGitWriteBackWork(ctx, &cp.FailManagedConfigurationGitWriteBackWorkRequest{Lease: proto.Clone(work.GetLease()).(*cp.ManagedConfigurationGitWriteBackLease), FailureCode: integration.ConfigurationWriteBackFailure(cause)})
	if err != nil {
		return 0, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validWriteBackReceipt(response.GetProposal(), work) {
		return 0, errConfigurationWriteBack
	}
	switch response.GetProposal().GetState() {
	case cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_QUEUED, cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_FAILED, cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_UNKNOWN_OUTCOME:
		return 1, nil
	default:
		return 0, errConfigurationWriteBack
	}
}

// completeWriteBack reports the read-back effect with exact lease, commit, approved content and pull-request pins, without retrying a lost ACK.
// RU: completeWriteBack сообщает проверенный readback эффект с точными арендой, commit, одобренным содержимым и привязками pull request без повтора потерянного ACK.
// Branch receipts require confirmation time and QUEUED or FAILED state; pull requests also require SUCCEEDED, exact ref/URL and confirmation time.
// RU: Квитанции ветки требуют времени подтверждения и состояния QUEUED либо FAILED; pull request дополнительно требует SUCCEEDED, точных ref/URL и времени подтверждения.
func completeWriteBack(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, work *cp.ManagedConfigurationGitWriteBackWork, config Config, candidate integration.ConfigurationWriteBackCandidate, pullRequest integration.ConfigurationWriteBackPullRequest) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	response, err := control.CompleteManagedConfigurationGitWriteBackEffect(ctx, &cp.CompleteManagedConfigurationGitWriteBackEffectRequest{Lease: proto.Clone(work.GetLease()).(*cp.ManagedConfigurationGitWriteBackLease), Effect: work.GetEffect(), CandidateCommitSha: candidate.CommitSHA, ContentSha256: work.GetProposal().GetProposedContentSha256(), PullRequestRef: pullRequest.Ref, PullRequestUrl: pullRequest.URL})
	if err != nil {
		return 0, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validWriteBackReceipt(response.GetProposal(), work) || response.GetProposal().GetCandidateCommitSha() != candidate.CommitSHA {
		return 0, errConfigurationWriteBack
	}
	p := response.GetProposal()
	if p.GetBranchConfirmedAt() == nil || p.GetBranchConfirmedAt().CheckValid() != nil {
		return 0, errConfigurationWriteBack
	}
	if work.GetEffect() == cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_BRANCH {
		if p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_QUEUED && p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_FAILED {
			return 0, errConfigurationWriteBack
		}
	} else if p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_SUCCEEDED || p.GetPullRequestRef() != pullRequest.Ref || p.GetPullRequestUrl() != pullRequest.URL || p.GetPullRequestConfirmedAt() == nil || p.GetPullRequestConfirmedAt().CheckValid() != nil {
		return 0, errConfigurationWriteBack
	}
	return 1, nil
}

// validWriteBackReceipt requires a newer known-field proposal preserving exact configuration, source, connection, repository, approval and content pins.
// RU: validWriteBackReceipt требует новую версию предложения без неизвестных полей с сохранением точных привязок конфигурации, источника, подключения, репозитория, одобрения и содержимого.
// Kind, format, base-content hash and creation/expiry/approval times must remain exact, and the failure enum must be known.
// RU: Kind, format, хеш исходного содержимого и времена создания, истечения и одобрения должны совпадать точно, а failure enum должен быть известен.
// Callers separately validate transition state and effect evidence; identity matching alone never authorizes a write.
// RU: Вызывающий отдельно проверяет состояние перехода и доказательства эффекта; совпадение идентичности само по себе не разрешает запись.
func validWriteBackReceipt(p *cp.ManagedConfigurationGitWriteBack, work *cp.ManagedConfigurationGitWriteBackWork) bool {
	previous := work.GetProposal()
	if p == nil || p.GetKind() != previous.GetKind() || p.GetContentFormat() != previous.GetContentFormat() || p.GetBaseContentSha256() != previous.GetBaseContentSha256() || !proto.Equal(p.GetCreatedAt(), previous.GetCreatedAt()) || !proto.Equal(p.GetExpiresAt(), previous.GetExpiresAt()) || !proto.Equal(p.GetApprovedAt(), previous.GetApprovedAt()) {
		return false
	}
	if _, known := cp.ManagedConfigurationGitWriteBackFailure_name[int32(p.GetFailureCode())]; !known {
		return false
	}
	return p != nil && len(p.ProtoReflect().GetUnknown()) == 0 && p.GetRef() == previous.GetRef() && p.GetVersion() > previous.GetVersion() && p.GetConfigurationRef() == previous.GetConfigurationRef() && p.GetConfigurationVersion() == previous.GetConfigurationVersion() && p.GetSourceRef() == previous.GetSourceRef() && p.GetSourceVersion() == previous.GetSourceVersion() && p.GetConnectionRef() == previous.GetConnectionRef() && p.GetConnectionVersion() == previous.GetConnectionVersion() && p.GetApprovalDigest() == previous.GetApprovalDigest() && p.GetBaseCommitSha() == previous.GetBaseCommitSha() && p.GetProposedContentSha256() == previous.GetProposedContentSha256() && p.GetProposalBranch() == previous.GetProposalBranch() && p.GetRepositoryRef() == previous.GetRepositoryRef() && p.GetSourceRefName() == previous.GetSourceRefName() && p.GetPath() == previous.GetPath()
}
