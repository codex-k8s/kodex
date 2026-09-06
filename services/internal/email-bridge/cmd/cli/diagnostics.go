package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/jackc/pgx/v5/pgconn"
)

type migrationStage string

const (
	stageConfiguration      migrationStage = "configuration"
	stageArguments          migrationStage = "arguments"
	stageDSNRead            migrationStage = "dsn_read"
	stageDatabaseOpen       migrationStage = "database_open"
	stageDatabaseConnect    migrationStage = "database_connect"
	stageDialect            migrationStage = "dialect"
	stageMigration          migrationStage = "migration"
	stageStatus             migrationStage = "status"
	migrationFailureMessage                = "Email bridge migration failed"
)

type migrationFailure struct {
	stage migrationStage
	cause error
}

// Даже случайное форматирование wrapper не раскрывает вложенную ошибку.
func (*migrationFailure) Error() string { return migrationFailureMessage }

func failure(stage migrationStage, err error) error {
	if err == nil {
		return nil
	}
	return &migrationFailure{stage: stage, cause: err}
}

func reportFailure(output io.Writer, err error) {
	stage, class := "unknown", "unknown"
	var failed *migrationFailure
	if errors.As(err, &failed) {
		switch failed.stage {
		case stageConfiguration, stageArguments, stageDSNRead, stageDatabaseOpen, stageDatabaseConnect, stageDialect, stageMigration, stageStatus:
			stage = string(failed.stage)
		}
		class = failureClass(failed.cause)
		if failed.stage == stageDSNRead {
			class = "secure_file"
		}
		if failed.stage == stageConfiguration || failed.stage == stageArguments || failed.stage == stageDialect {
			class = "configuration"
		}
	}
	fmt.Fprintf(output, "%s stage=%s error_class=%s\n", migrationFailureMessage, stage, class)
}

// Только закрытые классы: Error(), SQLSTATE вне allowlist, SQL/DSN и пути не выводятся.
func failureClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var database *pgconn.PgError
	if errors.As(err, &database) {
		switch database.Code {
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
	var unknownCA x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	var hostname x509.HostnameError
	var tlsVerification *tls.CertificateVerificationError
	if errors.As(err, &unknownCA) || errors.As(err, &invalidCertificate) || errors.As(err, &hostname) || errors.As(err, &tlsVerification) {
		return "tls_verification"
	}
	var path *os.PathError
	if errors.As(err, &path) {
		return "filesystem"
	}
	var configuration *pgconn.ParseConfigError
	if errors.As(err, &configuration) {
		return "connection_configuration"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "timeout"
		}
		return "network"
	}
	return "unknown"
}
