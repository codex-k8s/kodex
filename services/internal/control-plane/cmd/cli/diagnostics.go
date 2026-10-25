package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/postgresdiagnostic"
)

// cliStage restricts diagnostic phases to CLI-owned constants, never filenames or command input.
// cliStage ограничивает диагностические этапы константами CLI, исключая имена файлов и ввод команды.
type cliStage string

const (
	// stageArguments identifies rejected commands before reading connection credentials.
	// stageArguments обозначает отказ команды до чтения реквизитов соединения.
	stageArguments cliStage = "arguments"
	// stageConfiguration identifies missing or empty configured DSN input.
	// stageConfiguration обозначает отсутствующий либо пустой настроенный DSN.
	stageConfiguration cliStage = "configuration"
	// stageDSNRead identifies file access failure without revealing the credential path.
	// stageDSNRead обозначает отказ чтения без раскрытия пути реквизитов.
	stageDSNRead cliStage = "dsn_read"
	// stageDSNParse identifies pgx configuration parsing failure without exposing DSN bytes.
	// stageDSNParse обозначает отказ разбора pgx без раскрытия байтов DSN.
	stageDSNParse cliStage = "dsn_parse"
	// stageDatabaseConnect identifies the single bounded initial PingContext failure.
	// stageDatabaseConnect обозначает отказ единственного ограниченного начального PingContext.
	stageDatabaseConnect cliStage = "database_connect"
	// stageDialect identifies local Goose dialect configuration failure.
	// stageDialect обозначает локальный отказ настройки диалекта Goose.
	stageDialect cliStage = "dialect"
	// stageMigration identifies an up/status failure after connectivity, without retry authority.
	// stageMigration обозначает отказ up/status после подключения без полномочия повтора.
	stageMigration cliStage = "migration"
	// stageOutput identifies a failed success-output write without echoing its raw cause.
	// stageOutput обозначает отказ записи успешного вывода без повторения сырой причины.
	stageOutput cliStage = "output"
	// cliFailureMessage supplies the redacted public failure prefix for every CLI path.
	// cliFailureMessage задаёт закрытый публичный префикс отказа для всех путей CLI.
	cliFailureMessage = "control-plane CLI failed"
)

// commandFailure retains typed causes privately while accidental Error formatting stays redacted.
// commandFailure сохраняет типизированную причину приватно, закрывая её при случайном форматировании Error.
type commandFailure struct {
	stage cliStage
	cause error
}

// Error never interpolates upstream messages, DSN, queries or connection metadata.
// Error никогда не подставляет внешние сообщения, DSN, запросы или метаданные соединения.
func (*commandFailure) Error() string { return cliFailureMessage }

// Unwrap preserves internal errors.Is/As inspection without authorizing error-chain output.
// Unwrap сохраняет внутреннюю проверку errors.Is/As без разрешения вывода цепочки ошибок.
func (failure *commandFailure) Unwrap() error { return failure.cause }

// cliFailure records a rejected stage, including a configuration rejection without an upstream cause.
// cliFailure фиксирует отказ этапа, включая отказ конфигурации без внешней причины.
func cliFailure(stage cliStage, cause error) error {
	return &commandFailure{stage: stage, cause: cause}
}

// reportCLIFailure writes only an allowlisted stage and closed typed class, never Error().
// Unknown wrappers and private-looking stage values collapse to fixed fallbacks at this process boundary.
// reportCLIFailure записывает только разрешённый этап и закрытый типизированный класс, никогда Error().
// Неизвестные wrappers и похожие на приватные данные этапы сводятся к fallback на границе процесса.
func reportCLIFailure(output io.Writer, err error) {
	stage := "unknown"
	class := postgresFailureClass(err)
	var failure *commandFailure
	if errors.As(err, &failure) {
		switch failure.stage {
		case stageArguments, stageConfiguration, stageDSNRead, stageDSNParse, stageDatabaseConnect, stageDialect, stageMigration, stageOutput:
			stage = string(failure.stage)
		}
		switch failure.stage {
		case stageArguments, stageConfiguration, stageDialect:
			class = "configuration"
		case stageDSNRead:
			class = "filesystem"
		}
	}
	_, _ = fmt.Fprintf(output, "%s stage=%s error_class=%s\n", cliFailureMessage, stage, class)
}

// postgresFailureClass delegates to the service-owned classifier, preserving CLI output and precedence.
// postgresFailureClass передаёт проверку классификатору сервиса, сохраняя вывод CLI и приоритет причин.
func postgresFailureClass(err error) string { return postgresdiagnostic.Class(err) }
