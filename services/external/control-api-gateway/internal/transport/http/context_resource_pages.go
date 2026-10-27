package httptransport

import controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"

// contextPageWireItem restricts page collection to the four generated skill/memory
// resource/revision pointers whose nil-safe getters expose the same Ref copied by
// the unchanged strict view converters. It is not an authorization or storage port.
// contextPageWireItem ограничивает сбор страницы четырьмя generated указателями ресурсов
// и ревизий skill/memory; их nil-safe getters дают тот же Ref, который копируют прежние
// строгие view converters. Это не порт авторизации или хранилища.
type contextPageWireItem interface {
	*controlplanev1.SkillBundle | *controlplanev1.SkillBundleRevision |
		*controlplanev1.KodexMemoryRecord | *controlplanev1.MemoryRecordRevision

	// GetRef is read only after successful conversion and supplies the canonical duplicate key.
	// GetRef читается лишь после успешного преобразования и даёт канонический ключ уникальности.
	GetRef() string
}

// collectContextPageViews appends strict views in source order into the caller's initial
// fresh empty slice, rejecting invalid items, duplicate refs and a failing optional eligibility check.
// Failure preserves the already converted prefix with false; callers must reject the whole
// page before writing it. The caller owns envelope limits, RPC, project filter and output
// schema; conversion and duplicate checks precede that filter. Only generated pure getters
// are read additionally. No query, permission decision, logging or external I/O is performed.
// collectContextPageViews добавляет строгие views в исходном порядке в свежий пустой срез caller,
// отклоняя неверный элемент, повтор ref либо отказ необязательного фильтра допустимости.
// При отказе сохраняется уже преобразованный префикс с false; caller обязан отвергнуть всю
// страницу до её выдачи. Пределы страницы, RPC, project filter и формат ответа остаются у
// caller; преобразование и проверка повторов идут до фильтра. Дополнительно читаются только
// чистые generated getters. Запросов, решения о полномочиях, логов или внешнего I/O нет.
func collectContextPageViews[Input contextPageWireItem, Output any](items []Output, inputs []Input, convert func(Input) (Output, bool), eligible func(Input) bool) ([]Output, bool) {
	seen := make(map[string]bool)
	for _, input := range inputs {
		item, ok := convert(input)
		if !ok {
			return items, false
		}
		ref := input.GetRef()
		if seen[ref] || eligible != nil && !eligible(input) {
			return items, false
		}
		seen[ref] = true
		items = append(items, item)
	}
	return items, true
}
