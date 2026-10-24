package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryConfigurationSourceAccept pins an accepted commit/content digest and published revision on
// the exact source, refreshes sync time and clears its previous raw content before the matching
// raw write.
// queryConfigurationSourceAccept закрепляет accepted commit/content digest и published revision
// точного source, обновляет sync time и очищает прежний raw content перед соответствующей raw
// write.
//
//go:embed sql/configuration_source__accept.sql
var queryConfigurationSourceAccept string

// queryConfigurationSourceAcceptSet updates only the exact GIT-managed set source revision after
// accepted source publication without modifying UI-owned sets.
// queryConfigurationSourceAcceptSet обновляет source revision только точного GIT-managed set после
// accepted source publication без изменения UI-owned sets.
//
//go:embed sql/configuration_source__accept_set.sql
var queryConfigurationSourceAcceptSet string

// queryConfigurationSourceAcceptRaw stores raw accepted content only when source ID, accepted
// commit and content digest still match, allowing affected-row confirmation in the owner
// transaction.
// queryConfigurationSourceAcceptRaw сохраняет raw accepted content только при совпадении source
// ID, accepted commit и content digest для проверки affected-row в owner transaction.
//
//go:embed sql/configuration_source__accept_raw.sql
var queryConfigurationSourceAcceptRaw string

// validSourceCommit accepts exactly forty or sixty-four lowercase hexadecimal characters as a Git
// commit locator, rejecting prefixes and all other characters without verifying ancestry.
// validSourceCommit принимает ровно сорок либо шестьдесят четыре lowercase hexadecimal символа как
// Git commit locator, отклоняя prefixes и остальные символы без проверки ancestry.
func validSourceCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	for _, c := range commit {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// sourceCompletionValid checks bounded nonempty UTF8/NUL-free content, exact content SHA256 and
// the server-pinned predecessor. It accepts only INITIAL for no predecessor, UNCHANGED with
// identical digest for the same commit, otherwise FAST_FORWARD; this validates a supplied ancestry
// classification rather than independently traversing Git history.
// sourceCompletionValid проверяет ограниченный непустой UTF8/NUL-free content, точный content
// SHA256 и server-pinned predecessor. Принимает только INITIAL без predecessor, UNCHANGED с тем же
// digest для того же commit, иначе FAST_FORWARD; проверяет переданную ancestry classification, а
// не самостоятельно обходит Git history.
func sourceCompletionValid(row sourceWorkRow, source configurationSource, input platformrepo.ConfigurationSourceCompletion) bool {
	if !validSourceCommit(input.CommitSHA) || len(input.Content) == 0 || len(input.Content) > int(row.work.MaximumContentBytes) ||
		!utf8.Valid(input.Content) || strings.ContainsRune(string(input.Content), 0) {
		return false
	}
	digest := sha256.Sum256(input.Content)
	if hex.EncodeToString(digest[:]) != input.ContentSHA256 || row.work.PreviousCommitSHA != source.AcceptedCommitSHA {
		return false
	}
	if source.AcceptedCommitSHA == "" {
		return input.Ancestry == "INITIAL"
	}
	if source.AcceptedCommitSHA == input.CommitSHA {
		return input.Ancestry == "UNCHANGED" && source.AcceptedContentSHA256 == input.ContentSHA256
	}
	return input.Ancestry == "FAST_FORWARD"
}

// completeConfigurationSource locks exact source work in a Serializable transaction and returns a
// matching stored completion before fresh lease/eligibility checks. New completion validates live
// lease, source generation and content/provenance; lost root eligibility blocks source, unchanged
// content is revalidated without publication, and new valid content publishes a revision plus any
// role-image effect. Invalid content retains an INVALID candidate and blocks sync; ready raw
// content, source state/audit and durable work receipt are finalized together before commit.
// completeConfigurationSource блокирует точную source work в Serializable транзакции и возвращает
// совпавшее сохранённое completion до новых lease/eligibility checks. Новое completion проверяет
// live lease, source generation и content/provenance; потеря root eligibility блокирует source,
// неизменённый content повторно проверяется без публикации, новый допустимый content публикует
// revision и возможный role-image effect. Неверный content сохраняет INVALID candidate и блокирует
// sync; ready raw content, source state/audit и устойчивая work receipt завершаются вместе до
// commit.
func (repository *Repository) completeConfigurationSource(ctx context.Context, p value.Principal, input platformrepo.ConfigurationSourceCompletion) (entity.ManagedConfigurationGitSource, error) {
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockSourceWork(ctx, tx, current, input.Lease.WorkRef)
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	digest := sourceActionDigest("COMPLETE", input.Lease, struct {
		Commit, Digest, Ancestry string
		Content                  []byte
	}{input.CommitSHA, input.ContentSHA256, input.Ancestry, input.Content})
	if result, found, err := sourceReplay(row, input.Lease, "COMPLETE", digest); found {
		return result, err
	}
	if !sourceLeaseMatches(row, input.Lease, time.Now().UTC()) {
		return entity.ManagedConfigurationGitSource{}, errs.ErrForbidden
	}
	source, actor, eligibilityErr := repository.sourceWorkEligibility(ctx, tx, row, current)
	if source.id == "" || source.Generation != input.Lease.SourceGeneration || source.State == entity.ConfigurationSourceDetached {
		return entity.ManagedConfigurationGitSource{}, errs.ErrForbidden
	}
	if !sourceCompletionValid(row, source, input) {
		return entity.ManagedConfigurationGitSource{}, errs.ErrInvalid
	}
	next, failure := entity.ConfigurationSourceReady, ""
	if eligibilityErr != nil {
		next, failure = entity.ConfigurationSourceBlocked, entity.ConfigurationSourceAccess
		if actor.actorID == "" {
			actor = current
		}
	} else if input.Ancestry == "UNCHANGED" {
		set, err := repository.resolveManagedSet(ctx, tx, actor, command.ManagedConfigurationInput{ConfigurationRef: row.configurationRef}, row.work.Kind, false)
		if err != nil {
			return entity.ManagedConfigurationGitSource{}, err
		}
		if set.Kind == "INTEGRATION_DEFINITION" {
			_, _, err = integrationpackage.NormalizeManagedRevision(input.Content, "GIT", repository.integrationDefinitions)
		} else {
			err = repository.validateSourceRoleImage(set, row.work.ContentFormat, string(input.Content))
		}
		if err == errs.ErrUnavailable {
			return entity.ManagedConfigurationGitSource{}, err
		}
		if err != nil {
			next, failure = entity.ConfigurationSourceBlocked, entity.ConfigurationSourceContent
		}
	} else {
		set, err := repository.resolveManagedSet(ctx, tx, actor, command.ManagedConfigurationInput{ConfigurationRef: row.configurationRef}, row.work.Kind, false)
		if err != nil {
			return entity.ManagedConfigurationGitSource{}, err
		}
		format, content := row.work.ContentFormat, string(input.Content)
		valid := true
		if set.Kind == "INTEGRATION_DEFINITION" {
			_, canonical, err := integrationpackage.NormalizeManagedRevision(input.Content, "GIT", repository.integrationDefinitions)
			valid = err == nil
			if valid {
				format, content = "JSON", string(canonical)
			}
		} else {
			err := repository.validateSourceRoleImage(set, format, content)
			if err == errs.ErrUnavailable {
				return entity.ManagedConfigurationGitSource{}, err
			}
			valid = err == nil
		}
		ref, err := newRef("mrev")
		if err != nil {
			return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
		}
		contentDigest := sha256.Sum256([]byte(content))
		revision, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationInsertRevision, pgx.StrictNamedArgs{
			"revision_ref": ref, "organization_id": actor.organizationID, "configuration_set_id": set.id, "content_format": format, "content": content,
			"digest": hex.EncodeToString(contentDigest[:]), "parent_revision_id": set.currentRevisionID, "actor_id": actor.actorID}))
		if err != nil {
			return entity.ManagedConfigurationGitSource{}, mapWriteError(err)
		}
		state := "VALID"
		diagnostics := []string{}
		if !valid {
			state = "INVALID"
			diagnostics = []string{"REVISION_SEMANTICS_INVALID:Configuration source content is invalid"}
			next, failure = entity.ConfigurationSourceBlocked, entity.ConfigurationSourceContent
		}
		if _, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationValidateRevision, pgx.StrictNamedArgs{"revision_id": revision.internalID, "state": state, "diagnostics": asJSON(diagnostics)})); err != nil {
			return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
		}
		if valid {
			// RoleImage effect материализуется тем же owner до публикации revision.
			if set.Kind == "ROLE_IMAGE" {
				if err := repository.publishSourceRoleImage(ctx, tx, actor, set, revision.ManagedConfigurationRevision); err != nil {
					return entity.ManagedConfigurationGitSource{}, err
				}
			}
			if _, _, _, err := scanPublishedManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationPublishRevision, pgx.StrictNamedArgs{
				"configuration_set_id": set.id, "revision_id": revision.internalID, "expected_version": set.Version})); err != nil {
				return entity.ManagedConfigurationGitSource{}, errs.ErrConflict
			}
			if _, err := tx.Exec(ctx, queryConfigurationSourceAccept, source.id, input.CommitSHA, input.ContentSHA256, revision.internalID); err != nil {
				return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
			}
			if _, err := tx.Exec(ctx, queryConfigurationSourceAcceptSet, set.id, input.CommitSHA); err != nil {
				return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
			}
			source, err = readConfigurationSource(ctx, tx, actor.organizationID, set.Ref)
			if err != nil {
				return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
			}
		} else {
			if err := tx.QueryRow(ctx, queryManagedConfigurationTouchSet, pgx.StrictNamedArgs{"configuration_set_id": set.id, "expected_version": set.Version}).Scan(&set.Version, &set.UpdatedAt); err != nil {
				return entity.ManagedConfigurationGitSource{}, errs.ErrConflict
			}
		}
	}
	if next == entity.ConfigurationSourceReady {
		tag, err := tx.Exec(ctx, queryConfigurationSourceAcceptRaw, source.id, input.CommitSHA, input.ContentSHA256, string(input.Content))
		if err != nil || tag.RowsAffected() != 1 {
			return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
		}
	}
	result, err := repository.sourceState(ctx, tx, actor, source, next, failure)
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	row.receipts[sourceReceiptKey(input.Lease)] = sourceWorkReceipt{Action: "COMPLETE", Digest: digest, Result: result}
	if _, err := tx.Exec(ctx, queryConfigurationSourceFinish, row.id, "COMPLETED", asJSON(row.receipts), failure, digest); err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrConflict
	}
	return result, nil
}
