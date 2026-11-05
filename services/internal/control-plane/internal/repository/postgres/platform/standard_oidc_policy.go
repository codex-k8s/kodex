package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// ConfigureStandardOIDCIdentity pins a validated independent copy before bootstrap; it never claims an owner or mutates live state itself.
// ConfigureStandardOIDCIdentity закрепляет проверенную независимую копию до bootstrap; метод сам не назначает owner и не меняет live state.
func (repository *Repository) ConfigureStandardOIDCIdentity(policy value.OIDCIdentityPolicy) error {
	if err := policy.Validate(); err != nil || repository.standardOIDCPolicy != nil {
		return errs.ErrInvalid
	}
	policy.BootstrapSubjects = slices.Clone(policy.BootstrapSubjects)
	slices.Sort(policy.BootstrapSubjects)
	repository.standardOIDCPolicy = &policy
	return nil
}

// scanStandardOIDCPolicy validates the complete persisted canonical policy and its independent revision/issuer/digest columns.
// Missing, unknown or inconsistent data does not become an empty or legacy authority policy.
// scanStandardOIDCPolicy проверяет полную persisted canonical policy и отдельные revision/issuer/digest columns.
// Отсутствующие, неизвестные и противоречивые данные не превращаются в пустую или legacy authority policy.
func scanStandardOIDCPolicy(row pgx.Row) (string, value.OIDCIdentityPolicy, string, error) {
	var organizationID, issuer, digest string
	var revision uint64
	var raw []byte
	if err := row.Scan(&organizationID, &revision, &issuer, &digest, &raw); err != nil {
		return "", value.OIDCIdentityPolicy{}, "", err
	}
	var policy value.OIDCIdentityPolicy
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 16384 || decoder.Decode(&policy) != nil || decoder.Decode(new(any)) != io.EOF {
		return "", value.OIDCIdentityPolicy{}, "", errs.ErrUnavailable
	}
	actual, err := policy.Digest()
	if err != nil || actual != digest || revision != policy.Revision || issuer != policy.Issuer {
		return "", value.OIDCIdentityPolicy{}, "", errs.ErrUnavailable
	}
	return organizationID, policy, digest, nil
}

// reconcileStandardOIDCPolicy advances deployment intent only inside installation bootstrap's existing serializable transaction.
// History and current high-watermark survive restarts; removing configuration or lowering/changing an accepted revision fails closed.
// reconcileStandardOIDCPolicy продвигает deployment intent только внутри существующей serializable bootstrap transaction.
// History и current high-watermark переживают restart; удаление configuration и понижение/изменение принятой revision закрыто отклоняются.
func (repository *Repository) reconcileStandardOIDCPolicy(ctx context.Context, tx pgx.Tx) error {
	storedOrganization, current, digest, readErr := scanStandardOIDCPolicy(tx.QueryRow(ctx, queryStandardOIDCPolicyRead))
	if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
		return bootstrapFailure("read standard OIDC policy", readErr)
	}
	if repository.standardOIDCPolicy == nil {
		if errors.Is(readErr, pgx.ErrNoRows) {
			return nil
		}
		return bootstrapFailure("standard OIDC mode downgrade denied", errs.ErrConflict)
	}
	desired := *repository.standardOIDCPolicy
	expected, err := desired.Digest()
	if err != nil {
		return bootstrapFailure("validate standard OIDC policy", err)
	}
	var organizationID string
	if err := tx.QueryRow(ctx, queryStandardOIDCInstallation).Scan(&organizationID); err != nil {
		return bootstrapFailure("resolve standard OIDC installation", err)
	}
	if readErr == nil {
		if storedOrganization != organizationID || current.Revision > desired.Revision || current.Revision == desired.Revision && digest != expected {
			return bootstrapFailure("standard OIDC policy rollback denied", errs.ErrConflict)
		}
		if current.Revision == desired.Revision {
			return nil
		}
	}
	if _, err := tx.Exec(ctx, queryStandardOIDCPolicyInsert, pgx.StrictNamedArgs{
		"revision": desired.Revision, "organization_id": organizationID, "issuer": desired.Issuer, "digest": expected, "document": asJSON(desired),
	}); err != nil {
		return bootstrapFailure("publish standard OIDC policy", err)
	}
	var historyOrganization, historyIssuer, historyDigest string
	if err := tx.QueryRow(ctx, queryStandardOIDCPolicySelectRevision, pgx.StrictNamedArgs{"revision": desired.Revision}).Scan(&historyOrganization, &historyIssuer, &historyDigest); err != nil {
		return bootstrapFailure("read standard OIDC policy history", err)
	}
	if historyOrganization != organizationID || historyIssuer != desired.Issuer || historyDigest != expected {
		return bootstrapFailure("standard OIDC history mismatch", errs.ErrConflict)
	}
	if _, err := tx.Exec(ctx, queryStandardOIDCPolicyAdvance, pgx.StrictNamedArgs{"revision": desired.Revision}); err != nil {
		return bootstrapFailure("advance standard OIDC policy", err)
	}
	actualOrganization, actual, actualDigest, err := scanStandardOIDCPolicy(tx.QueryRow(ctx, queryStandardOIDCPolicyRead))
	if err != nil || actualOrganization != organizationID || actual.Revision != desired.Revision || actualDigest != expected {
		return bootstrapFailure("verify standard OIDC policy", errs.ErrConflict)
	}
	return nil
}

// standardOIDCPolicyReady requires the database authority to match the process configuration and refuses an implicit return to legacy mode.
// standardOIDCPolicyReady требует совпадения database authority с process configuration и запрещает неявный возврат к legacy mode.
func (repository *Repository) standardOIDCPolicyReady(ctx context.Context) error {
	_, current, digest, err := scanStandardOIDCPolicy(repository.pool.QueryRow(ctx, queryStandardOIDCPolicyRead))
	if repository.standardOIDCPolicy == nil && errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || repository.standardOIDCPolicy == nil {
		return errs.ErrUnavailable
	}
	expected, err := repository.standardOIDCPolicy.Digest()
	if err != nil || current.Revision != repository.standardOIDCPolicy.Revision || digest != expected {
		return errs.ErrUnavailable
	}
	return nil
}
