package integration

import (
	"encoding/json"
	"strconv"
)

// catalogPageProjection serializes caller-selected items after provider-specific page checks into the unchanged string-valued items/count/optional-next envelope.
// It preserves the caller's nil-versus-empty slice and binds the receipt reference to operation:page through providerResult.
// Scope, page limits, cursor checks and nil normalization remain with each provider; serialization alone is not authorization or completeness proof.
// catalogPageProjection сериализует выбранные вызывающим элементы после специфичных для провайдера проверок страницы в прежнюю оболочку со строковым items, count и необязательным next.
// Сохраняется различие nil и пустого среза вызывающего, а ссылка квитанции связывается с operation:page через providerResult.
// Область, лимиты страницы, курсоры и нормализация nil остаются у каждого провайдера; сериализация не доказывает полномочия или полноту.
func catalogPageProjection[T any](request Request, page int, items []T, next int) (Result, error) {
	body, err := json.Marshal(items)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, request.Operation+":"+strconv.Itoa(page), struct {
		Items string `json:"items"`
		Count int    `json:"count"`
		Next  int    `json:"next_cursor,omitempty"`
	}{string(body), len(items), next})
}
