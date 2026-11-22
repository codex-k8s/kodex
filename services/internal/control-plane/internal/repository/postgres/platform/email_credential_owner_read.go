package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryEmailCredentialOwnerRead locks the exact email connection and resolves owner and actor subjects from the same tenant, without credential descriptors or values.
// queryEmailCredentialOwnerRead блокирует точное email-подключение и разрешает owner и actor subjects внутри одного tenant без дескрипторов и значений credentials.
//
//go:embed sql/email_credential_owner__read.sql
var queryEmailCredentialOwnerRead string

// emailCredentialOwnerRow retains private current ownership and the separately resolved initiator; no transported subject ID becomes authority.
// emailCredentialOwnerRow сохраняет закрытое текущее владение и отдельно разрешённого инициатора; переданный subject ID не становится полномочием.
type emailCredentialOwnerRow struct {
	owner                                         entity.EmailMailboxCredentialOwner
	initiator                                     entity.IntegrationCredentialInitiator
	connectionID, subjectRef, configuredBy, state string
	connectionVersion                             int64
	connectionState                               string
	connectionEnabled                             bool
	initiatorActive                               bool
}

// readEmailCredentialOwnerTx serializes ownership changes with consumers on the Connection row and rejects missing or malformed owner joins without exposing descriptor metadata.
// readEmailCredentialOwnerTx сериализует изменения владельца с потребителями на строке Connection и отклоняет отсутствующие либо повреждённые owner joins без выдачи metadata дескрипторов.
func readEmailCredentialOwnerTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef, actorRef string) (emailCredentialOwnerRow, error) {
	var row emailCredentialOwnerRow
	row.owner.OrganizationID, row.initiator.OrganizationID = current.organizationID, current.organizationID
	err := tx.QueryRow(ctx, queryEmailCredentialOwnerRead, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "connection_ref": connectionRef, "actor_ref": actorRef,
	}).Scan(&row.connectionID, &row.connectionVersion, &row.connectionState, &row.connectionEnabled, &row.owner.Policy,
		&row.owner.SubjectID, &row.subjectRef, &row.owner.SubjectKind, &row.owner.SubjectActive,
		&row.configuredBy, &row.owner.Generation, &row.state,
		&row.initiator.SubjectID, &row.initiator.SubjectKind, &row.initiatorActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil {
		return row, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return row, nil
}

// requireEmailCredentialOwnerTx checks a live root actor against current mailbox ownership and an optional immutable generation; a revoked owner never falls back to shared credentials.
// requireEmailCredentialOwnerTx проверяет живого root actor по текущему владельцу mailbox и необязательному immutable generation; отозванный owner не заменяется общими credentials.
func requireEmailCredentialOwnerTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef, actorRef string, generation int64) (emailCredentialOwnerRow, error) {
	row, err := readEmailCredentialOwnerTx(ctx, tx, current, connectionRef, actorRef)
	if err != nil {
		return row, err
	}
	if !row.initiatorActive || row.state != "ACTIVE" {
		return row, errs.ErrForbidden
	}
	if generation == 0 {
		generation = row.owner.Generation
	}
	return row, emailpolicy.AuthorizeCredentialOwner(row.owner, row.initiator, generation)
}

// requireEmailMailboxManagerTx permits configuration only by the active USER who established ownership, including an explicit service setup; generic integration administration does not transfer that ownership.
// requireEmailMailboxManagerTx разрешает настройку только активному USER, установившему владение, включая явный service setup; общие права управления интеграциями не передают это владение.
func requireEmailMailboxManagerTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef string) (emailCredentialOwnerRow, error) {
	row, err := readEmailCredentialOwnerTx(ctx, tx, current, connectionRef, current.actorRef)
	if err != nil {
		return row, err
	}
	if !row.initiatorActive || row.initiator.SubjectKind != "USER" || row.configuredBy != row.initiator.SubjectID || row.owner.Generation < 1 {
		return row, errs.ErrNotFound
	}
	return row, nil
}
