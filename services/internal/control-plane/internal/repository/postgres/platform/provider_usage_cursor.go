package platform

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// providerUsageCursor stores source digest, last ref/time and short expiry for unsigned
// provider-usage pagination. These pins detect stale source/position and are not a principal,
// ownership claim or permission.
// providerUsageCursor хранит source digest, последние ref/time и короткий expiry для неподписанной
// provider-usage pagination. Pins обнаруживают stale source/position и не являются principal,
// ownership claim либо permission.
type providerUsageCursor struct {
	Source, Ref          string
	UpdatedAt, ExpiresAt time.Time
}

// encodeProviderUsageCursor encodes source/ref/update time with a ten-second UTC expiry as raw
// URL-safe base64 JSON. It preserves the existing ignored marshal error and signs nothing; callers
// must authorize the underlying read independently.
// encodeProviderUsageCursor кодирует source/ref/update time с десятисекундным UTC expiry в raw
// URL-safe base64 JSON. Сохраняет прежнюю игнорируемую marshal error и ничего не подписывает;
// caller независимо авторизует underlying read.
func encodeProviderUsageCursor(source string, updatedAt time.Time, ref string) string {
	raw, _ := json.Marshal(providerUsageCursor{Source: source, Ref: ref, UpdatedAt: updatedAt, ExpiresAt: time.Now().UTC().Add(10 * time.Second)})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeProviderUsageCursor treats an empty token as the first page; otherwise enforces size,
// base64/strict JSON, exact source, valid ref, nonzero update time and an unexpired deadline no
// more than eleven seconds ahead. Invalid input is Invalid; returned timestamp/ref only define
// pagination and confer no access.
// decodeProviderUsageCursor считает пустой token первой страницей; иначе проверяет size,
// base64/strict JSON, exact source, valid ref, nonzero update time и unexpired deadline не далее
// одиннадцати секунд. Недопустимый input даёт Invalid; returned timestamp/ref задают лишь
// pagination и не выдают access.
func decodeProviderUsageCursor(source, token string) (*time.Time, string, error) {
	if token == "" {
		return nil, "", nil
	}
	if len(token) > 1024 {
		return nil, "", errs.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	var cursor providerUsageCursor
	if decodeStrict(raw, &cursor) != nil || cursor.Source != source || !validOverlayHistoryRef(cursor.Ref) || cursor.UpdatedAt.IsZero() || !cursor.ExpiresAt.After(time.Now().UTC()) || cursor.ExpiresAt.After(time.Now().UTC().Add(11*time.Second)) {
		return nil, "", errs.ErrInvalid
	}
	return &cursor.UpdatedAt, cursor.Ref, nil
}
