package migrations

import (
	"testing"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
)

func TestGooseMigrationFiles(t *testing.T) {
	migrationtest.AssertGooseMigrationFiles(t, ".")
}
