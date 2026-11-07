package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// assertControlPlaneCatalogMutationRejected applies one synthetic DDL change in the caller's
// disposable database and evaluates the shipped guard body inside that same transaction. Only
// its outer BEGIN/ROLLBACK frame is removed: a nested transaction-options error must not stand in
// for real catalog rejection. The exact guard SQLSTATE/message is required before fixture rollback.
// assertControlPlaneCatalogMutationRejected вносит одно synthetic DDL изменение в disposable DB
// caller и проверяет поставляемый guard внутри той же транзакции. Удаляется лишь внешний
// BEGIN/ROLLBACK: ошибка параметров вложенной транзакции не заменяет настоящий отказ catalog.
// До rollback оснастки требуются точные SQLSTATE/message самого guard.
func assertControlPlaneCatalogMutationRejected(t *testing.T, ctx context.Context, observer *pgx.Conn, proof []byte, mutation, expectedMessage string) {
	t.Helper()
	const begin = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;"
	source := strings.TrimSpace(string(proof))
	if strings.Count(source, begin) != 1 || !strings.HasSuffix(source, "ROLLBACK;") || expectedMessage == "" {
		t.Fatal("catalog counterfactual requires the exact shipped transaction frame")
	}
	body := strings.TrimSuffix(strings.Replace(source, begin, "-- Counterfactual uses its existing fixture transaction.", 1), "ROLLBACK;")
	if _, err := observer.Exec(ctx, "BEGIN ISOLATION LEVEL REPEATABLE READ; "+mutation); err != nil {
		t.Fatal("prepare disposable catalog counterfactual", err)
	}
	results, readErr := observer.PgConn().Exec(ctx, body).ReadAll()
	rejected := false
	var cause *pgconn.PgError
	if errors.As(readErr, &cause) && cause.Code == "P0001" && cause.Message == expectedMessage {
		rejected = true
	}
	for _, result := range results {
		if errors.As(result.Err, &cause) && cause.Code == "P0001" && cause.Message == expectedMessage {
			rejected = true
		}
	}
	if _, err := observer.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal("rollback disposable catalog counterfactual", err)
	}
	if !rejected {
		code := "NO_DATABASE_ERROR"
		if cause != nil {
			code = cause.Code
		}
		t.Fatalf("catalog counterfactual did not reach the expected guard; SQLSTATE=%s", code)
	}
}
