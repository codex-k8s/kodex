package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// writeBackActor resolves active membership for the supplied actor/organization and requires the
// current organization ID, copying only correlation context; lookup failures close as Forbidden.
// writeBackActor разрешает активное membership указанного actor/organization и требует текущий
// organization ID, копируя только correlation context; ошибки поиска закрывает как Forbidden.
func (repository *Repository) writeBackActor(ctx context.Context, tx pgx.Tx, current scope, ref, organizationRef string) (scope, error) {
	var actor scope
	if tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, ref, organizationRef).Scan(
		&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role) != nil || actor.organizationID != current.organizationID {
		return actor, errs.ErrForbidden
	}
	actor.correlationRef = current.correlationRef
	return actor, nil
}

// writeBackEligibility revalidates both root and approver authority, substituting the current
// actor while approving, against current set/source/connection versions and exact
// credential/public-configuration JSON pins. A changed set version is Conflict; other pin
// mismatches are Forbidden.
// writeBackEligibility повторно проверяет полномочия root и approver, подставляя текущего actor
// при одобрении, по текущим set/source/connection versions и точным JSON pins
// credential/public-configuration. Изменённая версия set даёт Conflict; остальные несовпадения
// pins дают Forbidden.
func (repository *Repository) writeBackEligibility(ctx context.Context, tx pgx.Tx, current scope, row writeBackRow, approving bool) error {
	actors := []string{row.rootRef, row.approverRef}
	if approving {
		actors[1] = current.actorRef
	}
	for _, ref := range actors {
		if ref == "" {
			return errs.ErrForbidden
		}
		actor, err := repository.writeBackActor(ctx, tx, current, ref, row.organizationRef)
		if err != nil {
			return err
		}
		set, err := repository.writeBackSetAuthority(ctx, tx, actor, row.proposal.ConfigurationRef, row.proposal.Kind, true)
		if err != nil {
			return err
		}
		if set.Version != row.proposal.ConfigurationVersion {
			return errs.ErrConflict
		}
		source, fresh, _, _, err := repository.writeBackSource(ctx, tx, actor, set)
		if err != nil {
			return err
		}
		if source.Ref != row.proposal.SourceRef || source.Version != row.proposal.SourceVersion || source.AcceptedCommitSHA != row.proposal.BaseCommitSHA || source.AcceptedContentSHA256 != row.proposal.BaseContentSHA256 ||
			fresh.ConnectionVersion != row.proposal.ConnectionVersion || fresh.DefinitionDigest != row.snapshot.Source.DefinitionDigest ||
			!bytes.Equal(asJSON(fresh.CredentialRevision), asJSON(row.snapshot.Source.CredentialRevision)) || !bytes.Equal(asJSON(fresh.PublicConfiguration), asJSON(row.snapshot.Source.PublicConfiguration)) {
			return errs.ErrForbidden
		}
	}
	return nil
}

// writeBackActions rebuilds APPROVE/REJECT/CANCEL hints from current mutation access, database
// time and state; only APPROVE additionally checks fresh source eligibility. These hints do not
// replace command authority, approval digest or version checks.
// writeBackActions перестраивает подсказки APPROVE/REJECT/CANCEL по текущим mutation access,
// времени БД и состоянию; только APPROVE дополнительно проверяет свежую source eligibility.
// Подсказки не заменяют command authority, approval digest или version checks.
func (repository *Repository) writeBackActions(ctx context.Context, tx pgx.Tx, current scope, row *writeBackRow) {
	p := &row.proposal
	p.NextActions = nil
	_, ownerErr := repository.writeBackSetAuthority(ctx, tx, current, p.ConfigurationRef, p.Kind, true)
	var now time.Time
	timeErr := tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now)
	for _, action := range []string{"APPROVE", "REJECT", "CANCEL"} {
		reason := "NONE"
		switch {
		case ownerErr != nil || timeErr != nil:
			reason = "FORBIDDEN"
		case p.State == entity.WriteBackUnknown:
			reason = "OUTCOME_UNKNOWN"
		case writeBackTerminal(p.State):
			reason = "STATE"
		case action != "CANCEL" && p.State != entity.WriteBackWaiting:
			reason = "STATE"
		case action != "CANCEL" && !now.Before(p.ExpiresAt):
			reason = "EXPIRED"
		case action == "APPROVE" && repository.writeBackEligibility(ctx, tx, current, *row, true) != nil:
			reason = "SOURCE_CHANGED"
		}
		p.NextActions = append(p.NextActions, entity.ConfigurationWriteBackAction{Action: action, Reason: reason, Enabled: reason == "NONE"})
	}
}

// GetConfigurationWriteBack reads a locked validated proposal in RepeatableRead, checks set
// visibility and content permission including role-image source access, then adds action hints. It
// returns base/proposed text with the raw commit result, which may accompany a populated view.
// GetConfigurationWriteBack читает проверенное предложение с блокировкой в RepeatableRead,
// проверяет видимость set и разрешение содержимого, включая доступ к role-image source, затем
// добавляет подсказки действий. Возвращает исходный/предлагаемый текст с исходным результатом
// commit, который может сопровождать заполненный view.
func (repository *Repository) GetConfigurationWriteBack(ctx context.Context, principal value.Principal, ref string) (entity.ConfigurationWriteBackView, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ConfigurationWriteBackView{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.ConfigurationWriteBackView{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockWriteBack(ctx, tx, current, ref)
	if err != nil {
		return entity.ConfigurationWriteBackView{}, err
	}
	set, err := repository.writeBackSetAuthority(ctx, tx, current, row.proposal.ConfigurationRef, row.proposal.Kind, false)
	if err != nil {
		return entity.ConfigurationWriteBackView{}, err
	}
	permission := "organization.manage"
	if set.Kind == "ROLE_IMAGE" {
		permission = "image.build"
		if err := repository.managedRoleImageSourceAccess(ctx, tx, current, set.Ref, set.ProjectRef, "image.source.view"); err != nil {
			return entity.ConfigurationWriteBackView{}, err
		}
	}
	if repository.requireManagedSetAccess(ctx, tx, current, set, permission, "organization.manage") != nil {
		return entity.ConfigurationWriteBackView{}, errs.ErrForbidden
	}
	repository.writeBackActions(ctx, tx, current, &row)
	return entity.ConfigurationWriteBackView{Proposal: row.proposal, BaseContent: row.snapshot.BaseContent, ProposedContent: row.snapshot.ProposedContent}, tx.Commit(ctx)
}

// writeBackCursor carries the organization/actor/configuration digest and last proposal reference
// for unsigned base64url pagination; it is a scope consistency marker, not authorization.
// writeBackCursor несёт digest organization/actor/configuration и последнюю ссылку proposal для
// неподписанной base64url пагинации; это маркер согласованности scope, а не полномочия.
type writeBackCursor struct{ Scope, After string }

// ListConfigurationWriteBacks checks set visibility in RepeatableRead, validates the bounded
// scope-bound cursor, counts all proposals and reads a reference page plus one. It locks/validates
// each returned proposal and adds action hints without content; raw commit failure may accompany
// populated results.
// ListConfigurationWriteBacks проверяет видимость set в RepeatableRead, ограниченный cursor с
// привязкой scope, считает все предложения и читает страницу ссылок с одной дополнительной.
// Блокирует/проверяет каждое возвращаемое proposal и добавляет подсказки без содержимого; исходная
// ошибка commit может сопровождать заполненные результаты.
func (repository *Repository) ListConfigurationWriteBacks(ctx context.Context, principal value.Principal, ref string, filter query.Filter) ([]entity.ConfigurationWriteBack, string, int64, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", 0, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	set, err := repository.writeBackSetAuthority(ctx, tx, current, ref, "", false)
	if err != nil {
		return nil, "", 0, err
	}
	digest := writeBackDigest(asJSON([]string{current.organizationID, current.actorID, ref}))
	var cursor writeBackCursor
	if filter.Page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
		if err != nil || len(raw) > 1024 || json.Unmarshal(raw, &cursor) != nil || cursor.Scope != digest || cursor.After == "" {
			return nil, "", 0, errs.ErrInvalid
		}
	}
	var total int64
	if tx.QueryRow(ctx, queryWriteBackCount, current.organizationID, set.id).Scan(&total) != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryWriteBackList, current.organizationID, set.id, cursor.After, limit+1)
	if err != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		var item string
		if rows.Scan(&item) != nil {
			rows.Close()
			return nil, "", 0, errs.ErrUnavailable
		}
		refs = append(refs, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	next := ""
	if len(refs) > int(limit) {
		refs = refs[:limit]
		next = base64.RawURLEncoding.EncodeToString(asJSON(writeBackCursor{Scope: digest, After: refs[len(refs)-1]}))
	}
	result := make([]entity.ConfigurationWriteBack, 0, len(refs))
	for _, ref := range refs {
		row, err := lockWriteBack(ctx, tx, current, ref)
		if err != nil {
			return nil, "", 0, err
		}
		repository.writeBackActions(ctx, tx, current, &row)
		result = append(result, row.proposal)
	}
	return result, next, total, tx.Commit(ctx)
}
