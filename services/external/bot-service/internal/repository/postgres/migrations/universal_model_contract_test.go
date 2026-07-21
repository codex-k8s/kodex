package migrations_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUniversalModelRunbookMatchesStablePreflightCodes(t *testing.T) {
	repositoryRoot := universalModelRepositoryRoot(t)
	migration := readUniversalModelContractFile(t, filepath.Join(
		repositoryRoot,
		"services/external/bot-service/internal/repository/postgres/migrations/000030_universal_model.sql",
	))
	runbook := readUniversalModelContractFile(t, filepath.Join(repositoryRoot, "docs/runbooks/universal-model-expand.md"))

	for _, code := range []string{
		"MCV30_DUPLICATE_MATTERMOST_TEAM_BINDINGS",
		"MCV30_DUPLICATE_MATTERMOST_CHANNEL_BINDINGS",
		"MCV30_CROSS_PROJECT_CHAT_PARTICIPANT",
		"MCV30_CROSS_PROJECT_BOT_IDENTITY",
		"MCV30_INSTRUCTION_MARKDOWN_TOO_LARGE",
	} {
		if strings.Count(migration, code) != 1 {
			t.Fatalf("код %s должен быть объявлен в migration ровно один раз", code)
		}
		if !strings.Contains(runbook, "`"+code+"`") {
			t.Fatalf("runbook не содержит стабильный код %s", code)
		}
	}
	if strings.Contains(runbook, "MCV30_DUPLICATE_EXTERNAL_BINDINGS") {
		t.Fatal("runbook содержит несуществующий обобщённый код preflight")
	}
}

func universalModelRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() не вернул путь теста")
	}
	current := filepath.Dir(currentFile)
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("корень репозитория с go.mod не найден")
		}
		current = parent
	}
}

func readUniversalModelContractFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение обязательного контракта %s: %v", filepath.Base(path), err)
	}
	return string(body)
}
