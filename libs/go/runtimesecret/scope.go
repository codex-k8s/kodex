package runtimesecret

import "errors"

// ScopeKind задаёт область ресурса, назначенную только авторитетным владельцем.
type ScopeKind string

const (
	ScopeOrganization ScopeKind = "ORGANIZATION"
	ScopeProject      ScopeKind = "PROJECT"
)

var ErrScopeInvalid = errors.New("runtime secret resource scope is invalid")

// ValidateScope закрыто проверяет область и точный кортеж владельца.
// Пустой projectRef не является основанием для выбора ORGANIZATION.
func ValidateScope(kind ScopeKind, organizationRef, projectRef string) error {
	if !validScopeReference(organizationRef) {
		return ErrScopeInvalid
	}
	switch kind {
	case ScopeOrganization:
		if projectRef != "" {
			return ErrScopeInvalid
		}
	case ScopeProject:
		if !validScopeReference(projectRef) {
			return ErrScopeInvalid
		}
	default:
		return ErrScopeInvalid
	}
	return nil
}

func validScopeReference(ref string) bool {
	if len(ref) == 0 || len(ref) > 128 {
		return false
	}
	for _, c := range ref {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
