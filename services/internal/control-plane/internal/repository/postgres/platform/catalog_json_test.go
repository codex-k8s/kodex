package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// catalogJSONRow supplies synthetic row data, including a deferred scan failure
// after destinations were populated, without opening a database connection.
// catalogJSONRow задаёт синтетическую строку, включая отложенную ошибку Scan
// после заполнения приёмников, без подключения к базе данных.
type catalogJSONRow struct {
	raw   string
	total int64
	err   error
}

// Scan fills the two catalog destinations before returning the configured error,
// allowing tests to detect accidental exposure of partial data on failure.
// Scan заполняет два приёмника каталога перед возвратом заданной ошибки,
// позволяя тестам обнаружить случайную выдачу частичных данных при отказе.
func (row catalogJSONRow) Scan(destinations ...any) error {
	*destinations[0].(*[]byte) = []byte(row.raw)
	*destinations[1].(*int64) = row.total
	return row.err
}

// TestScanJSONCatalogPreservesTypedPages checks memory and skill payloads through
// the same scanner, retaining database totals and the original nil/empty distinction.
// TestScanJSONCatalogPreservesTypedPages проверяет страницы памяти и навыков
// через общий scanner с сохранением итогового count и различия nil/пустого среза.
func TestScanJSONCatalogPreservesTypedPages(t *testing.T) {
	memory, total, err := scanJSONCatalog[entity.KodexMemoryRecord](catalogJSONRow{raw: `[{"ref":"mem_test"}]`, total: 19})
	if err != nil || total != 19 || len(memory) != 1 || memory[0].Ref != "mem_test" {
		t.Fatalf("memory page decoding failed: items=%#v total=%d err=%v", memory, total, err)
	}
	skills, total, err := scanJSONCatalog[entity.SkillBundleRevision](catalogJSONRow{raw: `[{"revision":7}]`, total: 3})
	if err != nil || total != 3 || len(skills) != 1 || skills[0].Revision != 7 {
		t.Fatalf("skill revision decoding failed: items=%#v total=%d err=%v", skills, total, err)
	}
	for _, raw := range []string{"[]", "null"} {
		items, total, err := scanJSONCatalog[entity.SkillBundle](catalogJSONRow{raw: raw})
		if err != nil || total != 0 || len(items) != 0 || (items == nil) != (raw == "null") {
			t.Fatalf("empty page semantics changed for %s", raw)
		}
	}
}

// TestScanJSONCatalogDiscardsFailedRows proves that driver errors and malformed
// arrays yield one safe domain error without partially decoded items or totals.
// TestScanJSONCatalogDiscardsFailedRows доказывает, что ошибки драйвера и неверные
// массивы дают одну безопасную доменную ошибку без частичных элементов и count.
func TestScanJSONCatalogDiscardsFailedRows(t *testing.T) {
	for _, row := range []catalogJSONRow{
		{raw: `[{"ref":"mem_test"}]`, total: 19, err: errors.New("synthetic scan detail")},
		{raw: `[{"ref":"mem_test"},`, total: 19},
		{raw: `[{"ref":"mem_test"},{"ref":7}]`, total: 19},
		{raw: `{}`, total: 19},
	} {
		items, total, err := scanJSONCatalog[entity.KodexMemoryRecord](row)
		if err != errs.ErrUnavailable || items != nil || total != 0 {
			t.Fatalf("failed catalog row exposed partial result: items=%#v total=%d err=%v", items, total, err)
		}
	}
}
