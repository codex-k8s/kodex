package main

import (
	"testing"

	"github.com/pressly/goose/v3"
)

// TestManagedKubernetesOwnerRegressionSeed creates only a disposable schema047 database for
// existing TOKEN owner regressions. It neither registers a release profile nor qualifies the
// complete Kubernetes migration/backup contract, which requires independent full-catalog proofs.
// TestManagedKubernetesOwnerRegressionSeed создаёт только disposable schema047 БД для прежних
// TOKEN owner regressions. Не регистрирует release profile и не квалифицирует полный Kubernetes
// migration/backup contract: для него нужны независимые full-catalog proofs.
func TestManagedKubernetesOwnerRegressionSeed(t *testing.T) {
	ctx, database, _ := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261114140000); err != nil {
		t.Fatal("prepare exact managed Kubernetes predecessor", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114150000); err != nil {
		t.Fatal("prepare disposable managed Kubernetes owner schema", err)
	}
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil || version != 20261114150000 {
		t.Fatal("managed Kubernetes schema did not commit its Goose metadata")
	}
}
