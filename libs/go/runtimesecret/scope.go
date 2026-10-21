package runtimesecret

import "errors"

// ScopeKind identifies the owner-selected resource boundary; the string is
// validated with its complete organization/project tuple before use.
// RU: ScopeKind обозначает назначенную владельцем область ресурса; перед
// использованием строка проверяется вместе с полным кортежем организации и проекта.
type ScopeKind string

const (
	// ScopeOrganization requires an organization and forbids a project reference.
	// RU: ScopeOrganization требует организацию и запрещает ссылку на проект.
	ScopeOrganization ScopeKind = "ORGANIZATION"
	// ScopeProject requires valid references to both organization and project.
	// RU: ScopeProject требует допустимые ссылки и на организацию, и на проект.
	ScopeProject ScopeKind = "PROJECT"
)

// ErrScopeInvalid is one value-free error for invalid kind or owner tuple.
// RU: ErrScopeInvalid является единой ошибкой без значений для неверной области
// либо кортежа владельца.
var ErrScopeInvalid = errors.New("runtime secret resource scope is invalid")

// ValidateScope requires a bounded organization reference for both kinds and
// a project reference exactly for PROJECT. It never infers ORGANIZATION from
// an empty project field or assigns authority from caller-provided values.
// RU: ValidateScope требует ограниченную ссылку на организацию для обеих
// областей и ссылку на проект только для PROJECT. Пустое поле проекта не
// превращает область в ORGANIZATION и не назначает полномочия по входным значениям.
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

// validScopeReference accepts 1 to 128 ASCII letters, digits, underscores or
// dashes; it rejects separators and whitespace before scope validation.
// RU: validScopeReference принимает от1 до128 ASCII-букв, цифр, подчёркиваний
// и дефисов; разделители и пробелы отклоняются до проверки области.
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
