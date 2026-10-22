package publisher

import (
	"strings"
	"testing"
)

// TestPromoteSnapshotUsesExactRequiredTargets asserts the embedded promotion query supplies expected workload/role/generation arrays and rejects an old global-readback-count fragment, without running the promotion function.
// TestPromoteSnapshotUsesExactRequiredTargets требует массивы ожидаемых workload, ролей и поколений во встроенном запросе продвижения и отклоняет старый фрагмент глобального подсчёта readback без запуска функции продвижения.
func TestPromoteSnapshotUsesExactRequiredTargets(t *testing.T) {
	t.Parallel()

	for _, parameter := range []string{
		"@expected_workload_ids",
		"@expected_roles",
		"@expected_generations",
	} {
		if !strings.Contains(promoteSnapshotSQL, parameter) {
			t.Fatalf("publisher promote query omits %s", parameter)
		}
	}
	if strings.Contains(
		promoteSnapshotSQL,
		"authority_snapshot_readbacks\nWHERE source_revision",
	) {
		t.Fatal("publisher promote query still counts every snapshot readback")
	}
}

// TestLoadSnapshotHistoryIncludesCurrentRevisionForRestart pins the embedded LIMIT 33 spelling so the current revision can coexist with thirty-two predecessors; it does not restart a publisher or read live history.
// TestLoadSnapshotHistoryIncludesCurrentRevisionForRestart закрепляет форму LIMIT 33 во встроенном SQL для текущей ревизии и тридцати двух предшественников; publisher не перезапускается и живая история не читается.
func TestLoadSnapshotHistoryIncludesCurrentRevisionForRestart(t *testing.T) {
	t.Parallel()

	if !strings.Contains(loadSnapshotHistorySQL, "LIMIT 33") {
		t.Fatal("snapshot history query must retain current revision plus 32 predecessors")
	}
}

// TestLoadSnapshotPredecessorUsesPersistedCrossDomainProvenance checks the function name and exact source/registry parameters in embedded SQL; it does not establish actual cross-domain database provenance.
// TestLoadSnapshotPredecessorUsesPersistedCrossDomainProvenance проверяет имя функции и точные параметры источника и реестра во встроенном SQL; фактическое междоменное происхождение данных БД не устанавливается.
func TestLoadSnapshotPredecessorUsesPersistedCrossDomainProvenance(t *testing.T) {
	t.Parallel()
	for _, required := range []string{
		"publisher_load_snapshot_predecessor",
		"@source_revision",
		"@source_digest_sha256",
		"@registry_digest_sha256",
	} {
		if !strings.Contains(loadSnapshotPredecessorSQL, required) {
			t.Fatalf("snapshot predecessor query omits %s", required)
		}
	}
}
