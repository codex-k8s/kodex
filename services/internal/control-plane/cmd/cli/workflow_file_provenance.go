package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"

	"github.com/pressly/goose/v3"
)

const (
	// workflowFileProvenanceCommand selects the stopped, backup-compatible ART 050 cohort.
	// workflowFileProvenanceCommand выбирает остановленный набор компонентов ART 050 с резервным чтением.
	workflowFileProvenanceCommand = "up-workflow-file-provenance"

	// workflowFileProvenanceVersion bounds the command even when the binary includes later migrations.
	// workflowFileProvenanceVersion ограничивает команду, даже если бинарник содержит более поздние миграции.
	workflowFileProvenanceVersion int64 = 20261121000200

	// workflowFileProvenanceReceipt acknowledges only an observed schema 051 with intact backup access.
	// workflowFileProvenanceReceipt подтверждает только наблюдаемую схему 051 с сохранённым резервным доступом.
	workflowFileProvenanceReceipt = "Workflow file provenance migration completed at version 20261121000200"

	// workflowFileProvenancePreviousBody identifies the function installed in schema 050.
	// workflowFileProvenancePreviousBody определяет функцию, установленную в схеме 050.
	workflowFileProvenancePreviousBody = "1fa42c481cd3935099281855abf46c96"

	// workflowFileProvenanceCurrentBody identifies the intended replacement body in schema 051.
	// workflowFileProvenanceCurrentBody определяет ожидаемое тело функции в схеме 051.
	workflowFileProvenanceCurrentBody = "a016906fbdbbf7aae09f0bc2f28dbe6f"
)

// workflowFileProvenanceAuthoritySQL checks the exact predicate and relevant backup permissions
// without evaluating the function or reading any workspace data.
// workflowFileProvenanceAuthoritySQL проверяет точный предикат и применимые права резервного чтения
// без выполнения функции и чтения данных рабочей области.
//
//go:embed workflow_file_provenance_sql/workflow_file_provenance__authority.sql
var workflowFileProvenanceAuthoritySQL string

// requireWorkflowFileProvenanceAuthority checks the relevant database owners, code and backup role
// before execution and before issuing a success receipt.
// requireWorkflowFileProvenanceAuthority проверяет владельцев БД, код и роль резервного чтения
// перед выполнением и перед выдачей подтверждения.
func requireWorkflowFileProvenanceAuthority(ctx context.Context, database *sql.DB, version int64, body string) error {
	var allowed bool
	if err := database.QueryRowContext(ctx, workflowFileProvenanceAuthoritySQL, body, version).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return errors.New("workflow file provenance authority rejected")
	}
	return nil
}

// upgradeWorkflowFileProvenance applies the single registered forward step. On rejoin it only
// checks the committed head and authority. A database error stops the command without retry.
// upgradeWorkflowFileProvenance выполняет единственный зарегистрированный переход вперёд.
// При повторном входе проверяет зафиксированную версию и права. Ошибка БД завершает команду без повтора.
func upgradeWorkflowFileProvenance(ctx context.Context, database *sql.DB) error {
	version, err := readControlPlaneSchemaHead(ctx, database)
	if err != nil {
		return err
	}
	switch version {
	case workflowFileProvenanceVersion:
		return requireWorkflowFileProvenanceAuthority(ctx, database, version, workflowFileProvenanceCurrentBody)
	case artifactRetentionGrantVersion:
		if err := requireWorkflowFileProvenanceAuthority(ctx, database, version, workflowFileProvenancePreviousBody); err != nil {
			return err
		}
	default:
		return errors.New("workflow file provenance schema head rejected")
	}
	if err := goose.UpToContext(ctx, database, "migrations", workflowFileProvenanceVersion); err != nil {
		return err
	}
	version, err = readControlPlaneSchemaHead(ctx, database)
	if err != nil {
		return err
	}
	if version != workflowFileProvenanceVersion {
		return errors.New("workflow file provenance schema successor rejected")
	}
	return requireWorkflowFileProvenanceAuthority(ctx, database, version, workflowFileProvenanceCurrentBody)
}
