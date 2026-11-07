// Package oidcidentity maps bounded OIDC identifiers to stable internal UUIDs.
// It does not verify tokens, issuer trust, signatures, roles or session lifetime.
// Пакет oidcidentity преобразует ограниченные OIDC-идентификаторы в устойчивые
// внутренние UUID. Токены, доверие issuer, подписи, роли и сроки он не проверяет.
package oidcidentity

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

const (
	// minimumOpaqueIDBytes rejects empty or trivially short external identifiers
	// before either UUID parsing or deterministic opaque-ID derivation.
	// minimumOpaqueIDBytes отклоняет пустые и слишком короткие внешние ID
	// до разбора UUID или детерминированного преобразования opaque ID.
	minimumOpaqueIDBytes = 16
	// maximumOpaqueIDBytes bounds the raw byte input accepted by canonicalization.
	// maximumOpaqueIDBytes ограничивает вход canonicalization по числу байт.
	maximumOpaqueIDBytes = 256
)

// namespace is part of the persisted opaque-ID mapping contract. Changing it
// would give existing non-UUID identities different internal identifiers.
// namespace — часть сохраняемого контракта отображения opaque ID. Его изменение
// назначило бы существующим не-UUID identity другие внутренние идентификаторы.
var namespace = uuid.MustParse("ae20e041-30fe-4ac4-95c6-7b965b8dcbb3")

// Subject canonicalizes an external subject: parsed UUIDs keep their identity in
// canonical spelling; opaque values derive a UUID in the issuer/subject namespace.
// Subject канонизирует внешний subject: UUID сохраняет идентичность в стандартной
// записи, а opaque значение получает UUID в пространстве issuer/subject.
func Subject(issuer, raw string) (string, error) {
	return canonicalize(issuer, "subject", raw)
}

// ScopedSubject always derives identity from exact issuer and case-sensitive printable ASCII subject, including UUID-shaped values.
// It accepts bounded standard subjects of one to 255 bytes, preserves spaces and never applies the legacy UUID shortcut.
// Issuer trust and token authenticity must be established separately before using the result.
// ScopedSubject всегда выводит identity из точного issuer и регистрозависимого printable ASCII subject, включая UUID.
// Допускает standard subjects длиной 1–255 байт, сохраняет пробелы и не применяет legacy UUID shortcut.
// Доверие к issuer и подлинность token проверяются отдельно до использования результата.
func ScopedSubject(issuer, raw string) (string, error) {
	// invalidScopedSubject keeps rejection diagnostics independent of the supplied subject.
	// invalidScopedSubject сохраняет diagnostic отказа независимо от переданного subject.
	const invalidScopedSubject = "OIDC scoped subject is invalid"
	if issuer == "" || strings.TrimSpace(issuer) != issuer || len(issuer) > 2048 || len(raw) < 1 || len(raw) > 255 {
		return "", errors.New(invalidScopedSubject)
	}
	for _, character := range []byte(raw) {
		if character < 0x20 || character > 0x7e {
			return "", errors.New(invalidScopedSubject)
		}
	}
	return uuid.NewSHA1(namespace, []byte(issuer+"\x00subject\x00"+raw)).String(), nil
}

// SessionID returns a canonical session UUID, isolating opaque inputs by exact
// issuer and the session kind rather than trimming or normalizing their content.
// SessionID возвращает канонический UUID сессии, разделяя opaque ввод по точному
// issuer и виду session без обрезки или нормализации содержимого.
func SessionID(issuer, raw string) (string, error) {
	return canonicalize(issuer, "session", raw)
}

// TokenID returns the token UUID, using a distinct namespace for opaque token
// identifiers. It establishes no token validity, uniqueness or replay protection.
// TokenID возвращает UUID токена с отдельным пространством для opaque token ID.
// Корректность токена, его уникальность и защиту от replay функция не доказывает.
func TokenID(issuer, raw string) (string, error) {
	return canonicalize(issuer, "token", raw)
}

// canonicalize rejects malformed bounded inputs, preserves parsable UUID identity
// and otherwise derives a deterministic UUID from NUL-separated issuer/kind/raw.
// It validates the issuer's size and whitespace, not its URL or trustworthiness.
// canonicalize отклоняет неверный ограниченный ввод, сохраняет UUID-идентичность
// и иначе вычисляет устойчивый UUID из issuer/kind/raw, разделённых NUL.
// Для issuer проверяются размер и пробелы, но не URL или доверенность источника.
func canonicalize(issuer, kind, raw string) (string, error) {
	if issuer == "" || strings.TrimSpace(issuer) != issuer || len(issuer) > 2048 ||
		kind == "" || !validOpaqueID(raw) {
		return "", errors.New("OIDC identity is invalid")
	}
	if parsed, err := uuid.Parse(raw); err == nil {
		return parsed.String(), nil
	}
	return uuid.NewSHA1(namespace, []byte(issuer+"\x00"+kind+"\x00"+raw)).String(), nil
}

// validOpaqueID enforces byte bounds and an ASCII alphanumeric/-._~: allowlist;
// whitespace, separators and Unicode outside that grammar are not normalized.
// validOpaqueID проверяет размер в байтах и ASCII-набор букв/цифр/-._~:;
// пробелы, разделители и Unicode вне этой грамматики не нормализуются в допустимые.
func validOpaqueID(value string) bool {
	if len(value) < minimumOpaqueIDBytes || len(value) > maximumOpaqueIDBytes ||
		strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("-._~:", character) {
			continue
		}
		return false
	}
	return true
}
