package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryRepositoryBundlePersonalCredential resolves only the descriptor already pinned to the authoritative root invocation; no subject or Secret selector is accepted.
// queryRepositoryBundlePersonalCredential разрешает только descriptor, уже закреплённый за авторитетным root invocation; subject либо Secret selector не принимается.
//
//go:embed sql/repository_bundle__personal_credential.sql
var queryRepositoryBundlePersonalCredential string

// nullableBundleCredentialID preserves disjoint shared and personal revision tables; absent personal keys never fall back to shared descriptors.
// nullableBundleCredentialID сохраняет раздельные таблицы общих и личных ревизий; отсутствие личного ключа не разрешает общий fallback.
func nullableBundleCredentialID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// requireRepositoryBundlePersonalCredential rechecks the root's current own binding before every new work admission, preserving immutable receipt/recovery after a begun effect.
// requireRepositoryBundlePersonalCredential повторно проверяет текущую собственную привязку root перед новым допуском; immutable receipt/recovery начатого эффекта сохраняется.
func (repository *Repository) requireRepositoryBundlePersonalCredential(ctx context.Context, tx pgx.Tx, current scope, row repositoryBundleRow) error {
	if row.snapshot.PersonalBindingRef == "" {
		return nil
	}
	choice, err := repository.selectPersonalInvocationCredentialTx(ctx, tx, current.organizationID, row.proposal.InvocationRef)
	if err != nil {
		return err
	}
	selected := choice.selection
	if selected.PersonalBindingRef != row.snapshot.PersonalBindingRef || selected.BindingGeneration != row.snapshot.PersonalBindingGeneration || selected.Credential == nil || *selected.Credential != row.snapshot.Credential {
		return errs.ErrForbidden
	}
	return nil
}
