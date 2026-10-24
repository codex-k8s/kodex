package platform

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
