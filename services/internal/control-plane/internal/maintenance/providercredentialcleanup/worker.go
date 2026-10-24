// Package providercredentialcleanup выполняет ограниченную фоновую очистку
// заменённого provider credential material.
package providercredentialcleanup

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// SafeCodeUnavailable classifies unavailable or resource-exhausted cleanup dependencies using one
	// repository-safe code without copying remote error text.
	// SafeCodeUnavailable классифицирует недоступность либо resource exhaustion cleanup зависимости
	// одним безопасным для repository кодом без копирования remote error текста.
	SafeCodeUnavailable = "PROVIDER_CREDENTIAL_CLEANUP_UNAVAILABLE"
	// SafeCodeRejected classifies rejected cleanup inputs or authority/precondition/conflict failures
	// without exposing their raw RPC message.
	// SafeCodeRejected классифицирует отклонённые cleanup inputs либо authority, precondition и
	// conflict отказы без раскрытия raw RPC сообщения.
	SafeCodeRejected = "PROVIDER_CREDENTIAL_CLEANUP_REJECTED"
	// SafeCodeTimeout classifies context or gRPC deadline exhaustion separately from generic cleanup
	// failure.
	// SafeCodeTimeout классифицирует исчерпание context либо gRPC deadline отдельно от общего отказа
	// cleanup.
	SafeCodeTimeout = "PROVIDER_CREDENTIAL_CLEANUP_TIMEOUT"
	// SafeCodeFailed provides the closed fallback cleanup failure code for errors outside the
	// recognized RPC/deadline classes.
	// SafeCodeFailed задаёт закрытый запасной cleanup failure код для ошибок вне известных RPC и
	// deadline классов.
	SafeCodeFailed = "PROVIDER_CREDENTIAL_CLEANUP_FAILED"
	// SafeCodeCASChanged identifies the exact CAS-snapshot ErrorInfo shape accepted from the
	// materializer boundary for owner-side recovery decisions.
	// SafeCodeCASChanged обозначает точную форму CAS snapshot ErrorInfo, принятую с границы
	// materializer для recovery решений владельца.
	SafeCodeCASChanged = "PROVIDER_CREDENTIAL_CLEANUP_CAS_CHANGED"

	// claimUnavailableReason names claim-path readiness degradation independently of an individual
	// task effect or finalization failure.
	// claimUnavailableReason задаёт причину ухудшения readiness claim пути независимо от отказа
	// эффекта либо финализации отдельной task.
	claimUnavailableReason = "provider_credential_cleanup_claim_unavailable"
	// minimumPollInterval sets the fifty-millisecond minimum configured cleanup poll interval before
	// worker construction.
	// minimumPollInterval задаёт минимальный configured cleanup poll интервал пятьдесят миллисекунд до
	// создания worker.
	minimumPollInterval = 50 * time.Millisecond
	// maximumPollInterval sets the one-minute maximum configured cleanup poll interval accepted by the
	// worker constructor.
	// maximumPollInterval задаёт максимальный configured cleanup poll интервал одна минута,
	// принимаемый конструктором worker.
	maximumPollInterval = time.Minute
	// minimumOperationTimeout sets the one-hundred-millisecond minimum budget used for claim, effect
	// and finalization operations.
	// minimumOperationTimeout задаёт минимальный бюджет сто миллисекунд для операций claim, эффекта и
	// финализации.
	minimumOperationTimeout = 100 * time.Millisecond
	// maximumOperationTimeout caps the configured claim/effect/finalization operation budget at thirty
	// seconds.
	// maximumOperationTimeout ограничивает configured бюджет операций claim, эффекта и финализации
	// тридцатью секундами.
	maximumOperationTimeout = 30 * time.Second
	// maximumIdleInterval supplies a five-second idle ceiling; shared IdleBackoff raises it to the
	// configured poll minimum when that minimum is larger.
	// maximumIdleInterval задаёт idle предел пять секунд; общий IdleBackoff поднимает его до
	// configured poll minimum, если minimum больше.
	maximumIdleInterval = 5 * time.Second
)

// Repository owns bounded cleanup claims and exact task/lease-owner/generation completion or
// failure transitions; the worker never supplies replacement owner identity.
// Repository владеет ограниченными cleanup claims и точными task, lease owner и generation
// переходами завершения либо отказа; worker не подставляет identity другого владельца.
type Repository interface {
	// ClaimProviderCredentialCleanupTasks claims a bounded task batch for the supplied lease owner
	// through authoritative repository state.
	// ClaimProviderCredentialCleanupTasks получает ограниченный пакет task claim для переданного lease
	// owner через авторитетное repository состояние.
	ClaimProviderCredentialCleanupTasks(context.Context, string, int32) ([]platformrepository.ProviderCredentialCleanupTask, error)
	// CompleteProviderCredentialCleanupTask records the exact claimed task generation and typed
	// cleanup receipt/observation through the domain owner.
	// CompleteProviderCredentialCleanupTask фиксирует точное поколение claimed task и типизированные
	// cleanup receipt либо observation через доменного владельца.
	CompleteProviderCredentialCleanupTask(context.Context, string, string, int64, entity.ProviderAuthorizationCleanupResult) (platformrepository.ProviderCredentialCleanupResult, error)
	// FailProviderCredentialCleanupTask records one closed safe failure code for the exact
	// task/lease-owner/generation tuple, leaving retry scheduling to the owner.
	// FailProviderCredentialCleanupTask фиксирует один закрытый безопасный failure код точного кортежа
	// task, lease owner и generation с сохранением retry scheduling у владельца.
	FailProviderCredentialCleanupTask(context.Context, string, string, int64, string) (platformrepository.ProviderCredentialCleanupResult, error)
}

// Materializer separates credential cleanup, authorization cleanup and metadata-only observation
// without giving an observation the meaning of a terminal deletion receipt.
// Materializer разделяет cleanup credentials, cleanup authorization и metadata-only observation
// без придания observation смысла terminal deletion receipt.
type Materializer interface {
	// CleanupProviderCredential cleans the exact task/account/generation and credential descriptor
	// with optional recovery identity and returns typed completion metadata.
	// CleanupProviderCredential очищает точные task, account, generation и credential descriptor с
	// необязательной recovery identity и возвращает типизированные completion metadata.
	CleanupProviderCredential(context.Context, string, string, int64, entity.ProviderCredentialDescriptor, ...*entity.ProviderCleanupRecoveryIdentity) (entity.ProviderAuthorizationCleanupResult, error)
	// ObserveAuthorizationCleanup reads authoritative cleanup metadata for an exact authorization
	// target without repeating a cleanup effect.
	// ObserveAuthorizationCleanup читает авторитетные cleanup metadata точной authorization цели без
	// повторения cleanup эффекта.
	ObserveAuthorizationCleanup(context.Context, entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupObservation, error)
	// CleanupAuthorization executes authorization-attempt or absence cleanup against the exact owner
	// target and returns typed receipt metadata.
	// CleanupAuthorization исполняет cleanup authorization attempt либо absence для точной owner цели
	// и возвращает типизированные receipt metadata.
	CleanupAuthorization(context.Context, entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupResult, error)
}

// Config binds cleanup claims to one lease owner with bounded batch size, polling interval and
// per-operation timeout.
// Config связывает cleanup claims с одним lease owner с ограниченными размером пакета, polling
// интервалом и timeout каждой операции.
type Config struct {
	LeaseOwner       string
	BatchSize        int32
	PollInterval     time.Duration
	OperationTimeout time.Duration
}

// Worker coordinates claimed cleanup effects and owner finalization with claim-path readiness;
// successful claiming is not proof of every materializer capability.
// Worker согласует эффекты claimed cleanup и финализацию владельца с readiness claim пути;
// успешный claim не доказывает исправность каждой materializer capability.
type Worker struct {
	repository   Repository
	materializer Materializer
	health       *serviceruntime.Readiness
	logger       *slog.Logger
	config       Config
}

// New requires nonnil ports/health/logger and validates owner length, batch and poll/operation
// budgets before returning an unstarted worker.
// New требует непустые ports, health и logger и проверяет длину owner, batch и бюджеты poll и
// операции до возврата незапущенного worker.
func New(
	repository Repository,
	materializer Materializer,
	health *serviceruntime.Readiness,
	logger *slog.Logger,
	config Config,
) (*Worker, error) {
	if repository == nil || materializer == nil || health == nil || logger == nil ||
		config.LeaseOwner == "" || len(config.LeaseOwner) > 128 ||
		config.BatchSize < 1 || config.BatchSize > 16 ||
		config.PollInterval < minimumPollInterval || config.PollInterval > maximumPollInterval ||
		config.OperationTimeout < minimumOperationTimeout || config.OperationTimeout > maximumOperationTimeout {
		return nil, errors.New("provider credential cleanup worker configuration is invalid")
	}
	return &Worker{
		repository: repository, materializer: materializer, health: health,
		logger: logger, config: config,
	}, nil
}

// Run updates readiness only from claim-cycle outcomes, uses shared idle backoff, logs readiness
// transitions and returns caller cancellation after each joined task batch.
// Run обновляет readiness только по исходам claim цикла, использует общий idle backoff, записывает
// readiness переходы и возвращает отмену caller после каждого завершённого пакета task.
func (worker *Worker) Run(ctx context.Context) error {
	idleBackoff := serviceruntime.NewIdleBackoff(worker.config.PollInterval, maximumIdleInterval)
	for {
		processed, err := worker.runCycle(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if worker.health.Set(false, claimUnavailableReason) {
				worker.logger.WarnContext(ctx, "provider credential cleanup claim degraded", "error_class", "postgresql")
			}
		} else if worker.health.Set(true, "ready") {
			worker.logger.InfoContext(ctx, "provider credential cleanup claim restored")
		}

		timer := time.NewTimer(idleBackoff.Next(err == nil && processed > 0))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// runCycle bounds a claim, processes each claimed task concurrently and joins the entire batch,
// logging fixed finalization stages while returning only claim failure as cycle failure.
// runCycle ограничивает claim, параллельно обрабатывает каждую claimed task и ожидает весь пакет,
// записывая фиксированные stages финализации и возвращая ошибкой цикла только отказ claim.
func (worker *Worker) runCycle(ctx context.Context) (int, error) {
	claimCtx, cancelClaim := context.WithTimeout(ctx, worker.config.OperationTimeout)
	tasks, err := worker.repository.ClaimProviderCredentialCleanupTasks(
		claimCtx,
		worker.config.LeaseOwner,
		worker.config.BatchSize,
	)
	cancelClaim()
	if err != nil {
		return 0, err
	}

	var wait sync.WaitGroup
	wait.Add(len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			defer wait.Done()
			if stage := worker.processTask(ctx, task); stage != "" && ctx.Err() == nil {
				worker.logger.WarnContext(ctx, "provider credential cleanup task finalization failed", "stage", stage)
			}
		}()
	}
	wait.Wait()
	return len(tasks), nil
}

// processTask dispatches the closed cleanup target kind, preserves metadata observations
// separately from receipts and finalizes under the parent context only while it remains active;
// cancellation leaves the claim for owner recovery.
// processTask выбирает закрытый вид cleanup цели, сохраняет metadata observations отдельно от
// receipts и финализирует под parent context лишь пока он активен; отмена оставляет claim для
// owner recovery.
func (worker *Worker) processTask(ctx context.Context, task platformrepository.ProviderCredentialCleanupTask) string {
	cleanupCtx, cancelCleanup := context.WithTimeout(ctx, worker.config.OperationTimeout)
	var receipt entity.ProviderAuthorizationCleanupResult
	var cleanupErr error
	switch task.TargetKind {
	case "CREDENTIAL":
		receipt, cleanupErr = worker.materializer.CleanupProviderCredential(cleanupCtx, task.Ref, task.AccountRef, task.Generation, task.Credential, task.Recovery)
	case "AUTHORIZATION_METADATA":
		var observation entity.ProviderAuthorizationCleanupObservation
		observation, cleanupErr = worker.materializer.ObserveAuthorizationCleanup(cleanupCtx, task.Authorization)
		if cleanupErr == nil {
			receipt.Observation = &observation
		}
	case "AUTHORIZATION_ATTEMPT", "AUTHORIZATION_ABSENCE":
		receipt, cleanupErr = worker.materializer.CleanupAuthorization(cleanupCtx, task.Authorization)
	default:
		cleanupErr = status.Error(codes.InvalidArgument, "provider cleanup target kind is invalid")
	}
	cancelCleanup()
	if ctx.Err() != nil {
		return ""
	}

	finalizeCtx, cancelFinalize := context.WithTimeout(ctx, worker.config.OperationTimeout)
	defer cancelFinalize()
	if cleanupErr != nil {
		_, err := worker.repository.FailProviderCredentialCleanupTask(
			finalizeCtx,
			task.Ref,
			worker.config.LeaseOwner,
			task.Generation,
			safeErrorCode(cleanupErr),
		)
		if err != nil {
			return "fail"
		}
		return ""
	}
	if _, err := worker.repository.CompleteProviderCredentialCleanupTask(
		finalizeCtx,
		task.Ref,
		worker.config.LeaseOwner,
		task.Generation,
		receipt,
	); err != nil {
		return "complete"
	}
	return ""
}

// safeErrorCode maps errors to five closed codes; CAS recovery requires FailedPrecondition plus
// exactly one matching ErrorInfo with empty metadata, while authentication of that RPC belongs to
// the client boundary.
// safeErrorCode отображает ошибки в пять закрытых кодов; CAS recovery требует FailedPrecondition и
// ровно один совпадающий ErrorInfo с пустыми metadata, а аутентификация RPC принадлежит client
// boundary.
func safeErrorCode(err error) string {
	if status.Code(err) == codes.FailedPrecondition {
		details := status.Convert(err).Details()
		if len(details) == 1 {
			info, ok := details[0].(*errdetails.ErrorInfo)
			if ok && info.Domain == "kodex.provider_credential_cleanup" && info.Reason == "CAS_SNAPSHOT_CHANGED" && len(info.Metadata) == 0 {
				return SafeCodeCASChanged
			}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
		return SafeCodeTimeout
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted:
		return SafeCodeUnavailable
	case codes.InvalidArgument, codes.Unauthenticated, codes.PermissionDenied,
		codes.NotFound, codes.AlreadyExists, codes.FailedPrecondition, codes.Aborted:
		return SafeCodeRejected
	default:
		return SafeCodeFailed
	}
}
