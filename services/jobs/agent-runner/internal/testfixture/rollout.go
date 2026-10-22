// Package testfixture provides inert filesystem preparation shared only by agent-runner unit tests, without constructing execution authority or starting providers.
// Пакет testfixture предоставляет неактивную файловую подготовку только unit-тестам agent-runner, не создавая полномочий исполнения и не запуская провайдеров.
package testfixture

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteRolloutSource creates the test-chosen parent path with mode 0700 and writes two synthetic JSONL records with create/write-only mode 0640, then checks write, sync and close. It deliberately preserves the original non-truncating flags and existing modes; callers must provide a disposable test path.
// WriteRolloutSource создаёт выбранный тестом parent path с режимом 0700 и пишет две синтетические JSONL-записи с create/write-only и режимом 0640, затем проверяет запись, sync и close. Намеренно сохраняет прежние флаги без truncation и существующие режимы; вызывающий обязан передать одноразовый тестовый путь.
func WriteRolloutSource(t testing.TB, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("{\"type\":\"session_meta\"}\n{\"type\":\"event_msg\"}\n")
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		t.Fatal("fixture source was not durably closed")
	}
}
