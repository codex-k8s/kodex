// Package errorclass provides bounded technical diagnostic labels without importing telemetry SDKs or database drivers.
// Пакет errorclass задаёт ограниченные технические диагностические метки без импорта telemetry SDK или драйверов БД.
package errorclass

// Postgres maps known SQLSTATE values to the shared closed diagnostic classes and normalizes every other value to database.
// Callers establish the typed database-error boundary and classification precedence; this function never interprets error text or decides retries.
// Postgres переводит известные SQLSTATE в общие закрытые диагностические классы, нормализуя все прочие значения в database.
// Вызывающий устанавливает границу типизированной ошибки БД и порядок классификации; функция не разбирает текст ошибки и не решает вопрос повторов.
func Postgres(code string) string {
	switch code {
	case "28P01", "28000":
		return "database_authentication"
	case "42501":
		return "database_permission"
	case "3D000":
		return "database_missing"
	case "53300", "57P03":
		return "database_unavailable"
	default:
		return "database"
	}
}
