package admin_test

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
)

func isolatedPostgresTestDSN(t *testing.T, label string) string {
	t.Helper()
	return testsupport.IsolatedSchemaDSN(t, label)
}
