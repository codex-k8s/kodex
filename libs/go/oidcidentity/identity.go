// Package oidcidentity приводит идентификаторы стандартных OIDC-провайдеров
// к внутреннему UUID-представлению MatterCodex.
package oidcidentity

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

const (
	minimumOpaqueIDBytes = 16
	maximumOpaqueIDBytes = 256
)

var namespace = uuid.MustParse("ae20e041-30fe-4ac4-95c6-7b965b8dcbb3")

// Subject возвращает стабильный UUID субъекта. Исходный UUID сохраняется.
func Subject(issuer, raw string) (string, error) {
	return canonicalize(issuer, "subject", raw)
}

// SessionID возвращает стабильный UUID OIDC-сессии.
func SessionID(issuer, raw string) (string, error) {
	return canonicalize(issuer, "session", raw)
}

// TokenID возвращает стабильный UUID конкретного OIDC-токена.
func TokenID(issuer, raw string) (string, error) {
	return canonicalize(issuer, "token", raw)
}

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
