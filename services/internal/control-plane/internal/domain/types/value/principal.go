// Package value содержит проверенные значения предметной области.
package value

import (
	"errors"
	"strings"
	"time"
)

// Principal carries the verified actor and credential context supplied by the
// internal authorization boundary; caller payload cannot construct authority.
// Principal переносит проверенный actor и контекст реквизита из внутренней
// границы авторизации; payload вызывающего не создаёт полномочия.
type Principal struct {
	ActorID                   string
	AuthorityTenant           string
	Permission                string
	CorrelationRef            string
	CallerWorkload            string
	ProjectRef                string
	CredentialRevision        uint64
	CredentialAuthenticatedAt time.Time
	CredentialACR             string
	CredentialAMR             []string
}

// Validate rejects a principal missing its actor, tenant, permission, trace,
// workload or nonzero credential revision before domain use.
// Validate отклоняет principal без actor, арендатора, разрешения, корреляции,
// workload или ненулевой ревизии реквизита до использования в домене.
func (principal Principal) Validate() error {
	if strings.TrimSpace(principal.ActorID) == "" ||
		strings.TrimSpace(principal.AuthorityTenant) == "" ||
		strings.TrimSpace(principal.Permission) == "" ||
		strings.TrimSpace(principal.CorrelationRef) == "" ||
		strings.TrimSpace(principal.CallerWorkload) == "" ||
		principal.CredentialRevision == 0 {
		return errors.New("principal is incomplete")
	}
	return nil
}

// AuthenticationIsFresh bounds credential age and rejects zero timestamps,
// nonpositive limits and authentication more than 30 seconds in the future.
// AuthenticationIsFresh ограничивает возраст реквизита и отклоняет нулевые
// метки времени, неположительный предел и вход более чем на 30 секунд в будущем.
func (principal Principal) AuthenticationIsFresh(now time.Time, maximumAge time.Duration) bool {
	if principal.CredentialAuthenticatedAt.IsZero() || now.IsZero() || maximumAge <= 0 {
		return false
	}
	authenticatedAt := principal.CredentialAuthenticatedAt.UTC()
	now = now.UTC()
	return !authenticatedAt.After(now.Add(30*time.Second)) && now.Sub(authenticatedAt) <= maximumAge
}

// InteractiveAuthenticationIsFresh additionally requires a nonblank ACR and
// at least one nonblank AMR from the same verified credential context.
// InteractiveAuthenticationIsFresh дополнительно требует непустой ACR и
// хотя бы один непустой AMR из того же проверенного контекста реквизита.
func (principal Principal) InteractiveAuthenticationIsFresh(now time.Time, maximumAge time.Duration) bool {
	if !principal.AuthenticationIsFresh(now, maximumAge) || strings.TrimSpace(principal.CredentialACR) == "" {
		return false
	}
	for _, method := range principal.CredentialAMR {
		if strings.TrimSpace(method) != "" {
			return true
		}
	}
	return false
}

// Mutation carries semantic idempotency and optimistic concurrency inputs for
// one command; validating its shape does not grant access to a resource.
// Mutation переносит семантическую идемпотентность и оптимистическую проверку
// версии одной команды; проверка формы не даёт доступ к ресурсу.
type Mutation struct {
	Operation       string
	IdempotencyKey  string
	ExpectedVersion *int64
	IntentDigest    string
}

// Validate checks the command name, bounded idempotency key, digest length and
// positive optional version without deciding ownership or intent semantics.
// Validate проверяет имя команды, длину ключа идемпотентности и дайджеста,
// положительную необязательную версию, не решая вопрос владения и смысла intent.
func (mutation Mutation) Validate() error {
	if strings.TrimSpace(mutation.Operation) == "" ||
		len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 ||
		len(mutation.IntentDigest) != 64 {
		return errors.New("mutation context is invalid")
	}
	if mutation.ExpectedVersion != nil && *mutation.ExpectedVersion < 1 {
		return errors.New("expected version is invalid")
	}
	return nil
}
