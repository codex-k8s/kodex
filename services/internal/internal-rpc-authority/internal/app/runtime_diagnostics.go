package app

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// runtimeStage names the closed startup or runtime phase attached to issuer/verifier failures; arbitrary values are normalized before public diagnostics are formatted.
// runtimeStage именует закрытый этап запуска или работы, добавляемый к ошибкам issuer/verifier; произвольные значения нормализуются перед форматированием публичной диагностики.
type runtimeStage string

const (
	// stageConfiguration marks configuration loading and validation before observability or external dependency setup begins.
	// stageConfiguration помечает загрузку и проверку конфигурации до настройки наблюдаемости или внешних зависимостей.
	stageConfiguration runtimeStage = "CONFIGURATION"
	// stageObservability marks telemetry composition so its failure is distinct from later database or authority construction errors.
	// stageObservability помечает сборку телеметрии, отделяя её ошибку от последующих ошибок БД или создания authority.
	stageObservability runtimeStage = "OBSERVABILITY"
	// stagePostgres labels PostgreSQL initialization in the runtime sequence; SQL text and connection parameters are not part of the label.
	// stagePostgres помечает инициализацию PostgreSQL в последовательности runtime; SQL-текст и параметры соединения в метку не входят.
	stagePostgres runtimeStage = "POSTGRES"
	// stageSnapshotLoad identifies loading of the authority snapshot before construction and activation; it does not claim successful activation.
	// stageSnapshotLoad обозначает загрузку снимка authority до создания компонента и активации и не утверждает успешную активацию.
	stageSnapshotLoad runtimeStage = "SNAPSHOT_LOAD"
	// stageAuthorityConstruction labels creation of the authority application separately from loading its snapshot and exposing local servers.
	// stageAuthorityConstruction помечает создание приложения authority отдельно от загрузки снимка и открытия локальных серверов.
	stageAuthorityConstruction runtimeStage = "AUTHORITY_CONSTRUCTION"
	// stageReadbackClient identifies readback-client setup without exposing endpoint or TLS configuration values in public diagnostics.
	// stageReadbackClient обозначает настройку клиента контрольного чтения без раскрытия endpoint или значений TLS-конфигурации в публичной диагностике.
	stageReadbackClient runtimeStage = "READBACK_CLIENT"
	// stageRestoreClient labels restore-client setup independently of the later restore verification and open stages.
	// stageRestoreClient помечает настройку клиента восстановления отдельно от последующих этапов проверки и открытия восстановления.
	stageRestoreClient runtimeStage = "RESTORE_CLIENT"
	// stageRestoreVerify labels the restore-verification startup step, not permission to bypass verification after an error.
	// stageRestoreVerify помечает стартовый шаг проверки восстановления, а не разрешение обойти проверку после ошибки.
	stageRestoreVerify runtimeStage = "RESTORE_VERIFY"
	// stageSnapshotActivation identifies the activation step separately from snapshot loading so diagnostics do not confuse obtaining bytes with serving them.
	// stageSnapshotActivation обозначает шаг активации отдельно от загрузки снимка, чтобы диагностика не смешивала получение данных с их обслуживанием.
	stageSnapshotActivation runtimeStage = "SNAPSHOT_ACTIVATION"
	// stageRestoreOpen labels opening the restore runtime after snapshot activation; it is diagnostic metadata, not a restore directive.
	// stageRestoreOpen помечает открытие runtime восстановления после активации снимка; это диагностические метаданные, а не директива восстановления.
	stageRestoreOpen runtimeStage = "RESTORE_OPEN"
	// stageLocalServers labels local RPC and technical-server setup before the worker runtime phase.
	// stageLocalServers помечает настройку локальных RPC- и технического серверов до этапа работы исполнителей.
	stageLocalServers runtimeStage = "LOCAL_SERVERS"
	// stageRuntime labels failures after entering the worker/server runtime phase, without serializing workload inputs or private causes.
	// stageRuntime помечает ошибки после входа в этап работы исполнителей и серверов без сериализации входов workload или закрытых причин.
	stageRuntime runtimeStage = "RUNTIME"
	// diagnosticUnknown is the bounded fallback for unrecognized diagnostic fields, error classes or SQLSTATE values, preventing raw caller or adapter strings from becoming output labels.
	// diagnosticUnknown — ограниченное запасное значение для неизвестных диагностических полей, классов ошибок или SQLSTATE, не допускающее попадание исходных строк вызывающего или адаптера в метки вывода.
	diagnosticUnknown = "UNKNOWN"
	// diagnosticNone distinguishes an absent domain, context or SQLSTATE classification from an encountered but unrecognized value represented by UNKNOWN.
	// diagnosticNone отличает отсутствующую классификацию домена, контекста или SQLSTATE от встретившегося неизвестного значения UNKNOWN.
	diagnosticNone = "NONE"
	// diagnosticDeadline labels a matched context deadline; context state is recorded separately from the original database or network error class.
	// diagnosticDeadline помечает найденный срок контекста; состояние контекста фиксируется отдельно от исходного класса ошибки БД или сети.
	diagnosticDeadline = "DEADLINE"
	// diagnosticCanceled labels matched context cancellation, with deadline taking precedence when both are present in the error chain.
	// diagnosticCanceled помечает найденную отмену контекста; при наличии в цепочке и отмены, и срока приоритет имеет срок.
	diagnosticCanceled = "CANCELED"
	// diagnosticNetwork identifies a recognized non-timeout network error without exposing the network operation, address or wrapped message.
	// diagnosticNetwork обозначает распознанную сетевую ошибку без таймаута, не раскрывая сетевую операцию, адрес или обёрнутое сообщение.
	diagnosticNetwork = "NETWORK"
	// diagnosticNetworkTimeout identifies a recognized network error whose Timeout method reports true; it remains separate from the context-state field.
	// diagnosticNetworkTimeout обозначает распознанную сетевую ошибку, у которой Timeout возвращает true; это значение отдельно от поля состояния контекста.
	diagnosticNetworkTimeout = "NETWORK_TIMEOUT"
)

// runtimeStageError retains the runtime phase and original cause for typed matching while exposing only a fixed Error message. SafeRuntimeFailure independently normalizes the phase before CLI output.
// runtimeStageError хранит этап runtime и исходную причину для типизированного сопоставления, показывая только фиксированное сообщение Error. SafeRuntimeFailure отдельно нормализует этап перед выводом CLI.
type runtimeStageError struct {
	stage runtimeStage
	cause error
}

// Error returns a fixed runtime-stage message and never formats the stored stage or private cause.
// Error возвращает фиксированное сообщение об этапе runtime, не форматируя сохранённый этап или закрытую причину.
func (err *runtimeStageError) Error() string { return "authority runtime stage failed" }

// Unwrap returns the original cause so error-chain classification and cancellation checks survive the stage wrapper.
// Unwrap возвращает исходную причину, сохраняя классификацию цепочки ошибок и проверки отмены через обёртку этапа.
func (err *runtimeStageError) Unwrap() error { return err.cause }

// SafeRuntimeFailure produces the issuer/verifier CLI diagnostic solely from closed mode, stage, domain kind, error class, SQLSTATE and context labels. It does not call the cause's Error method or format private database details, paths or endpoints; preserving the error chain is separate from serializing it.
// SafeRuntimeFailure строит диагностику CLI issuer/verifier только из закрытых меток режима, этапа, доменного класса, класса ошибки, SQLSTATE и контекста. Метод Error причины не вызывается, закрытые подробности БД, пути и endpoint не форматируются; сохранение цепочки ошибок отличается от её сериализации.
func SafeRuntimeFailure(mode Mode, err error) string {
	role := diagnosticUnknown
	if mode == ModeIssuer || mode == ModeVerifier {
		role = string(mode)
	}
	stage := diagnosticUnknown
	var staged *runtimeStageError
	if errors.As(err, &staged) {
		switch staged.stage {
		case stageConfiguration, stageObservability, stagePostgres, stageSnapshotLoad,
			stageAuthorityConstruction, stageReadbackClient, stageRestoreClient,
			stageRestoreVerify, stageSnapshotActivation, stageRestoreOpen, stageLocalServers, stageRuntime:
			stage = string(staged.stage)
		}
	}
	kind := diagnosticNone
	var domain *failure.Error
	if errors.As(err, &domain) {
		kind = diagnosticUnknown
		switch domain.Kind {
		case failure.InvalidRequest, failure.NotFound, failure.Unauthenticated,
			failure.PermissionDenied, failure.OperationNotAllowed, failure.AuthorityRejected,
			failure.BindingMismatch, failure.ReplayDetected, failure.SnapshotRejected,
			failure.PersistenceUnavailable, failure.Internal:
			kind = string(domain.Kind)
		}
	}
	contextState := diagnosticNone
	if errors.Is(err, context.DeadlineExceeded) {
		contextState = diagnosticDeadline
	} else if errors.Is(err, context.Canceled) {
		contextState = diagnosticCanceled
	}
	class, sqlstate := runtimeErrorClass(err)
	return fmt.Sprintf("internal-rpc-authority runtime failed: mode=%s stage=%s domain_kind=%s error_class=%s sqlstate=%s context_state=%s", role, stage, kind, class, sqlstate, contextState)
}

// runtimeErrorClass classifies in order PostgreSQL with allowlisted SQLSTATE, snapshot rollback, gRPC status, net.OpError, deadline, cancellation and other net.Error, then UNKNOWN. The original class can survive a joined context failure; adapter messages and arbitrary SQLSTATE or gRPC codes are never returned.
// runtimeErrorClass последовательно классифицирует PostgreSQL с разрешённым SQLSTATE, откат снимка, gRPC-статус, net.OpError, срок, отмену и остальные net.Error, затем UNKNOWN. Исходный класс может сохраняться при присоединённой ошибке контекста; сообщения адаптеров и произвольные SQLSTATE либо gRPC-коды не возвращаются.
func runtimeErrorClass(err error) (string, string) {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		code := diagnosticUnknown
		switch postgres.Code {
		case "08000", "08001", "08003", "08004", "08006", "08007", "08P01",
			"22001", "22003", "22P02", "23502", "23503", "23505", "23514",
			"28000", "28P01", "40001", "40P01", "42501", "42601", "42703",
			"42883", "42P01", "53300", "53400", "57014", "57P01", "57P02", "57P03":
			code = postgres.Code
		}
		return "POSTGRES", code
	}
	// Retry exhaustion may join ctx.Err() to the original refusal; keep that cause's class separate from context_state instead of replacing it with a timeout label.
	// Исчерпание повторов может присоединить ctx.Err() к исходному отказу; его класс сохраняется отдельно от context_state вместо подмены меткой таймаута.
	if errors.Is(err, repository.ErrSnapshotRollback) {
		return "SNAPSHOT_REJECTED", diagnosticNone
	}
	var rpc interface{ GRPCStatus() *status.Status }
	if errors.As(err, &rpc) && rpc.GRPCStatus() != nil {
		switch rpc.GRPCStatus().Code() {
		case codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.DeadlineExceeded,
			codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.ResourceExhausted,
			codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented,
			codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unauthenticated:
			return "GRPC_" + rpc.GRPCStatus().Code().String(), diagnosticNone
		}
		return "GRPC_UNKNOWN", diagnosticNone
	}
	var network *net.OpError
	if errors.As(err, &network) {
		if network.Timeout() {
			return diagnosticNetworkTimeout, diagnosticNone
		}
		return diagnosticNetwork, diagnosticNone
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return diagnosticDeadline, diagnosticNone
	}
	if errors.Is(err, context.Canceled) {
		return diagnosticCanceled, diagnosticNone
	}
	var otherNetwork net.Error
	if errors.As(err, &otherNetwork) {
		if otherNetwork.Timeout() {
			return diagnosticNetworkTimeout, diagnosticNone
		}
		return diagnosticNetwork, diagnosticNone
	}
	return diagnosticUnknown, diagnosticNone
}
