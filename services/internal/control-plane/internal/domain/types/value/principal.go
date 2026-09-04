// Package value содержит проверенные значения предметной области.
package value

import (
	"errors"
	"strings"
	"time"
)

// Principal выводится только из проверенного internal authorization context.
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

// AuthenticationIsFresh проверяет возраст аутентификации в проверенном
// credential context.
func (principal Principal) AuthenticationIsFresh(now time.Time, maximumAge time.Duration) bool {
	if principal.CredentialAuthenticatedAt.IsZero() || now.IsZero() || maximumAge <= 0 {
		return false
	}
	authenticatedAt := principal.CredentialAuthenticatedAt.UTC()
	now = now.UTC()
	return !authenticatedAt.After(now.Add(30*time.Second)) && now.Sub(authenticatedAt) <= maximumAge
}

// InteractiveAuthenticationIsFresh дополнительно требует подтверждённые ACR и
// хотя бы один AMR из того же credential context.
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

// Mutation связывает semantic idempotency и OCC с одной командой.
type Mutation struct {
	Operation       string
	IdempotencyKey  string
	ExpectedVersion *int64
	IntentDigest    string
}

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
