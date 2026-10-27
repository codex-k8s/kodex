package platform

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryProviderAccountBlockersPage embeds tenant-bound dependency pagination with visibility and
// cancel eligibility before limit. It returns visible/hidden counts and a digest over all
// dependency pins; the caller still authorizes the account.
// queryProviderAccountBlockersPage встраивает tenant-bound пагинацию зависимостей с visibility и
// cancel eligibility до limit. Возвращает видимые/скрытые counts и digest всех dependency pins;
// caller отдельно авторизует account.
//
//go:embed sql/provider_account_blockers_page.sql
var queryProviderAccountBlockersPage string

// queryProviderAccountDeletionRead embeds one organization/account deletion intent with
// non-completed cleanup count and grouped dependency totals. Missing intent is distinct from an
// empty blocker set; it performs no access check.
// queryProviderAccountDeletionRead встраивает чтение одного organization/account deletion intent с
// количеством незавершённых cleanup и сгруппированными зависимостями. Отсутствующий intent
// отличается от пустого набора blockers; access здесь не проверяется.
//
//go:embed sql/provider_account_deletion_read.sql
var queryProviderAccountDeletionRead string

// queryProviderAccountDeletionsRead embeds batch deletion hydration for exact organization/account
// refs, including cleanup and blocker counts. It does not filter by caller visibility or normalize
// inconsistent lifecycle fields.
// queryProviderAccountDeletionsRead встраивает batch hydration удаления для точных
// organization/account refs, включая cleanup и blocker counts. Не фильтрует по caller visibility и
// не нормализует противоречивые lifecycle fields.
//
//go:embed sql/provider_account_deletions_read.sql
var queryProviderAccountDeletionsRead string

// providerAccountBlockerKinds defines the ordered six supported dependency categories for input
// validation and complete count projection. Membership in this list does not prove visibility,
// cancellability or account ownership.
// providerAccountBlockerKinds задаёт упорядоченные шесть категорий зависимостей для input
// validation и полной проекции counts. Наличие в списке не доказывает visibility, cancellability
// либо account ownership.
var providerAccountBlockerKinds = []string{"AGENT", "PROVIDER_POOL", "AUTOMATION", "ACTIVE_TURN", "QUEUED_TURN", "WARM_RUNTIME"}

// providerBlockerCursor carries the source/filter context digest and last kind/ref key in an
// unsigned base64 JSON cursor. The reader checks context freshness; this value grants no
// authority.
// providerBlockerCursor переносит digest источника/фильтра и последний kind/ref key в
// неподписанном base64 JSON cursor. Reader проверяет актуальность context; значение не выдаёт
// полномочий.
type providerBlockerCursor struct {
	Context, After string
}

// ListProviderAccountBlockers validates kind/search/page bounds, opens an authorized
// provider-usage read and resolves the account inside tenant scope before reading its versioned
// blocker page. It commits before returning; commit failure maps to Conflict and deferred rollback
// remains bounded by the caller context.
// ListProviderAccountBlockers проверяет kind/search/page bounds, открывает авторизованное
// provider-usage чтение и разрешает account внутри tenant scope до чтения versioned blocker page.
// Требует commit до возврата; commit failure даёт Conflict, deferred rollback использует caller
// context.
func (repository *Repository) ListProviderAccountBlockers(ctx context.Context, principal value.Principal, input query.ProviderAccountBlockers) (entity.ProviderAccountBlockerPage, error) {
	if !strings.HasPrefix(input.AccountRef, "pacc_") || input.Kind != "" && !slices.Contains(providerAccountBlockerKinds, input.Kind) ||
		!utf8.ValidString(input.Query) || utf8.RuneCountInString(input.Query) > 200 || strings.ContainsRune(input.Query, 0) ||
		input.Page.Size < 1 || input.Page.Size > 100 || len(input.Page.Token) > 2048 {
		return entity.ProviderAccountBlockerPage{}, errs.ErrInvalid
	}
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: input.AccountRef}
	current, tx, err := repository.providerUsageRead(ctx, principal, func(scope) entity.AccessScope { return target })
	if err != nil {
		return entity.ProviderAccountBlockerPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	resolved, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, target)
	if err != nil {
		return entity.ProviderAccountBlockerPage{}, err
	}
	account, err := repository.providerAccountByRef(ctx, tx, current, input.AccountRef)
	if err != nil {
		return entity.ProviderAccountBlockerPage{}, err
	}
	result, err := repository.providerAccountBlockerPage(ctx, tx, current, resolved.resourceID, account.Version, input)
	if err != nil {
		return entity.ProviderAccountBlockerPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ProviderAccountBlockerPage{}, errs.ErrConflict
	}
	return result, nil
}

// providerAccountBlockerPage decodes an optional cursor, loads deletion version and visible/hidden
// SQL counts, then binds context to actor, project, account/version and all dependency pins.
// Filter/source drift yields VersionMismatch; one extra row determines the next token. It does not
// commit or independently authorize the account.
// providerAccountBlockerPage декодирует optional cursor, читает deletion version и SQL counts,
// затем связывает context с actor, project, account/version и всеми dependency pins. Изменение
// фильтра/источника даёт VersionMismatch; лишняя строка определяет next token. Не выполняет commit
// или самостоятельную авторизацию account.
func (repository *Repository) providerAccountBlockerPage(ctx context.Context, tx pgx.Tx, current scope, accountID string, accountVersion int64, input query.ProviderAccountBlockers) (entity.ProviderAccountBlockerPage, error) {
	var cursor providerBlockerCursor
	if input.Page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(input.Page.Token)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Context == "" || cursor.After == "" {
			return entity.ProviderAccountBlockerPage{}, errs.ErrInvalid
		}
	}
	result := entity.ProviderAccountBlockerPage{AccountVersion: accountVersion}
	deletion, err := repository.providerAccountDeletion(ctx, tx, current.organizationID, accountID)
	if err != nil {
		return result, err
	}
	if deletion != nil {
		result.DeletionIntentVersion = deletion.Version
	}
	var raw []byte
	var source string
	if err := tx.QueryRow(ctx, queryProviderAccountBlockersPage, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_id": accountID,
		"actor_id": current.actorID, "authority_project": current.authorityProjectID,
		"kind": input.Kind, "query": input.Query, "after_key": cursor.After, "page_size": input.Page.Size + 1,
	}).Scan(&raw, &result.Total, &result.HiddenCount, &source); err != nil {
		return result, errs.ErrUnavailable
	}
	if json.Unmarshal(raw, &result.Items) != nil {
		return result, errs.ErrUnavailable
	}
	result.ContextDigest = digestBytes(asJSON([]any{current.organizationID, current.actorID, current.authorityProjectID,
		input.AccountRef, accountVersion, result.DeletionIntentVersion, source}))
	cursorDigest := digestBytes(asJSON([]string{result.ContextDigest, input.Kind, input.Query}))
	if cursor.Context != "" && cursor.Context != cursorDigest {
		return entity.ProviderAccountBlockerPage{}, errs.ErrVersionMismatch
	}
	if len(result.Items) > int(input.Page.Size) {
		result.Items = result.Items[:input.Page.Size]
		last := result.Items[len(result.Items)-1]
		result.NextPageToken = base64.RawURLEncoding.EncodeToString(asJSON(providerBlockerCursor{Context: cursorDigest, After: last.Kind + "/" + last.Ref}))
	}
	return result, nil
}

// providerAccountDeletion reads and validates one deletion projection in the supplied transaction.
// Missing intent returns nil without error; scan/JSON/invariant failures are Unavailable. No
// lifecycle mutation or permission check occurs here.
// providerAccountDeletion читает и проверяет одну deletion projection в переданной transaction.
// Отсутствующий intent возвращает nil без ошибки; scan/JSON/invariant failures дают Unavailable.
// Lifecycle mutation и permission check здесь отсутствуют.
func (repository *Repository) providerAccountDeletion(ctx context.Context, tx pgx.Tx, organizationID, accountID string) (*entity.ProviderAccountDeletion, error) {
	var result entity.ProviderAccountDeletion
	var raw []byte
	err := tx.QueryRow(ctx, queryProviderAccountDeletionRead, pgx.StrictNamedArgs{"organization_id": organizationID, "account_id": accountID}).Scan(
		&result.Ref, &result.Version, &result.State, &result.SafeReason, &result.RequestedAt, &result.CompletedAt, &result.PendingCleanup, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil || json.Unmarshal(raw, &result.Blockers) != nil {
		return nil, errs.ErrUnavailable
	}
	if err := normalizeProviderAccountBlockerCounts(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// normalizeProviderAccountBlockerCounts rejects unknown state/reason pairs, invalid
// versions/timestamps/counts, duplicate kinds and inconsistent terminal cleanup. On success it
// replaces blockers with all six kinds in canonical order, filling absent totals with zero; a nil
// argument is not supported.
// normalizeProviderAccountBlockerCounts отклоняет неизвестные state/reason pairs, некорректные
// versions/timestamps/counts, повторяющиеся kinds и противоречивый terminal cleanup. При успехе
// заменяет blockers всеми шестью kinds в каноническом порядке, заполняя отсутствующие totals
// нулями; nil argument не поддерживается.
func normalizeProviderAccountBlockerCounts(result *entity.ProviderAccountDeletion) error {
	reasons := map[string]string{"PENDING_BLOCKERS": "WAITING_FOR_DEPENDENCIES", "CLEANUP_QUEUED": "CREDENTIAL_CLEANUP_PENDING",
		"CLEANING": "CREDENTIAL_CLEANUP_IN_PROGRESS", "FAILED": "CREDENTIAL_CLEANUP_FAILED", "DELETED": "ACCOUNT_DELETED"}
	if reasons[result.State] == "" || reasons[result.State] != result.SafeReason || result.Version < 1 || result.PendingCleanup < 0 ||
		result.RequestedAt.IsZero() || (result.State == "DELETED") != (result.CompletedAt != nil) {
		return errs.ErrUnavailable
	}
	counts := make(map[string]int64, len(result.Blockers))
	for _, item := range result.Blockers {
		if !slices.Contains(providerAccountBlockerKinds, item.Kind) || item.Total < 0 {
			return errs.ErrUnavailable
		}
		if _, duplicate := counts[item.Kind]; duplicate || result.State == "DELETED" && item.Total != 0 {
			return errs.ErrUnavailable
		}
		counts[item.Kind] = item.Total
	}
	if result.State == "DELETED" && (result.PendingCleanup != 0 || result.CompletedAt.Before(result.RequestedAt)) {
		return errs.ErrUnavailable
	}
	result.Blockers = make([]entity.ProviderAccountBlockerCount, 0, len(providerAccountBlockerKinds))
	for _, kind := range providerAccountBlockerKinds {
		result.Blockers = append(result.Blockers, entity.ProviderAccountBlockerCount{Kind: kind, Total: counts[kind]})
	}
	return nil
}

// hydrateProviderAccountLifecycle clears deletion/verification pointers on supplied account
// elements, hydrates and validates tenant-bound deletion rows, then hydrates latest verifications.
// Required missing deletion or inconsistent account/deletion state fails closed. Earlier slice
// mutations may remain on error; callers must discard partial results.
// hydrateProviderAccountLifecycle очищает deletion/verification pointers в переданных account
// elements, добавляет и проверяет tenant-bound deletion rows, затем latest verifications.
// Отсутствующее обязательное deletion или противоречивое account/deletion state закрыто
// отклоняется. Предыдущие изменения slice могут сохраниться при ошибке; caller отбрасывает partial
// results.
func (repository *Repository) hydrateProviderAccountLifecycle(ctx context.Context, tx pgx.Tx, current scope, items []entity.ProviderAccount) error {
	if len(items) == 0 {
		return nil
	}
	refs := make([]string, 0, len(items))
	byRef := make(map[string]*entity.ProviderAccount, len(items))
	for index := range items {
		refs = append(refs, items[index].Ref)
		byRef[items[index].Ref] = &items[index]
		items[index].Deletion = nil
		items[index].Verification = nil
	}
	rows, err := tx.Query(ctx, queryProviderAccountDeletionsRead, pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_refs": refs})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var raw []byte
		var deletion entity.ProviderAccountDeletion
		if rows.Scan(&ref, &deletion.Ref, &deletion.Version, &deletion.State, &deletion.SafeReason,
			&deletion.RequestedAt, &deletion.CompletedAt, &deletion.PendingCleanup, &raw) != nil || json.Unmarshal(raw, &deletion.Blockers) != nil {
			return errs.ErrUnavailable
		}
		item := byRef[ref]
		if item == nil || (item.State != "DELETING" && item.State != "DELETED") ||
			(item.State == "DELETED") != (deletion.State == "DELETED") || normalizeProviderAccountBlockerCounts(&deletion) != nil {
			return errs.ErrUnavailable
		}
		item.Deletion = &deletion
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	rows.Close()
	for _, item := range items {
		if (item.State == "DELETING" || item.State == "DELETED") && item.Deletion == nil {
			return errs.ErrUnavailable
		}
	}
	return repository.hydrateProviderVerifications(ctx, tx, current, refs, byRef)
}
