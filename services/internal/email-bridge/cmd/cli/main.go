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

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	if err := run(); err != nil {
		reportFailure(os.Stderr, err)
		os.Exit(1)
	}
}
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
