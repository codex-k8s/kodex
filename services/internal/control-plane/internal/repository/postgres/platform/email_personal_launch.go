package platform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// emailPersonalLaunchProblem dispatches personal email to the mailbox owner rather than the generic token selector; its positive observation never authorizes a later invocation or hides another user's owner coordinates in a response.
// emailPersonalLaunchProblem направляет личную почту владельцу mailbox вместо generic token selector; положительное наблюдение не разрешает будущую invocation и не выдаёт координаты владельца другого пользователя.
func (repository *Repository) emailPersonalLaunchProblem(ctx context.Context, tx pgx.Tx, current scope, connection personalLaunchConnection, launch personalLaunchContext) (entity.IntegrationCredentialProblem, error) {
	if launch.source == "SCHEDULE" || launch.source == "INTEGRATION" || connection.subjectKind != "USER" {
		return entity.IntegrationCredentialInitiatorMissing, nil
	}
	row, err := readEmailCredentialOwnerTx(ctx, tx, current, connection.ref, current.actorRef)
	if err != nil {
		return "", err
	}
	if row.owner.Policy != entity.IntegrationCredentialInitiatorRequired {
		return "", errs.ErrConflict
	}
	if row.state == "UNCONFIGURED" || row.owner.SubjectID != current.actorID {
		return entity.IntegrationCredentialMissing, nil
	}
	if row.state == "REVOKED" || !row.owner.SubjectActive || !row.initiatorActive {
		return entity.IntegrationCredentialRevoked, nil
	}
	if err := emailpolicy.AuthorizeCredentialOwner(row.owner, row.initiator, row.owner.Generation); err != nil {
		return entity.IntegrationCredentialConnectionUnavailable, nil
	}
	if !row.connectionEnabled || row.connectionState != "CONNECTED" {
		return entity.IntegrationCredentialCheckRequired, nil
	}
	var configuration map[string]string
	if json.Unmarshal(connection.configuration, &configuration) != nil {
		return "", errs.ErrUnavailable
	}
	if configuration["mailbox_id"] == "" {
		return entity.IntegrationCredentialMissing, nil
	}
	mailbox, err := repository.readEmailMailbox(ctx, tx, current, configuration["mailbox_id"], 0)
	if err != nil {
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			return entity.IntegrationCredentialCheckRequired, nil
		}
		return "", err
	}
	if mailbox.ConnectionRef != connection.ref || mailbox.OrganizationRef != current.organizationRef || mailbox.CredentialOwnerGeneration != row.owner.Generation {
		return entity.IntegrationCredentialCheckRequired, nil
	}
	return "", nil
}
