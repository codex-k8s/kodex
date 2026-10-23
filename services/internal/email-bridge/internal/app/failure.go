package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"os"

	"github.com/codex-k8s/kodex/libs/go/observability/errorclass"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// failureStage identifies a closed diagnostic process phase without embedding paths, credentials or upstream error text.
// failureStage задаёт закрытый диагностический этап процесса без путей, реквизитов или текста ошибки внешнего источника.
type failureStage string

const (
	// stageEnvironment identifies process environment parsing and local configuration validation failures.
	// stageEnvironment обозначает ошибки разбора окружения процесса и локальной валидации конфигурации.
	stageEnvironment failureStage = "environment"
	// stageTLS identifies setup of the inbound profile-specific transport configuration.
	// stageTLS обозначает настройку входящего транспорта выбранного профиля.
	stageTLS failureStage = "transport_tls"
	// stageCertificate identifies protected server-certificate reads before key-pair construction.
	// stageCertificate обозначает защищённое чтение сертификата сервера до сборки пары ключей.
	stageCertificate failureStage = "certificate_read"
	// stagePrivateKey identifies protected private-key reads without exposing the file path or bytes.
	// stagePrivateKey обозначает защищённое чтение закрытого ключа без раскрытия пути или байтов.
	stagePrivateKey failureStage = "private_key_read"
	// stageKeyPair identifies certificate/private-key parsing and pair validation.
	// stageKeyPair обозначает разбор сертификата и закрытого ключа и проверку пары.
	stageKeyPair failureStage = "keypair_validation"
	// stageCA identifies protected trust-bundle loading or PEM trust-pool construction failures.
	// stageCA обозначает ошибки защищённого чтения доверенного набора либо сборки PEM-пула доверия.
	stageCA failureStage = "ca_read"
	// stageDSN identifies the outer database-credential read and validation setup phase.
	// stageDSN обозначает внешний этап чтения и подготовки проверки реквизитов БД.
	stageDSN failureStage = "dsn_read"
	// stageDSNValidation identifies rejection of database destination, role or TLS/query overrides.
	// stageDSNValidation обозначает отказ из-за адреса, роли БД либо переопределений TLS/query.
	stageDSNValidation failureStage = "dsn_validation"
	// stageDatabaseOpen identifies pool construction, not a successful database readiness check.
	// stageDatabaseOpen обозначает создание пула, не успешную проверку готовности БД.
	stageDatabaseOpen failureStage = "database_open"
	// stageDatabaseReady identifies the bounded receipt-repository startup barrier.
	// stageDatabaseReady обозначает ограниченный startup barrier репозитория квитанций.
	stageDatabaseReady failureStage = "database_readiness"
	// stageTelemetry identifies canonical tracing/Sentry runtime construction.
	// stageTelemetry обозначает создание канонического runtime трассировки и Sentry.
	stageTelemetry failureStage = "telemetry"
	// stageMetrics identifies registration of the service's technical and business collectors.
	// stageMetrics обозначает регистрацию технических и бизнесовых collectors сервиса.
	stageMetrics failureStage = "metrics"
	// stageAuthority identifies construction of the protected control-plane owner client.
	// stageAuthority обозначает создание защищённого клиента владельца control plane.
	stageAuthority failureStage = "authority_client"
	// stageConfiguration identifies loading a single complete projected mailbox snapshot.
	// stageConfiguration обозначает чтение одного полного проецируемого снимка ящиков.
	stageConfiguration failureStage = "configuration_load"
	// stagePins identifies exact deployment publication-mode/revision/digest validation.
	// stagePins обозначает точную проверку режима публикации, ревизии и дайджеста развёртывания.
	stagePins failureStage = "configuration_pins"
	// stageWatermark identifies durable configuration acceptance before local publication.
	// stageWatermark обозначает устойчивый приём конфигурации до локальной публикации.
	stageWatermark failureStage = "configuration_watermark"
	// stageService identifies construction of the mailbox service from an accepted snapshot.
	// stageService обозначает создание сервиса ящиков из принятого снимка.
	stageService failureStage = "configuration_service"
	// stageReadback identifies owner acknowledgement after successful local snapshot publication.
	// stageReadback обозначает подтверждение владельцем после успешной локальной публикации снимка.
	stageReadback failureStage = "configuration_owner_readback"
	// stageTechnical identifies binding the bounded health/metrics listener.
	// stageTechnical обозначает привязку ограниченного listener готовности и метрик.
	stageTechnical failureStage = "technical_listener"
	// stageHTTPS identifies binding the mailbox API listener before profile-specific TLS wrapping and serving.
	// stageHTTPS обозначает привязку API-listener ящиков до профильного TLS и обслуживания.
	stageHTTPS failureStage = "https_listener"
	// stageShutdown identifies ordered bounded listener and worker shutdown failures.
	// stageShutdown обозначает ошибки упорядоченного ограниченного завершения listeners и workers.
	stageShutdown failureStage = "shutdown"
	// failureMessage is the only public wrapper/log text, preventing cause formatting from leaking sensitive details.
	// failureMessage задаёт единственный публичный текст wrapper и лога, не позволяя форматированию причины раскрыть закрытые подробности.
	failureMessage = "Email bridge stopped unexpectedly"
)

// runtimeFailure retains a private typed cause for classification and errors.Is/As while exposing only a constant error message.
// runtimeFailure сохраняет закрытую типизированную причину для классификации и errors.Is/As, раскрывая только постоянный текст ошибки.
type runtimeFailure struct {
	stage failureStage
	cause error
}

// Error returns only the fixed diagnostic message and never formats the private cause.
// Error возвращает только фиксированное диагностическое сообщение и никогда не форматирует закрытую причину.
func (*runtimeFailure) Error() string { return failureMessage }

// Unwrap preserves typed cause and cancellation identity for internal inspection without including it in Error output.
// Unwrap сохраняет тип причины и идентичность отмены для внутренней проверки, не включая их в вывод Error.
func (f *runtimeFailure) Unwrap() error { return f.cause }

// failure leaves nil untouched and wraps only previously unclassified failures so the earliest precise stage survives outer boundaries.
// failure сохраняет nil и оборачивает лишь ранее неклассифицированные ошибки, сохраняя первый точный этап на внешних границах.
func failure(stage failureStage, err error) error {
	if err == nil {
		return nil
	}
	var prior *runtimeFailure
	if errors.As(err, &prior) {
		return err
	}
	return &runtimeFailure{stage: stage, cause: err}
}

// LogFailure emits the fixed message and bounded stage/class fields without formatting the raw error, paths or payload.
// LogFailure выводит фиксированное сообщение и ограниченные stage/class без форматирования сырой ошибки, путей или содержимого.
func LogFailure(logger *slog.Logger, err error) {
	stage, class := failureFields(err)
	logger.Error(failureMessage, "stage", stage, "error_class", class)
}

// failureFields admits only registered runtime-failure stages and otherwise returns fixed unknown labels, regardless of caller-supplied strings.
// failureFields допускает только зарегистрированные этапы runtime-ошибки, иначе возвращая фиксированные unknown независимо от входных строк.
func failureFields(err error) (string, string) {
	var f *runtimeFailure
	if !errors.As(err, &f) {
		return "unknown", "unknown"
	}
	switch f.stage {
	case stageEnvironment, stageTLS, stageCertificate, stagePrivateKey, stageKeyPair, stageCA,
		stageDSN, stageDSNValidation, stageDatabaseOpen, stageDatabaseReady, stageTelemetry,
		stageMetrics, stageAuthority, stageConfiguration, stagePins, stageWatermark,
		stageService, stageReadback, stageTechnical, stageHTTPS, stageShutdown:
	default:
		return "unknown", "unknown"
	}
	return string(f.stage), failureClass(f.cause)
}

// failureClass classifies wrapped cancellation, domain, database, TLS, filesystem, network and typed gRPC errors into a closed label set.
// It avoids status.FromError on untyped errors because that path may call a sensitive or unsafe Error method.
// failureClass классифицирует обёрнутые ошибки отмены, домена, БД, TLS, файлов, сети и типизированного gRPC в закрытый набор меток.
// status.FromError для нетипизированных ошибок не используется, поскольку он может вызвать раскрывающий данные либо небезопасный Error.
func failureClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, errs.Invalid) {
		return "invalid"
	}
	if errors.Is(err, errs.Unavailable) {
		return "unavailable"
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return errorclass.Postgres(pg.Code)
	}
	var parse *pgconn.ParseConfigError
	if errors.As(err, &parse) {
		return "connection_configuration"
	}
	var ca x509.UnknownAuthorityError
	var cert x509.CertificateInvalidError
	var host x509.HostnameError
	var verify *tls.CertificateVerificationError
	if errors.As(err, &ca) || errors.As(err, &cert) || errors.As(err, &host) || errors.As(err, &verify) {
		return "tls_verification"
	}
	var path *os.PathError
	if errors.As(err, &path) {
		return "filesystem"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "timeout"
		}
		return "network"
	}
	// FromError вызывает Error() у нетипизированной ошибки; используем только GRPCStatus.
	var rpc interface{ GRPCStatus() *status.Status }
	if errors.As(err, &rpc) && rpc.GRPCStatus() != nil {
		switch rpc.GRPCStatus().Code() {
		case codes.Unavailable:
			return "rpc_unavailable"
		case codes.PermissionDenied:
			return "rpc_forbidden"
		case codes.Unauthenticated:
			return "rpc_unauthenticated"
		case codes.DeadlineExceeded:
			return "timeout"
		default:
			return "rpc"
		}
	}
	return "unknown"
}
