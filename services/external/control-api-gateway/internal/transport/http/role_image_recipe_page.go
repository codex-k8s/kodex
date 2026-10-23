package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// roleImageRecipePageResponse restricts page projection to the two owner RPC reply pointers, preserving typed-nil checks without reflection. It describes wire shape only; callers retain their exact scope predicate and RPC authority.
// roleImageRecipePageResponse ограничивает проекцию страницы двумя указателями ответов RPC владельца, сохраняя проверку типизированного nil без reflection. Описывает только wire shape; точный предикат области и полномочия RPC остаются у вызывающего кода.
type roleImageRecipePageResponse interface {
	*controlplanev1.ListRoleImageRecipesResponse | *controlplanev1.ListOrganizationRoleImageRecipesResponse

	// GetRecipes reads owner-returned recipes in their original order; projection does not mutate the slice or its elements.
	// GetRecipes читает рецепты владельца в исходном порядке; проекция не изменяет срез и его элементы.
	GetRecipes() []*controlplanev1.RoleImageRecipe
	// GetTotal reads the owner count for safe-integer and page consistency checks, not an independently computed total.
	// GetTotal читает счётчик владельца для проверки safe integer и согласованности страницы, а не независимо вычисленное количество.
	GetTotal() int64
	// GetPage reads optional continuation metadata; an absent protobuf page yields no next token.
	// GetPage читает необязательные метаданные продолжения; отсутствующая protobuf-страница не задаёт следующий токен.
	GetPage() *controlplanev1.PageInfo
}

// writeRoleImageRecipePage validates counts, ordered unique recipes, lineage/source visibility and cursor advancement before writing a public page. Its non-nil synchronous predicate is supplied by the specialized caller; it does not perform RPC, authorize scope, admit images or write partial items on failure.
// writeRoleImageRecipePage проверяет счётчики, порядок и уникальность рецептов, lineage/source visibility и продвижение cursor до выдачи публичной страницы. Ненулевой синхронный предикат задаёт специализированный вызывающий код; helper не выполняет RPC, не авторизует область, не допускает образы и не выдаёт частичные элементы при ошибке.
func writeRoleImageRecipePage[R roleImageRecipePageResponse](writer http.ResponseWriter, response R, pageSize *int, pageToken *string, inScope func(*controlplanev1.RoleImageRecipe) bool) {
	if response == nil || response.GetTotal() < int64(len(response.GetRecipes())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetRecipes()) > 100 ||
		pageSize != nil && len(response.GetRecipes()) > *pageSize {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.RoleImageRecipePage{Items: make([]generated.RoleImageRecipe, 0, len(response.GetRecipes())), Total: response.GetTotal()}
	seen := make(map[string]bool, len(response.GetRecipes()))
	for _, recipe := range response.GetRecipes() {
		if recipe == nil || !inScope(recipe) || !effectiveCapabilityRef(recipe.GetRef()) || seen[recipe.GetRef()] ||
			!validRoleImageLineage(recipe.GetManagedLineage()) || !validRoleImageSource(recipe) {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[recipe.GetRef()] = true
		result.Items = append(result.Items, publicRoleImageRecipe(recipe))
	}
	if token := response.GetPage().GetNextPageToken(); token != "" {
		if !boundedModelText(token, 512) || token == stringValue(pageToken) || len(result.Items) == 0 {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		result.NextPageToken = &token
	}
	writeJSON(writer, http.StatusOK, result)
}
