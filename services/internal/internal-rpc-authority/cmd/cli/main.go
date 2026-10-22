package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// migrations embeds the CLI's SQL migration history at build time for Goose. The CLI selects this filesystem explicitly; declaring it neither applies a migration nor permits editing already published migration bytes.
// migrations встраивает историю SQL-миграций CLI при сборке для Goose. CLI явно выбирает эту файловую систему; объявление не применяет миграции и не разрешает менять уже опубликованные байты.
//
//go:embed migrations/*.sql
var migrations embed.FS

// command is the CLI's closed action selector for migrations, status, bounded freshness, rotation and explicit provenance repair. A parsed action alone supplies no database authority or confirmation of its detailed arguments.
// command — закрытый селектор действий CLI для миграций, статуса, ограниченной свежести, ротации и явного восстановления происхождения. Разобранное действие само не даёт полномочий БД и не подтверждает подробные аргументы.
type command string

const (
	// commandUp selects forward Goose migration application; rollback and historical migration rewriting are not exposed through this selector.
	// commandUp выбирает применение миграций Goose вперёд; откат и переписывание истории через этот селектор не предоставляются.
	commandUp command = "up"
	// commandStatus selects Goose migration-status output without applying migration steps through the status branch.
	// commandStatus выбирает вывод статуса миграций Goose без применения шагов миграции в ветви статуса.
	commandStatus command = "status"
	// commandFreshnessStatus selects one validated freshness-policy readback rather than activation or continuous observation.
	// commandFreshnessStatus выбирает одно проверенное чтение политики свежести, а не активацию или непрерывное наблюдение.
	commandFreshnessStatus command = "freshness-status"
	// commandFreshnessWatch selects the bounded freshness observation loop; the action does not itself activate a new policy version.
	// commandFreshnessWatch выбирает ограниченный цикл наблюдения свежести; само действие не активирует новую версию политики.
	commandFreshnessWatch command = "freshness-watch"
	// commandFreshnessActivate selects the explicit version-one-to-two freshness activation path, whose exact flags, confirmation and canonical nonzero UUID are checked separately.
	// commandFreshnessActivate выбирает явный путь активации свежести с первой версии на вторую; точные флаги, подтверждение и канонический ненулевой UUID проверяются отдельно.
	commandFreshnessActivate command = "freshness-activate"
	// commandRotationStatus selects rotation-state readback, separately from watching one operation or attempting an explicit abort.
	// commandRotationStatus выбирает чтение состояния ротации отдельно от наблюдения одной операции или попытки явной отмены.
	commandRotationStatus command = "rotation-status"
	// commandRotationWatch selects observation of one explicitly supplied rotation operation; operation identity is parsed by the rotation-specific options path.
	// commandRotationWatch выбирает наблюдение одной явно переданной операции ротации; её идентичность разбирается отдельным путём параметров ротации.
	commandRotationWatch command = "rotation-watch"
	// commandRotationAbort selects the explicit rotation-abort path; command recognition does not replace validation of intent, source revision, digest and confirmation arguments.
	// commandRotationAbort выбирает явный путь отмены ротации; распознавание команды не заменяет проверку аргументов намерения, исходной ревизии, дайджеста и подтверждения.
	commandRotationAbort command = "rotation-abort"
	// commandLegacyRepair selects the explicitly requested provenance-repair path after forward migrations, using a separately read proof rather than an automatic startup repair.
	// commandLegacyRepair выбирает явно запрошенный путь восстановления происхождения после прямых миграций с отдельно прочитанным доказательством, а не автоматическое исправление при запуске.
	commandLegacyRepair command = "legacy-provenance-repair"
)

// main creates the signal-cancelled root context, runs the CLI with process arguments, and prints a fixed failure prefix plus the returned error before exiting nonzero. It is the process entry point, not a reusable migration or authorization helper.
// main создаёт корневой контекст с отменой по сигналу, запускает CLI с аргументами процесса и при ошибке выводит фиксированный префикс и возвращённую ошибку перед ненулевым выходом. Это точка входа процесса, а не переиспользуемый помощник миграции или авторизации.
func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "internal-rpc-authority CLI failed: %v\n", err)
		os.Exit(1)
	}
}

// run parses the action and specialized options before loading the DSN file, clears its byte buffer, enforces exact verify-full PostgreSQL TLS without fallbacks, pings the database and configures embedded Goose migrations. It then dispatches the exact status, activation, rotation, repair or forward-migration path and closes the database on return; this function is not safe to call as a unit probe against live state.
// run разбирает действие и специальные параметры до загрузки файла DSN, очищает его байтовый буфер, требует точный PostgreSQL TLS verify-full без запасных путей, проверяет подключение и настраивает встроенные миграции Goose. Затем выбирает точный путь статуса, активации, ротации, восстановления или прямой миграции и закрывает БД при возврате; функцию нельзя вызывать как юнит-пробу на живых данных.
func run(ctx context.Context, arguments []string) error {
	action, err := parseCommand(arguments)
	if err != nil {
		return err
	}
	options, err := parseFreshnessOptions(action, arguments)
	if err != nil {
		return err
	}
	rotationOptions, err := parseRotationOptions(action, arguments)
	if err != nil {
		return err
	}
	environment := struct {
		DSNFile       string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE,required,notEmpty"`
		TLSServerName string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME,required,notEmpty"`
	}{}
	if err := env.Parse(&environment); err != nil {
		return errors.New("parse migration environment configuration")
	}
	raw, err := securefile.Read(environment.DSNFile, 16<<10)
	if err != nil {
		return fmt.Errorf("read PostgreSQL DSN file: %w", err)
	}
	defer clear(raw)
	config, err := pgx.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return errors.New("parse PostgreSQL DSN")
	}
	expectedServerName := environment.TLSServerName
	if expectedServerName == "" ||
		len(config.Fallbacks) != 0 ||
		config.Host != expectedServerName ||
		config.TLSConfig == nil ||
		config.TLSConfig.RootCAs == nil ||
		config.TLSConfig.ServerName != expectedServerName ||
		config.TLSConfig.InsecureSkipVerify {
		return errors.New("PostgreSQL DSN must use verify-full TLS with exact server name")
	}
	database := stdlib.OpenDB(*config)
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return errors.New("verify PostgreSQL connectivity")
	}
	goose.SetBaseFS(migrations)
	goose.SetTableName("public.goose_db_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return errors.New("configure PostgreSQL migration dialect")
	}
	switch action {
	case commandFreshnessStatus, commandFreshnessWatch, commandFreshnessActivate:
		return runFreshness(ctx, database, action, options, os.Stdout)
	case commandRotationStatus, commandRotationWatch, commandRotationAbort:
		return runRotation(ctx, database, action, rotationOptions, os.Stdout)
	case commandLegacyRepair:
		proof, err := readLegacyRepairProof(arguments)
		if err != nil {
			return err
		}
		if err := goose.UpContext(ctx, database, "migrations"); err != nil {
			return errors.New("apply legacy provenance migration failed")
		}
		return repairLegacyProvenance(ctx, database, proof, os.Stdout)
	case commandUp:
		if err := goose.UpContext(ctx, database, "migrations"); err != nil {
			return fmt.Errorf("apply internal-rpc-authority migrations: %w", err)
		}
		return nil
	case commandStatus:
		return migrationStatus(ctx, database)
	default:
		return errors.New("unsupported CLI command")
	}
}

// parseCommand recognizes the closed action set with fixed argument counts for repair, activation, abort and operation watch. It does not validate those subcommands' flag values; specialized parsers do that, and unsupported or extra arguments return the fixed usage error.
// parseCommand распознаёт закрытый набор действий с фиксированным числом аргументов восстановления, активации, отмены и наблюдения операции. Значения флагов этих подкоманд проверяют специальные разборщики; неподдерживаемые или лишние аргументы возвращают фиксированную ошибку использования.
func parseCommand(arguments []string) (command, error) {
	if len(arguments) == 5 && arguments[0] == string(commandLegacyRepair) {
		return commandLegacyRepair, nil
	}

	if len(arguments) == 7 && arguments[0] == string(commandFreshnessActivate) {
		return commandFreshnessActivate, nil
	}
	if len(arguments) == 9 && arguments[0] == string(commandRotationAbort) {
		return commandRotationAbort, nil
	}
	if len(arguments) == 3 && arguments[0] == string(commandRotationWatch) {
		return commandRotationWatch, nil
	}
	if len(arguments) != 1 {
		return "", errors.New(cliUsage)
	}
	switch command(arguments[0]) {
	case commandUp, commandStatus, commandFreshnessStatus, commandFreshnessWatch, commandRotationStatus:
		return command(arguments[0]), nil
	default:
		return "", errors.New(cliUsage)
	}
}

// cliUsage is the fixed English diagnostic for unsupported CLI command shapes, listing the supported operations and their explicit confirmation arguments without interpolating environment or credential values.
// cliUsage — фиксированная английская диагностика неподдерживаемой формы команды CLI с перечислением операций и явных аргументов подтверждения без подстановки окружения или реквизитов.
const cliUsage = "usage: internal-rpc-authority-cli <up|status|freshness-status|freshness-watch|freshness-activate --expected-version 1 --activation-id UUID --confirm ACTIVATE-STAGING-AUTHORITY-FRESHNESS|rotation-status|rotation-watch --operation-id UUID|rotation-abort --intent-id UUID --source-revision NUMBER --source-digest-sha256 SHA256 --confirm ABORT-STAGING-AUTHORITY-ROTATION>"

// migrationStatus asks Goose to report the embedded migration set's status through the supplied database and context, wrapping a failure without applying migrations itself.
// migrationStatus запрашивает у Goose статус встроенного набора миграций через переданные БД и контекст, оборачивая ошибку без самостоятельного применения миграций.
func migrationStatus(ctx context.Context, database *sql.DB) error {
	if err := goose.StatusContext(ctx, database, "migrations"); err != nil {
		return fmt.Errorf("read internal-rpc-authority migration status: %w", err)
	}
	return nil
}
