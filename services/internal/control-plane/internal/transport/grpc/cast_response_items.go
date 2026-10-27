package grpc

// castResponseItems appends one converted value per source item in iteration order to
// a fresh nil slice. Nil or empty input returns nil without calling convert. It is
// used only for fresh, unpublished response fields: pagination and other fields stay
// with the caller, as do principal checks, domain reads and error handling. Conversion
// receives the same value copy as a range loop; address-based projections stay separate.
// castResponseItems добавляет по одному преобразованному значению на исходный элемент
// в порядке обхода в новый nil-срез. Nil либо пустой вход возвращает nil без вызова
// convert. Применяется только к полям новых, ещё не выданных ответов: пагинация и прочие
// поля остаются у caller вместе с проверками principal, доменным чтением и ошибками.
// Преобразование получает ту же копию значения, что range; проекции по адресу отдельны.
func castResponseItems[Source, Target any](items []Source, convert func(Source) Target) []Target {
	var result []Target
	for _, item := range items {
		result = append(result, convert(item))
	}
	return result
}
