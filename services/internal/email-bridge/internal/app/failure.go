package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"os"

	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failureStage string

const (
	stageEnvironment   failureStage = "environment"
	stageTLS           failureStage = "transport_tls"
	stageCertificate   failureStage = "certificate_read"
	stagePrivateKey    failureStage = "private_key_read"
	stageKeyPair       failureStage = "keypair_validation"
	stageCA            failureStage = "ca_read"
	stageDSN           failureStage = "dsn_read"
	stageDSNValidation failureStage = "dsn_validation"
	stageDatabaseOpen  failureStage = "database_open"
	stageDatabaseReady failureStage = "database_readiness"
	stageTelemetry     failureStage = "telemetry"
	stageMetrics       failureStage = "metrics"
	stageAuthority     failureStage = "authority_client"
	stageConfiguration failureStage = "configuration_load"
	stagePins          failureStage = "configuration_pins"
	stageWatermark     failureStage = "configuration_watermark"
	stageService       failureStage = "configuration_service"
	stageReadback      failureStage = "configuration_owner_readback"
	stageTechnical     failureStage = "technical_listener"
	stageHTTPS         failureStage = "https_listener"
	stageShutdown      failureStage = "shutdown"
	failureMessage                  = "Email bridge stopped unexpectedly"
)

type runtimeFailure struct {
	stage failureStage
	cause error
}

// Форматирование wrapper никогда не раскрывает cause; Unwrap сохраняет cancellation.
func (*runtimeFailure) Error() string   { return failureMessage }
func (f *runtimeFailure) Unwrap() error { return f.cause }

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

// LogFailure выводит только закрытые stage/class, без Error(), путей и payload.
func LogFailure(logger *slog.Logger, err error) {
	stage, class := failureFields(err)
	logger.Error(failureMessage, "stage", stage, "error_class", class)
}

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
		switch pg.Code {
		case "28P01", "28000":
			return "database_authentication"
		case "42501":
			return "database_permission"
		case "3D000":
			return "database_missing"
		case "53300", "57P03":
			return "database_unavailable"
		default:
			return "database"
		}
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
