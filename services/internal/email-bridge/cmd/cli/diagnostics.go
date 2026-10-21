package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/observability/errorclass"
	"github.com/jackc/pgx/v5/pgconn"
)

// migrationStage names the closed CLI phases used by safe diagnostics, independently of arbitrary upstream error messages.
// migrationStage задаёт закрытые этапы CLI для безопасной диагностики независимо от произвольных ошибок внешнего источника.
type migrationStage string

const (
	// stageConfiguration identifies missing or malformed migration-command environment settings.
	// stageConfiguration обозначает отсутствующие или неверные настройки окружения команды миграции.
	stageConfiguration migrationStage = "configuration"
	// stageArguments identifies an unsupported command or wrong argument count before credentials are opened.
	// stageArguments обозначает неподдержанную команду или неверное число аргументов до чтения реквизитов.
	stageArguments migrationStage = "arguments"
	// stageDSNRead identifies protected DSN-file read failures without exposing paths or bytes.
	// stageDSNRead обозначает отказ защищённого чтения DSN без раскрытия пути или байтов.
	stageDSNRead migrationStage = "dsn_read"
	// stageDatabaseOpen identifies database handle construction rather than successful connectivity.
	// stageDatabaseOpen обозначает создание дескриптора БД, не успешное соединение.
	stageDatabaseOpen migrationStage = "database_open"
	// stageDatabaseConnect identifies readiness ping and its bounded connection-wait outcome.
	// stageDatabaseConnect обозначает ping готовности и исход ограниченного ожидания соединения.
	stageDatabaseConnect migrationStage = "database_connect"
	// stageDialect identifies failure to select the PostgreSQL dialect before invoking a Goose command.
	// stageDialect обозначает отказ выбора PostgreSQL-диалекта до вызова команды Goose.
	stageDialect migrationStage = "dialect"
	// stageMigration identifies a single Goose up execution, which is not retried by connection recovery.
	// stageMigration обозначает однократный Goose up, который восстановление соединения не повторяет.
	stageMigration migrationStage = "migration"
	// stageStatus identifies the Goose migration-status command and any version-table setup it requires.
	// stageStatus обозначает команду статуса миграций Goose и требуемую ей подготовку таблицы версий.
	stageStatus migrationStage = "status"
	// migrationFailureMessage fixes the only CLI-owned public error prefix without private upstream details.
	// migrationFailureMessage фиксирует единственный собственный публичный префикс ошибки CLI без закрытых внешних подробностей.
	migrationFailureMessage = "Email bridge migration failed"
)

// migrationFailure keeps a private stage/cause pair for the CLI's explicit classifier and exposes only fixed Error text.
// migrationFailure хранит закрытую пару этап/причина для явного классификатора CLI, раскрывая лишь фиксированный Error.
type migrationFailure struct {
	stage migrationStage
	cause error
}

// Error never formats the nested cause, including when the wrapper is accidentally passed to a generic formatter.
// Error никогда не форматирует вложенную причину, даже при случайной передаче wrapper общему форматированию.
func (*migrationFailure) Error() string { return migrationFailureMessage }

// failure leaves successful nil untouched and wraps a failed command phase for explicit safe CLI reporting.
// failure сохраняет успешный nil и оборачивает отказ этапа команды для явного безопасного отчёта CLI.
func failure(stage migrationStage, err error) error {
	if err == nil {
		return nil
	}
	return &migrationFailure{stage: stage, cause: err}
}

// reportFailure writes only fixed stage/class labels and, for exhausted waits, a separate deadline/canceled status.
// It classifies the original network cause rather than hiding it behind budget exhaustion and never prints the raw cause.
// reportFailure записывает только фиксированные stage/class, а для исчерпанного ожидания — отдельный deadline/canceled.
// Классифицируется исходная сетевая причина, не скрытая исчерпанием бюджета, и никогда не печатается сырая ошибка.
func reportFailure(output io.Writer, err error) {
	stage, class := "unknown", "unknown"
	var failed *migrationFailure
	if errors.As(err, &failed) {
		switch failed.stage {
		case stageConfiguration, stageArguments, stageDSNRead, stageDatabaseOpen, stageDatabaseConnect, stageDialect, stageMigration, stageStatus:
			stage = string(failed.stage)
		}
		class = failureClass(failed.cause)
		var wait *connectionWaitFailure
		if errors.As(failed.cause, &wait) {
			class = failureClass(wait.cause)
			outcome := "deadline"
			if errors.Is(wait.budget, context.Canceled) {
				outcome = "canceled"
			}
			fmt.Fprintf(output, "%s stage=%s error_class=%s wait_status=%s\n", migrationFailureMessage, stage, class, outcome)
			return
		}
		if failed.stage == stageDSNRead {
			class = "secure_file"
		}
		if failed.stage == stageConfiguration || failed.stage == stageArguments || failed.stage == stageDialect {
			class = "configuration"
		}
	}
	fmt.Fprintf(output, "%s stage=%s error_class=%s\n", migrationFailureMessage, stage, class)
}

// failureClass prioritizes cancellation, database, TLS, file and config classes before network fallback, using only typed inspection.
// Error text, arbitrary SQLSTATE, SQL/DSN and file paths never become diagnostic values.
// failureClass ставит классы отмены, БД, TLS, файлов и config перед сетевым fallback, используя лишь типизированную проверку.
// Текст Error, произвольный SQLSTATE, SQL/DSN и пути не становятся диагностическими значениями.
func failureClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var database *pgconn.PgError
	if errors.As(err, &database) {
		return errorclass.Postgres(database.Code)
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
	return networkFailureClass(err)
}
