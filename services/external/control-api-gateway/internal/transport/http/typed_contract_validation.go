package httptransport

import (
	"net/http"
	"regexp"
	"strconv"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// opaqueHTTPReference defines the common opaque HTTP locator grammar; individual endpoints may
// impose a narrower length or resource prefix and must independently authorize the locator.
// opaqueHTTPReference задаёт общую грамматику opaque HTTP locator; отдельный endpoint может сужать
// длину или префикс и обязан отдельно авторизовать адресата.
var opaqueHTTPReference = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// httpIdempotencyKey defines the bounded ASCII mutation-key grammar; matching a key provides
// idempotency syntax and never proves owner authority.
// httpIdempotencyKey задаёт ограниченную ASCII грамматику ключа мутации; совпадение подтверждает
// синтаксис idempotency, а не полномочия владельца.
var httpIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

// validHTTPPage allows omitted page controls or a size from 1 to 100 and a token up to 512 bytes.
// It does not decode continuation scope or filter server results.
// validHTTPPage разрешает отсутствие параметров страницы либо размер от 1 до 100 и токен до 512
// байт. Область продолжения не декодируется и серверные результаты не фильтруются.
func validHTTPPage(size *int, token *string) bool {
	return (size == nil || *size >= 1 && *size <= 100) && (token == nil || len(*token) <= 512)
}

// requireVersionedMutation requires a valid idempotency key and canonical quoted positive If-Match
// within the safe JSON integer range. Invalid headers produce HTTP 400 locally; resource ownership
// and OCC remain with the owner command.
// requireVersionedMutation требует корректный idempotency key и канонический положительный
// If-Match в кавычках в безопасном диапазоне JSON integer. Неверные заголовки дают локальный HTTP
// 400; владение ресурсом и OCC остаются у команды владельца.
func requireVersionedMutation(w http.ResponseWriter, key, etag string) (*controlplanev1.MutationContext, bool) {
	result, ok := mutation(key, etag)
	if !ok || !httpIdempotencyKey.MatchString(key) || result.GetExpectedVersion() < 1 || result.GetExpectedVersion() > maximumSafeJSONInteger ||
		etag != "\""+strconv.FormatInt(result.GetExpectedVersion(), 10)+"\"" {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return result, true
}
