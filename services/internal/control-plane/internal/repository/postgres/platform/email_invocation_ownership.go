package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var (
	// queryEmailInvocationContext derives root lineage and mailbox connection only from the persisted invocation; no model-supplied actor or parent identifier is accepted.
	// queryEmailInvocationContext выводит root lineage и mailbox connection только из сохранённой invocation; actor и parent identifier модели не принимаются.
	//go:embed sql/email_invocation__context.sql
	queryEmailInvocationContext string
	// queryEmailInvocationPin stores the first immutable mailbox ownership and configuration binding beside the existing intent.
	// queryEmailInvocationPin сохраняет первую неизменную привязку владельца и конфигурации mailbox рядом с существующим intent.
	//go:embed sql/email_invocation__pin.sql
	queryEmailInvocationPin string
	// queryEmailInvocationMatches requires current eligibility to match the original pin and never rewrites it during rejoin or Begin.
	// queryEmailInvocationMatches требует совпадения текущей eligibility с исходным pin и не переписывает его при rejoin либо Begin.
	//go:embed sql/email_invocation__matches.sql
	queryEmailInvocationMatches string
	// queryEmailInvocationCompletionSource separates a new READ result from an already stored
	// successful fact before acquiring the invocation lock; it accepts no caller risk or state.
	// queryEmailInvocationCompletionSource отделяет новый READ result от сохранённого successful
	// fact до блокировки invocation; risk и state не принимаются от caller.
	//go:embed sql/email_invocation__completion_source.sql
	queryEmailInvocationCompletionSource string
)

// emailInvocationOwnership carries fresh private root and mailbox coordinates under the Connection lock, with no transported credential descriptor or value.
// emailInvocationOwnership хранит текущие закрытые root и mailbox coordinates под блокировкой Connection без передаваемого credential descriptor и значения.
type emailInvocationOwnership struct {
	invocationID, connectionID, connectionRef, rootID, initiatorID, actorRef, source string
	owner                                                                            emailCredentialOwnerRow
	mailbox                                                                          emailpolicy.MailboxProjection
}

// resolveEmailInvocationOwnershipTx checks the current root actor, interactive provenance for personal mail, mailbox owner epoch and accepted configuration before creating or reusing any call authority.
// resolveEmailInvocationOwnershipTx проверяет текущего root actor, интерактивное происхождение личной почты, mailbox owner epoch и принятую конфигурацию до создания либо повторного использования полномочий вызова.
func (repository *Repository) resolveEmailInvocationOwnershipTx(ctx context.Context, tx pgx.Tx, current scope, invocationRef string) (emailInvocationOwnership, error) {
	var result emailInvocationOwnership
	var rawScope []byte
	err := tx.QueryRow(ctx, queryEmailInvocationContext, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": invocationRef}).Scan(
		&result.invocationID, &result.connectionID, &result.connectionRef, &result.rootID, &result.initiatorID, &result.actorRef, &result.source, &rawScope)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, serializableTransactionError(err, errs.ErrUnavailable)
	}
	result.owner, err = requireEmailCredentialOwnerTx(ctx, tx, current, result.connectionRef, result.actorRef, 0)
	if err != nil {
		return result, err
	}
	if !result.owner.connectionEnabled || result.owner.connectionState != "CONNECTED" {
		return result, errs.ErrForbidden
	}
	if result.owner.owner.Policy == entity.IntegrationCredentialInitiatorRequired && (result.source == "SCHEDULE" || result.source == "INTEGRATION") {
		return result, errs.ErrForbidden
	}
	var resource map[string]string
	if json.Unmarshal(rawScope, &resource) != nil || resource["mailbox_id"] == "" {
		return result, errs.ErrUnavailable
	}
	result.mailbox, err = repository.readEmailMailbox(ctx, tx, current, resource["mailbox_id"], 0)
	if err != nil {
		return result, err
	}
	if result.mailbox.ConnectionRef != result.connectionRef || result.mailbox.OrganizationRef != current.organizationRef ||
		result.mailbox.CredentialOwnerGeneration != result.owner.owner.Generation {
		return result, errs.ErrForbidden
	}
	return result, nil
}

// emailInvocationOwnershipArgs uses only freshly resolved owner state and the accepted mailbox snapshot, not any request's subject, scope digest or credential selector.
// emailInvocationOwnershipArgs использует только заново разрешённое owner state и принятый mailbox snapshot, а не subject, scope digest или credential selector запроса.
func emailInvocationOwnershipArgs(current scope, choice emailInvocationOwnership) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": choice.invocationID, "connection_id": choice.connectionID,
		"root_run_id": choice.rootID, "initiator_id": choice.initiatorID, "owner_subject_id": choice.owner.owner.SubjectID,
		"owner_generation": choice.owner.owner.Generation, "mailbox_ref": choice.mailbox.Ref, "mailbox_revision": choice.mailbox.Revision, "mailbox_source_digest": choice.mailbox.SourceDigest}
}

// pinEmailInvocationMailboxTx records exactly one mailbox pin for a new intent and never reads the generic personal token selection path.
// pinEmailInvocationMailboxTx сохраняет ровно один mailbox pin нового intent и не читает путь выбора generic personal token.
func (repository *Repository) pinEmailInvocationMailboxTx(ctx context.Context, tx pgx.Tx, current scope, invocationRef string) error {
	choice, err := repository.resolveEmailInvocationOwnershipTx(ctx, tx, current, invocationRef)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, queryEmailInvocationPin, emailInvocationOwnershipArgs(current, choice))
	if err != nil {
		return serializableTransactionError(err, mapWriteError(err))
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// requireEmailInvocationMailboxTx rechecks eligibility and the immutable mailbox pin before claim, owner authorization or a pending invocation alias can be reused.
// requireEmailInvocationMailboxTx перепроверяет eligibility и неизменный mailbox pin перед claim, owner authorization либо повторным использованием pending invocation alias.
func (repository *Repository) requireEmailInvocationMailboxTx(ctx context.Context, tx pgx.Tx, current scope, invocationRef string) (emailInvocationOwnership, error) {
	choice, err := repository.resolveEmailInvocationOwnershipTx(ctx, tx, current, invocationRef)
	if err != nil {
		return choice, err
	}
	var matches bool
	if err := tx.QueryRow(ctx, queryEmailInvocationMatches, emailInvocationOwnershipArgs(current, choice)).Scan(&matches); err != nil {
		return choice, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if !matches {
		return choice, errs.ErrForbidden
	}
	return choice, nil
}

// requireEmailReadCompletionMailboxTx checks current owner/root/configuration pins before a new
// READ result can be published, independently of bounded metadata cleanup. The Connection lock
// precedes the invocation lock, matching revoke order. An already SUCCEEDED source still needs
// the existing exact receipt replay checks; mutation facts do not gain new effect authority here.
// requireEmailReadCompletionMailboxTx проверяет текущие owner/root/configuration pins до выдачи
// нового READ result независимо от ограниченной metadata cleanup. Connection lock предшествует
// invocation lock, как при revoke. Для SUCCEEDED source остаются прежние exact receipt replay
// checks; mutation facts здесь не получают новых полномочий на effect.
func (repository *Repository) requireEmailReadCompletionMailboxTx(ctx context.Context, tx pgx.Tx, current scope, invocationRef string) error {
	var fresh bool
	if err := tx.QueryRow(ctx, queryEmailInvocationCompletionSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": invocationRef}).Scan(&fresh); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	} else if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if !fresh {
		return nil
	}
	_, err := repository.requireEmailInvocationMailboxTx(ctx, tx, current, invocationRef)
	return err
}
