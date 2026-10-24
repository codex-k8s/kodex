package platform

import (
	"encoding/json"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// scanJSONCatalog decodes a catalog query's JSON array and total count from one
// row. Scan and decode failures return only ErrUnavailable and no partial data;
// SQL selection, authorization, pagination and transaction ownership stay outside.
// scanJSONCatalog декодирует JSON-массив и общее число элементов из одной строки
// запроса каталога. Ошибки Scan и декодирования дают только ErrUnavailable без
// частичных данных; SQL, авторизация, пагинация и транзакция остаются снаружи.
func scanJSONCatalog[T any](row pgx.Row) ([]T, int64, error) {
	var raw []byte
	var total int64
	if err := row.Scan(&raw, &total); err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	var items []T
	if json.Unmarshal(raw, &items) != nil {
		return nil, 0, errs.ErrUnavailable
	}
	return items, total, nil
}
