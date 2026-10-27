package platform

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// catalogCursor carries a version, scope digest and bounded position in a base64
// envelope. It is not signed and never establishes authorization or ownership.
// catalogCursor переносит версию, дайджест области и ограниченную позицию в base64
// конверте. Он не подписан и не устанавливает авторизацию либо владение.
type catalogCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	Ref     string `json:"r"`
}

// catalogScope hashes the organization, actor, authority project, catalog kind
// and filter while excluding page size/token. The digest prevents accidental
// cursor reuse across these inputs; fresh resource eligibility remains mandatory.
// catalogScope хэширует организацию, actor, authority project, вид каталога
// и фильтр без размера страницы и токена. Дайджест предотвращает случайное
// переиспользование курсора между этими входами; свежая проверка допуска обязательна.
func catalogScope(current scope, kind string, filter query.Filter) string {
	filter.Page = query.Page{}
	raw, _ := json.Marshal(struct {
		Organization, Actor, Kind, AuthorityProject string
		Filter                                      query.Filter
	}{current.organizationID, current.actorID, kind, current.authorityProjectID, filter})
	digest := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// decodeCatalogCursor accepts an empty first-page token or verifies the version,
// scope digest and bounded nonempty position of a supplied envelope. Artifact
// positions have a larger limit for their compound time/ref representation.
// decodeCatalogCursor допускает пустой токен первой страницы либо проверяет версию,
// дайджест области и ограниченную непустую позицию конверта. Позиции артефактов
// имеют больший лимит для составного представления времени и ref.
func decodeCatalogCursor(current scope, kind string, filter query.Filter) (string, error) {
	if filter.Page.Token == "" {
		return "", nil
	}
	if len(filter.Page.Token) > 512 {
		return "", errs.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
	if err != nil {
		return "", errs.ErrInvalid
	}
	var cursor catalogCursor
	maximumPositionLength := 96
	if kind == "ARTIFACT" {
		maximumPositionLength = 256
	}
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Scope != catalogScope(current, kind, filter) || cursor.Ref == "" || len(cursor.Ref) > maximumPositionLength || strings.ContainsAny(cursor.Ref, "\x00\n\r") {
		return "", errs.ErrInvalid
	}
	return cursor.Ref, nil
}

// encodeCatalogCursor wraps the caller's continuation position with the current
// catalog scope digest and version. Callers choose valid positions; the token is
// a pagination locator and provides no signature or additional access rights.
// encodeCatalogCursor оборачивает позицию продолжения от вызывающего текущим
// дайджестом области каталога и версией. Корректность позиции задаёт вызывающий;
// токен является адресацией страницы, без подписи и дополнительных прав доступа.
func encodeCatalogCursor(current scope, kind string, filter query.Filter, ref string) string {
	raw, _ := json.Marshal(catalogCursor{Version: 1, Scope: catalogScope(current, kind, filter), Ref: ref})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCatalogRevisionCursor preserves the catalog envelope checks and the existing signed
// base-ten int64 parser for revision histories. Empty first-page tokens yield zero; supplied
// positions must be positive, including the native accepted plus sign and leading zeroes.
// Malformed, out-of-range or scope-mismatched positions return ErrInvalid. The unsigned scope
// digest is only a locator: callers still resolve the principal and check resource eligibility
// in their own read-only transaction after validation. This helper performs no I/O or ownership
// decision and does not choose a catalog kind or resource for its callers.
// decodeCatalogRevisionCursor сохраняет проверки конверта каталога и прежний разбор signed
// base-ten int64 для истории ревизий. Пустой токен первой страницы даёт ноль; переданная позиция
// должна быть положительной, с прежним допуском плюса и ведущих нулей. Неверная, выходящая за
// диапазон либо не совпадающая по scope позиция даёт ErrInvalid. Неподписанный scope digest —
// только locator: callers разрешают principal и проверяют ресурс в собственной read-only
// transaction после валидации. Helper не выполняет I/O, не решает вопрос владения и не выбирает
// вид каталога либо ресурс за вызывающего.
func decodeCatalogRevisionCursor(current scope, kind string, filter query.Filter) (int64, error) {
	cursor, err := decodeCatalogCursor(current, kind, filter)
	if err != nil {
		return 0, err
	}
	var before int64
	if cursor != "" {
		before, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil || before < 1 {
			return 0, errs.ErrInvalid
		}
	}
	return before, nil
}

// catalogPageWithCursor trims an already eligible lookahead page using the caller's positive
// bounded limit, keeping the original slice backing array and capacity. Only an excess row
// triggers the position callback, exactly once after trimming, on the last visible item; an
// empty or complete page keeps an empty continuation. Kind, scope, filter and typed position
// mapping stay caller-selected. Totals, SQL access checks, transactions and errors remain outside.
// catalogPageWithCursor обрезает уже допустимую lookahead страницу по положительному bounded
// limit вызывающего, сохраняя исходный backing array и capacity среза. Только лишняя строка
// вызывает position callback ровно один раз после обрезки, для последнего видимого элемента;
// пустая либо полная страница сохраняет пустое продолжение. Kind, scope, filter и типизированное
// преобразование позиции задаёт caller. Totals, SQL-допуск, транзакции и ошибки остаются снаружи.
func catalogPageWithCursor[T any](current scope, kind string, filter query.Filter, items []T, limit int32, position func(T) string) ([]T, string) {
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeCatalogCursor(current, kind, filter, position(items[len(items)-1]))
	}
	return items, next
}
