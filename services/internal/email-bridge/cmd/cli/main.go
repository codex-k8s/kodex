package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"os"
	"time"
)

// migrations embeds the existing forward-only SQL set; the CLI reads these files without rewriting historical migrations.
// migrations встраивает существующий forward-only набор SQL; CLI читает файлы, не переписывая исторические миграции.
//
//go:embed migrations/*.sql
var migrations embed.FS

// main runs the migration command and emits only the CLI's bounded failure report before returning a nonzero process exit.
// main исполняет команду миграции и при ошибке выводит только ограниченный отчёт CLI перед ненулевым завершением процесса.
func main() {
	if err := run(); err != nil {
		reportFailure(os.Stderr, err)
		os.Exit(1)
	}
}

// run accepts only up/status, reads the designated protected DSN and waits for database connectivity within one minute.
// After a successful ping it invokes Goose once with the remaining context; status may initialize Goose's version table and is not a guaranteed read-only query.
// run допускает только up/status, читает назначенный защищённый DSN и ожидает связь с БД в пределах одной минуты.
// После успешного ping Goose вызывается один раз с остатком контекста; status может создать таблицу версий Goose и не гарантирует read-only чтение.
func run() error {
	var cfg struct {
		DSNFile string `env:"EMAIL_BRIDGE_MIGRATION_DSN_FILE,required,notEmpty"`
	}
	if e := env.ParseWithOptions(&cfg, env.Options{}); e != nil {
		return failure(stageConfiguration, e)
	}
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "status") {
		return failure(stageArguments, fmt.Errorf("unsupported migration command"))
	}
	raw, e := securefile.Read(cfg.DSNFile, 16384)
	if e != nil {
		return failure(stageDSNRead, e)
	}
	db, e := sql.Open("pgx", string(raw))
	if e != nil {
		return failure(stageDatabaseOpen, e)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return withReadyDatabase(ctx, db.PingContext, func(ready context.Context) error {
		goose.SetBaseFS(migrations)
		if e = goose.SetDialect("postgres"); e != nil {
			return failure(stageDialect, e)
		}
		if os.Args[1] == "up" {
			return failure(stageMigration, goose.UpContext(ready, db, "migrations"))
		}
		return failure(stageStatus, goose.StatusContext(ready, db, "migrations"))
	}, databaseRetryDelay)
}
