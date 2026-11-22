package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"

	"github.com/pressly/goose/v3"
)

const (
	// artifactRetentionIssuerBefore is the only already installed authority schema that this command upgrades.
	// artifactRetentionIssuerBefore — единственная ранее установленная схема authority для этой команды обновления.
	artifactRetentionIssuerBefore = 20261115000100
	// artifactRetentionIssuerAfter contains the single typed issuer LOGIN and its workload binding.
	// artifactRetentionIssuerAfter содержит отдельный LOGIN издателя и его привязку к workload.
	artifactRetentionIssuerAfter = 20261115000200
)

// artifactRetentionIssuerReadback tests the new LOGIN, exact PostgreSQL role membership and RLS owner binding without reading credential values.
// artifactRetentionIssuerReadback проверяет новый LOGIN, точное членство в роли PostgreSQL и привязку владельца RLS без чтения реквизитов.
//
//go:embed sql/artifact_retention_issuer_readback.sql
var artifactRetentionIssuerReadback string

// artifactRetentionIssuerHead reads the committed migration head and whether the exact role already exists.
// artifactRetentionIssuerHead читает зафиксированную версию миграций и наличие точной роли.
//
//go:embed sql/artifact_retention_issuer_head.sql
var artifactRetentionIssuerHead string

// artifactRetentionReadbackRole enters the existing readback owner only for the read-only transaction.
// artifactRetentionReadbackRole применяет существующую роль владельца readback только внутри read-only транзакции.
//
//go:embed sql/artifact_retention_readback_role.sql
var artifactRetentionReadbackRole string

// upArtifactRetentionIssuer performs only the issuer migration and checks the existing result without replay at the target schema.
// upArtifactRetentionIssuer выполняет только миграцию издателя и проверяет уже зафиксированный результат на целевой схеме без повторного применения.
func upArtifactRetentionIssuer(ctx context.Context, database *sql.DB) error {
	var head int64
	var principalExists bool
	if err := database.QueryRowContext(ctx, artifactRetentionIssuerHead).Scan(&head, &principalExists); err != nil {
		return fmt.Errorf("read artifact-retention issuer predecessor: %w", err)
	}
	switch head {
	case artifactRetentionIssuerBefore:
		if principalExists {
			return errors.New("artifact-retention issuer role exists before its migration")
		}
		if err := goose.UpToContext(ctx, database, "migrations", artifactRetentionIssuerAfter); err != nil {
			return fmt.Errorf("apply artifact-retention issuer migration: %w", err)
		}
	case artifactRetentionIssuerAfter:
		if !principalExists {
			return errors.New("artifact-retention issuer role is missing after its migration")
		}
	default:
		return errors.New("artifact-retention issuer predecessor is not supported")
	}
	return verifyArtifactRetentionIssuer(ctx, database)
}

// verifyArtifactRetentionIssuer checks the committed authority head and the role under the existing readback ownership in one read-only transaction.
// verifyArtifactRetentionIssuer проверяет зафиксированную схему и роль одним read-only переходом от существующего владельца readback.
func verifyArtifactRetentionIssuer(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var head int64
	var principalExists bool
	if err := tx.QueryRowContext(ctx, artifactRetentionIssuerHead).Scan(&head, &principalExists); err != nil {
		return err
	}
	if head != artifactRetentionIssuerAfter || !principalExists {
		return errors.New("artifact-retention issuer migration readback has an unexpected schema")
	}
	if _, err := tx.ExecContext(ctx, artifactRetentionReadbackRole); err != nil {
		return err
	}
	var valid bool
	if err := tx.QueryRowContext(ctx, artifactRetentionIssuerReadback).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errors.New("artifact-retention issuer database role readback rejected")
	}
	return tx.Commit()
}
